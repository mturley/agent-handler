-- | Port of cmd/register.go: register a new Claude Code agent session.
-- Also exports the worktree auto-subscribe and watcher catch-up helpers
-- shared with user-prompt-submit registration.
module Handler.Cmd.Register
  ( registerCommand
  , autoSubscribeWorktreeResources
  , spawnCatchUpWatcherRuns
  ) where

import Control.Concurrent (forkIO)
import Control.Monad (forM_, unless, void, when)
import Data.Aeson (object, (.=))
import Data.Map.Strict (Map)
import qualified Data.Map.Strict as Map
import Data.Text (Text)
import qualified Data.Text as T
import Options.Applicative
import System.Directory (createDirectoryIfMissing, getCurrentDirectory)
import System.FilePath (takeDirectory, (</>))
import System.Process (spawnProcess)

import Handler.Cli.Common
import Handler.Config (Config, configDefaultPath, defaultResourceUrl, isServiceConfigured, readConfig, resourceTypeToService)
import Handler.Db (Db, close, defaultPath)
import Handler.Db.Cursors (advanceCursor)
import Handler.Db.Events (unreadCountForSession)
import Handler.Db.Sessions (Session(..), getSession, upsertSession)
import Handler.Db.Subscriptions (Subscription(..), listSubscriptions, subscribeIfNew)
import Handler.Discover (cleanStalePidCaches, writePidCache)
import Handler.Worktree (Resource(..), parseResourceId, readResources)
import Handler.Util (newUuid, nowIso)

data RegisterOpts = RegisterOpts
  { rSessionId       :: Text
  , rBranch          :: Text
  , rRepo            :: Text
  , rPid             :: Int
  , rJsonlPath       :: Text
  , rTerminalType    :: Text
  , rTerminalId      :: Text
  , rCmuxWorkspaceId :: Text
  , rSessionName     :: Text
  }

registerCommand :: Mod CommandFields NamedCommand
registerCommand = mkCommand "register" "Register a new Claude Code agent session" (runRegister <$> opts)
  where
    opts = RegisterOpts
      <$> strOption (long "session-id" <> help "session ID")
      <*> strOption (long "branch" <> help "branch name")
      <*> strOption (long "repo" <> help "repository path")
      <*> option auto (long "pid" <> help "process ID")
      <*> strOption (long "jsonl-path" <> help "path to Claude JSONL file")
      <*> strOption (long "terminal-type" <> value "" <> help "terminal backend type (cmux, tmux)")
      <*> strOption (long "terminal-id" <> value "" <> help "terminal surface/pane ID")
      <*> strOption (long "cmux-workspace-id" <> value "" <> help "cmux workspace ID")
      <*> strOption (long "session-name" <> value "" <> help "session display name (from hook stdin)")

