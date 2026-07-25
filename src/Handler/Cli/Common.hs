-- | Shared CLI plumbing: port of the helpers in cmd/root.go plus output
-- conventions used by every command.
module Handler.Cli.Common
  ( Ctx(..)
  , CommandAction
  , NamedCommand
  , mkCommand
  , mkCommandGroup
  , openDb
  , openReadOnlyDb
  , ensureSetup
  , dieText
  , printJson
  , putTextLn
  , printfT
  , resolveSessionIdOpt
  , resolveSessionByTarget
  , resolveRecipient
  , claudePid
  , findSessionsWithUnreads
  , syncSessionMetadata
  , sessionIdOption
  ) where

import Control.Monad (filterM, unless, when)
import Data.Aeson (Value)
import Data.Aeson.Encode.Pretty (Config(..), Indent(..), defConfig, encodePretty', keyOrder)
import qualified Data.ByteString.Lazy.Char8 as BL
import Data.Maybe (fromMaybe)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.IO as TIO
import qualified Database.SQLite.Simple as SQL
import Database.SQLite.Simple.ToField (toField)
import Options.Applicative
import System.Directory (doesFileExist)
import System.Environment (lookupEnv)
import System.Exit (exitWith, ExitCode(..))
import System.FilePath (takeDirectory, (</>))
import System.IO (stderr)
import System.Posix.Process (getParentProcessID)
import Text.Read (readMaybe)

import Handler.Db (Db(..), defaultPath, handlerHome)
import qualified Handler.Db as Db
import Handler.Db.Events (humanUnreadCountForSession)
import Handler.Db.Sessions (Session(..), getSession, listSessions)
import Handler.Discover (isSessionProcess, resolveSessionId, writePidCache)
import Handler.Terminal (cmuxWorkspaceInfo)

-- | Global CLI context (the --json persistent flag).
newtype Ctx = Ctx { jsonOutput :: Bool }

type CommandAction = Ctx -> IO ()

-- | A subcommand: its name (for the setup-check exemption list) and action.
type NamedCommand = (String, CommandAction)

-- | Builds a subcommand entry for the root parser.
mkCommand :: String -> String -> Parser CommandAction -> Mod CommandFields NamedCommand
mkCommand name desc parser =
  command name (info (fmap ((,) name) parser <**> helper) (progDesc desc))

-- | Like mkCommand for commands with their own nested subcommands.
mkCommandGroup :: String -> String -> Parser NamedCommand -> Mod CommandFields NamedCommand
mkCommandGroup name desc parser =
  command name (info (parser <**> helper) (progDesc desc))

openDb :: IO Db
openDb = defaultPath >>= Db.open

openReadOnlyDb :: IO Db
openReadOnlyDb = defaultPath >>= Db.openReadOnly

-- | Refuses to run when the database doesn't exist yet (setup not run),
-- mirroring the cobra PersistentPreRunE check.
ensureSetup :: IO ()
ensureSetup = do
  dbPath <- defaultPath
  exists <- doesFileExist dbPath
  unless exists $ do
    TIO.hPutStrLn stderr "agent-handler is not set up yet. Run 'handler setup' to configure skills, hooks, and database."
    exitWith (ExitFailure 1)

-- | Prints an error to stderr and exits 1 (cobra's RunE error path).
dieText :: Text -> IO a
dieText msg = do
  TIO.hPutStrLn stderr msg
  exitWith (ExitFailure 1)

-- | 2-space-indented JSON with sorted keys, like Go's json.MarshalIndent.
printJson :: Value -> IO ()
printJson v = BL.putStrLn (encodePretty' cfg v)
  where cfg = defConfig { confIndent = Spaces 2, confCompare = keyOrder [] <> compare }

putTextLn :: Text -> IO ()
putTextLn = TIO.putStrLn

-- | printf-lite: Text interpolation helper used all over the CLI output.
printfT :: [Text] -> IO ()
printfT = TIO.putStrLn . T.concat

-- | The --session-id option shared by many commands.
sessionIdOption :: Parser (Maybe Text)
sessionIdOption = optional $ strOption
  ( long "session-id" <> metavar "ID"
  <> help "session ID (auto-detected if omitted)" )

-- | Explicit --session-id value, or discovery via env/PID cache.
resolveSessionIdOpt :: Maybe Text -> IO Text
resolveSessionIdOpt (Just sid) | sid /= "" = pure sid
resolveSessionIdOpt _ = do
  home <- handlerHome
  resolveSessionId home >>= \case
    Right sid -> pure sid
    Left err -> dieText ("could not determine session: " <> err)

-- | Finds a session by UUID, name, or branch; errors on ambiguity.
resolveSessionByTarget :: Db -> Text -> IO Session
resolveSessionByTarget db target = do
  bySessionId <- getSession db target
  case bySessionId of
    Just s -> pure s
    Nothing -> do
      sessions <- listSessions db False 100 0
      case [s | s <- sessions, s.sessionName == target || s.branch == target] of
        [s] -> pure s
        (_ : _) -> dieText ("multiple sessions match " <> T.pack (show target) <> " — use full session ID")
        [] -> dieText ("session " <> T.pack (show target) <> " not found")

-- | Resolves an emit --to target to (recipient_type, recipient_value):
-- UUID → session, repo:branch → branch, role name, session name, branch name.
resolveRecipient :: Db -> Text -> IO (Text, Text)
resolveRecipient db to
  | T.length to == 36 && T.count "-" to == 4 = pure ("session", to)
  | ":" `T.isInfixOf` to = pure ("branch", to)
  | otherwise = do
      roleCounts <- SQL.query db.conn
        "SELECT COUNT(*) FROM sessions WHERE role = ? AND status = 'active'" (SQL.Only to)
      let roleCount = case roleCounts of { (SQL.Only n : _) -> n; _ -> 0 :: Int }
      if roleCount > 0
        then pure ("role", to)
        else do
          nameRows <- SQL.query db.conn
            "SELECT session_id FROM sessions WHERE session_name = ? AND status != 'archived'"
            (SQL.Only to)
          case nameRows of
            [SQL.Only sid] -> pure ("session", sid)
            (_ : _ : _) -> dieText ("multiple sessions named " <> T.pack (show to) <> " — use session ID instead")
            [] -> do
              repos <- SQL.query db.conn
                "SELECT DISTINCT repo FROM sessions WHERE branch = ? AND status != 'archived'"
                (SQL.Only to)
              case [r | SQL.Only r <- repos] :: [Text] of
                [_] -> pure ("branch", to)
                (r0 : _ : _) -> dieText
                  ("branch " <> T.pack (show to) <> " exists in multiple repos: "
                   <> T.intercalate ", " [r | SQL.Only r <- repos]
                   <> ". Use repo:branch format (e.g. " <> r0 <> ":" <> to <> ")")
                [] -> dieText ("no session or branch found matching " <> T.pack (show to))

-- | The Claude process PID. Hooks set CLAUDE_PID=$PPID because the handler
-- binary is a grandchild of Claude (Claude → bash → handler).
claudePid :: IO Int
claudePid = do
  env <- lookupEnv "CLAUDE_PID"
  case env >>= readMaybe of
    Just pid | pid > 0 -> pure pid
    _ -> fromIntegral <$> getParentProcessID

-- | Sessions (other than self, other than handler role) whose process is
-- alive and that have human-unread events.
findSessionsWithUnreads :: Db -> Text -> IO [Session]
findSessionsWithUnreads db selfSessionId = do
  sessions <- listSessions db False 1000 0
  flip filterM sessions $ \s ->
    if s.sessionId == selfSessionId || s.role == "handler"
      then pure False
      else do
        aliveOk <- if s.pid > 0
          then isSessionProcess s.pid s.sessionId
          else pure True
        if not aliveOk
          then pure False
          else (> 0) <$> humanUnreadCountForSession db s.sessionId

-- | Updates session name, PID, and terminal info only if changed.
syncSessionMetadata :: Db -> Text -> Text -> Int -> Text -> Text -> Text -> Text -> IO ()
syncSessionMetadata db sessionId name pid termType termId workspaceId cwd = do
  msession <- getSession db sessionId
  case msession of
    Nothing -> pure ()
    Just session -> do
      when (pid > 0 && session.pid /= pid) $ do
        dbPath <- defaultPath
        writePidCache (takeDirectory dbPath </> "sessions") pid sessionId
      (wsName, wsColor) <-
        if termType == "cmux" && termId /= ""
          then cmuxWorkspaceInfo termId
          else pure ("", "")
      let updates = concat
            [ [("session_name", name) | name /= "" && session.sessionName /= name]
            , [("pid", T.pack (show pid)) | pid > 0 && session.pid /= pid]
            , [("terminal_type", termType) | termType /= "" && session.terminalType /= termType]
            , [("terminal_id", termId) | termId /= "" && session.terminalId /= termId]
            , [("cmux_workspace_id", workspaceId) | workspaceId /= "" && session.cmuxWorkspaceId /= workspaceId]
            , [("cmux_workspace_name", wsName) | wsName /= "" && session.cmuxWorkspaceName /= wsName]
            , [("cmux_workspace_color", wsColor) | wsColor /= "" && session.cmuxWorkspaceColor /= wsColor]
            , [("cwd", cwd) | cwd /= "" && session.cwd /= cwd]
            ]
      unless (null updates) $ do
        let setClauses = T.intercalate ", " [col <> " = ?" | (col, _) <- updates]
            args = [ if col == "pid" then toField pid else toField val
                   | (col, val) <- updates ]
        SQL.execute db.conn
          (SQL.Query ("UPDATE sessions SET " <> setClauses <> " WHERE session_id = ?"))
          (args ++ [toField sessionId])
