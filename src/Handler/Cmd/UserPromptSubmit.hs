-- | Port of cmd/user_prompt_submit.go: the UserPromptSubmit hook handler.
-- Reads Claude Code hook stdin JSON, registers if needed, bumps heartbeat,
-- and outputs inbox directives. Also exports the archived-session cursor
-- and subscription migration helpers (defined in cmd/statusline.go in Go).
module Handler.Cmd.UserPromptSubmit
  ( userPromptSubmitCommand
  , migrateOldCursor
  , migrateSubscriptionsFromArchived
  ) where

import Control.Monad (forM_, unless, when)
import qualified Data.Aeson as A
import qualified Data.Aeson.KeyMap as KM
import qualified Data.ByteString as BS
import Data.Maybe (fromMaybe)
import Data.Text (Text)
import qualified Data.Text as T
import Options.Applicative
import System.Directory (createDirectoryIfMissing, getCurrentDirectory)
import System.Exit (ExitCode(..))
import System.FilePath (takeDirectory, (</>))
import System.Process (readProcessWithExitCode)

import Handler.Cli.Common
import Handler.Cmd.Register (autoSubscribeWorktreeResources, spawnCatchUpWatcherRuns)
import Handler.Db (Db(..), close, defaultPath)
import Handler.Db.Cursors (advanceCursor, autoDeliveredCount, getCursor)
import Handler.Db.Events (unreadCountForSession)
import Handler.Db.Sessions (Session(..), bumpLastActive, bumpLastPrompt, getSession, upsertSession)
import Handler.Db.Subscriptions (Subscription(..), listSubscriptions, subscribeIfNew)
import Handler.Discover (writePidCache)
import Handler.Terminal (detect)
import Handler.Util (newUuid, nowIso)
import qualified Database.SQLite.Simple as SQL

newtype UpsOpts = UpsOpts { fromHook :: Bool }

userPromptSubmitCommand :: Mod CommandFields NamedCommand
userPromptSubmitCommand = mkCommand "user-prompt-submit" "Handle UserPromptSubmit hook events" (runUps <$> opts)
  where
    opts = UpsOpts <$> switch (long "from-hook" <> help "read from stdin JSON (hook mode)")

data PromptSubmitInput = PromptSubmitInput
  { inSessionId      :: Text
  , inTranscriptPath :: Text
  , inCwd            :: Text
  , inPrompt         :: Text
  , inSessionTitle   :: Text
  }

parseInput :: BS.ByteString -> Maybe PromptSubmitInput
parseInput raw = do
  A.Object o <- A.decodeStrict raw
  let str k = case KM.lookup k o of
        Just (A.String s) -> s
        _ -> ""
  pure PromptSubmitInput
    { inSessionId = str "session_id"
    , inTranscriptPath = str "transcript_path"
    , inCwd = str "cwd"
    , inPrompt = str "prompt"
    , inSessionTitle = str "session_title"
    }

runUps :: UpsOpts -> Ctx -> IO ()
runUps o _ctx = do
  unless o.fromHook $ dieText "--from-hook is required"
  raw <- BS.getContents
  case parseInput raw of
    Nothing -> dieText "failed to parse stdin JSON"
    Just input | input.inSessionId == "" -> pure ()
    Just input -> do
      db <- openDb

      -- Register if not yet registered (one-time cost on first prompt)
      msession0 <- getSession db input.inSessionId
      msession <- case msession0 of
        Just s | s.status /= "archived" -> pure (Just s)
        _ -> do
          when (input.inTranscriptPath /= "") $
            registerSession db input
          getSession db input.inSessionId

      case msession of
        Nothing -> close db
        Just session -> do
          let isAutoInbox = input.inPrompt == "/inbox --auto"

          -- Heartbeat: bump last_active and last_prompt
          now <- nowIso
          bumpLastActive db input.inSessionId now
          bumpLastPrompt db input.inSessionId now

          -- Auto mode: notify about auto-delivered events but DON'T advance
          -- the human cursor; it only advances on explicit /catchup or /inbox.
          when (not isAutoInbox && session.inboxMode == "auto") $ do
            autoCount <- autoDeliveredCount db input.inSessionId
            when (autoCount > 0) $
              printfT [ "IMPORTANT: ", T.pack (show autoCount)
                      , " event(s) were auto-delivered while the user was away. You MUST invoke the /catchup skill NOW, before doing anything else. Do not respond to the user's prompt until /catchup has run." ]

          -- Sync session metadata (name, terminal)
          (termType, termId, workspaceId) <- detect
          pid <- claudePid
          syncSessionMetadata db input.inSessionId input.inSessionTitle pid termType termId workspaceId input.inCwd

          -- On-submit mode: notify about unread events
          when (session.inboxMode == "on-submit") $ do
            (unreadCount, _) <- unreadCountForSession db input.inSessionId
            when (unreadCount > 0) $
              printfT [ "You have ", T.pack (show unreadCount)
                      , " new unread message(s). Invoke the /inbox skill now before responding to the user's prompt." ]
          close db