runRegister :: RegisterOpts -> Ctx -> IO ()
runRegister o ctx = do
  db <- openDb

  -- Session name comes from --session-name (passed by hooks from Claude's
  -- stdin data). The statusline hook syncs name changes every 10 seconds.
  let sessionName = o.rSessionName

  -- Re-registration vs brand new
  existingSession <- getSession db o.rSessionId
  let isReregistration = existingSession /= Nothing

  now <- nowIso
  upsertSession db Session
    { sessionId = o.rSessionId
    , harness = "claude-code"
    , repo = o.rRepo
    , branch = o.rBranch
    , sessionName = sessionName
    , pid = o.rPid
    , status = "active"
    , inboxMode = "manual"
    , autoPollInterval = Nothing
    , role = ""
    , terminalType = o.rTerminalType
    , terminalId = o.rTerminalId
    , cmuxWorkspaceId = o.rCmuxWorkspaceId
    , cmuxWorkspaceName = ""
    , cmuxWorkspaceColor = ""
    , lastActive = now
    , lastPrompt = ""
    , cwd = ""
    , registeredAt = now
    , jsonlPath = o.rJsonlPath
    }

  -- Write PID cache
  dbPath <- defaultPath
  let sessionsDir = takeDirectory dbPath </> "sessions"
  createDirectoryIfMissing True sessionsDir
  writePidCache sessionsDir o.rPid o.rSessionId
  void $ forkIO $ void $ cleanStalePidCaches sessionsDir

  -- Brand new sessions start with cursor = now; re-registration keeps it
  unless isReregistration $
    advanceCursor db o.rSessionId now

  -- Auto-subscribe to resources from .worktree-resources
  cwd <- getCurrentDirectory
  autoSubscribeWorktreeResources db o.rSessionId cwd now

  -- Spawn background catch-up watcher runs for subscribed resources
  spawnCatchUpWatcherRuns db o.rSessionId

  (unreadCount, breakdown) <- unreadCountForSession db o.rSessionId
  msession <- getSession db o.rSessionId
  session <- maybe (dieText ("session not found: " <> o.rSessionId)) pure msession

  if ctx.jsonOutput
    then printJson $ object
      [ "session_id" .= o.rSessionId
      , "session_name" .= sessionName
      , "status" .= ("active" :: Text)
      , "inbox_mode" .= session.inboxMode
      , "unread_count" .= unreadCount
      , "unread_breakdown" .= breakdown
      ]
    else do
      printfT ["✓ Registered session ", o.rSessionId]
      when (sessionName /= "") $
        printfT ["  Name: ", sessionName]
      putTextLn "  Status: active"
      printfT ["  Inbox mode: ", session.inboxMode]
      if unreadCount > 0
        then do
          printfT ["  Unread: ", T.pack (show unreadCount), " message(s)"]
          forM_ (Map.toList breakdown) $ \(eventType, count) ->
            printfT ["    - ", eventType, ": ", T.pack (show count)]
        else putTextLn "  Unread: No new messages"
      when (session.inboxMode == "auto") $
        putTextLn "\n💡 Inbox mode is 'auto' — restart polling with /inbox-mode auto"
  close db

-- | Subscribes the session to each entry of cwd/.worktree-resources,
-- filling in default URLs from config when the file omits them.
autoSubscribeWorktreeResources :: Db -> Text -> FilePath -> Text -> IO ()
autoSubscribeWorktreeResources db sessionId cwd now = do
  resources <- readResources (cwd </> ".worktree-resources")
  unless (null resources) $ do
    cfg <- configDefaultPath >>= readConfig
    forM_ resources $ \r -> do
      let (resourceType, resourceId) = parseResourceId r.resId
      unless (resourceType == "") $ do
        let resUrl = if r.resUrl == ""
              then defaultResourceUrl cfg resourceType resourceId
              else r.resUrl
        subId <- newUuid
        subscribeIfNew db Subscription
          { subId = subId
          , sessionId = sessionId
          , resourceType = resourceType
          , resourceId = resourceId
          , resourceUrl = if resUrl == "" then Nothing else Just resUrl
          , createdAt = now
          , deletedAt = Nothing
          }

-- | Fires one background `handler watcher run <service>` per configured
-- service covering the session's subscribed resources.
spawnCatchUpWatcherRuns :: Db -> Text -> IO ()
spawnCatchUpWatcherRuns db sessionId = do
  subs <- listSubscriptions db sessionId False
  unless (null subs) $ do
    cfg <- configDefaultPath >>= readConfig
    let byService :: Map Text [Text]
        byService = Map.fromListWith (flip (++))
          [ (service, [sub.resourceId])
          | sub <- subs
          , let service = resourceTypeToService sub.resourceType
          , service /= ""
          , isServiceConfigured cfg service
          ]
    forM_ (Map.toList byService) $ \(service, resources) ->
      void $ spawnProcess "handler"
        ["watcher", "run", T.unpack service, "--resources", T.unpack (T.intercalate "," resources)]