-- | First-prompt registration path (registerSession in Go).
registerSession :: Db -> PromptSubmitInput -> IO ()
registerSession db input = do
  cwd <- if input.inCwd == "" then getCurrentDirectory else pure (T.unpack input.inCwd)

  branch <- gitOut cwd ["rev-parse", "--abbrev-ref", "HEAD"] "unknown"
  repo <- do
    r <- gitOut cwd ["remote", "get-url", "origin"] ""
    pure $ case T.breakOn "github.com" r of
      (_, rest) | rest /= "" ->
        let r' = T.drop (T.length "github.com") rest
        in stripSuffix' ".git" (T.dropWhile (\c -> c == ':' || c == '/') r')
      _ -> "unknown"

  (termType, termId, workspaceId) <- detect
  pid <- claudePid
  now <- nowIso
  upsertSession db Session
    { sessionId = input.inSessionId
    , harness = "claude-code"
    , repo = repo
    , branch = branch
    , sessionName = input.inSessionTitle
    , pid = pid
    , status = "active"
    , inboxMode = "manual"
    , autoPollInterval = Nothing
    , role = ""
    , terminalType = termType
    , terminalId = termId
    , cmuxWorkspaceId = workspaceId
    , cmuxWorkspaceName = ""
    , cmuxWorkspaceColor = ""
    , lastActive = now
    , lastPrompt = ""
    , cwd = ""
    , registeredAt = now
    , jsonlPath = input.inTranscriptPath
    }

  -- Write PID cache
  dbPath <- defaultPath
  let sessionsDir = takeDirectory dbPath </> "sessions"
  createDirectoryIfMissing True sessionsDir
  writePidCache sessionsDir pid input.inSessionId

  -- Only initialize the cursor for brand new sessions; re-registered
  -- sessions keep their old cursor so queued inbox messages aren't lost.
  existingCursor <- getCursor db input.inSessionId
  when (existingCursor == "") $ do
    when (input.inSessionTitle /= "") $
      migrateOldCursor db input.inSessionId input.inSessionTitle
    c <- getCursor db input.inSessionId
    when (c == "") $
      advanceCursor db input.inSessionId now

  -- Migrate subscriptions from archived session with same name
  when (input.inSessionTitle /= "") $
    migrateSubscriptionsFromArchived db input.inSessionId input.inSessionTitle

  -- Auto-subscribe from .worktree-resources
  autoSubscribeWorktreeResources db input.inSessionId cwd now

  -- Spawn catch-up watcher runs for subscribed resources
  spawnCatchUpWatcherRuns db input.inSessionId
  where
    stripSuffix' suf t = fromMaybe t (T.stripSuffix suf t)
    gitOut cwd args fallback = do
      (code, out, _) <- readProcessWithExitCode "git" (["-C", cwd] ++ args) ""
      pure $ case code of
        ExitSuccess -> T.strip (T.pack out)
        _ -> fallback

-- | Port of migrateSubscriptionsFromArchived (cmd/statusline.go): copy
-- subscriptions from the most recently archived same-name session.
migrateSubscriptionsFromArchived :: Db -> Text -> Text -> IO ()
migrateSubscriptionsFromArchived db newSessionId sessionName = do
  -- Don't migrate if another active session shares the name (duplicate window)
  activeCounts <- SQL.query db.conn
    "SELECT COUNT(*) FROM sessions WHERE session_name = ? AND status = 'active'"
    (SQL.Only sessionName)
  let activeCount = case activeCounts of { (SQL.Only n : _) -> n; _ -> 0 :: Int }
  unless (activeCount > 1) $ do
    -- Don't migrate if this session already has subscriptions
    existingSubs <- listSubscriptions db newSessionId False
    when (null existingSubs) $ do
      archivedIds <- SQL.query db.conn
        "SELECT session_id FROM sessions\
        \ WHERE session_name = ? AND status = 'archived' AND session_id != ?\
        \ ORDER BY last_active DESC LIMIT 1"
        (sessionName, newSessionId)
      case archivedIds of
        [SQL.Only archivedId] -> do
          rows <- SQL.query db.conn
            "SELECT resource_type, resource_id, resource_url\
            \ FROM subscriptions WHERE session_id = ?\
            \ ORDER BY created_at DESC"
            (SQL.Only (archivedId :: Text))
            :: IO [(Text, Text, Maybe Text)]
          now <- nowIso
          forM_ rows $ \(resType, resId, resUrl) -> do
            subId <- newUuid
            subscribeIfNew db Subscription
              { subId = subId
              , sessionId = newSessionId
              , resourceType = resType
              , resourceId = resId
              , resourceUrl = resUrl
              , createdAt = now
              , deletedAt = Nothing
              }
        _ -> pure ()

-- | Port of migrateOldCursor (cmd/statusline.go): inherit the cursor of the
-- most recently archived same-name session.
migrateOldCursor :: Db -> Text -> Text -> IO ()
migrateOldCursor db newSessionId sessionName = do
  activeCounts <- SQL.query db.conn
    "SELECT COUNT(*) FROM sessions WHERE session_name = ? AND status = 'active'"
    (SQL.Only sessionName)
  let activeCount = case activeCounts of { (SQL.Only n : _) -> n; _ -> 0 :: Int }
  unless (activeCount > 1) $ do
    cursors <- SQL.query db.conn
      "SELECT sc.last_seen_ts\
      \ FROM session_cursors sc\
      \ JOIN sessions s ON sc.session_id = s.session_id\
      \ WHERE s.session_name = ? AND s.status = 'archived' AND s.session_id != ?\
      \ ORDER BY s.last_active DESC\
      \ LIMIT 1"
      (sessionName, newSessionId)
    case cursors of
      [SQL.Only oldCursor] | oldCursor /= ("" :: Text) ->
        advanceCursor db newSessionId oldCursor
      _ -> pure ()
