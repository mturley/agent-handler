-- | Port of cmd/triage.go: aggregate what needs attention across sessions.
module Handler.Cmd.Triage (triageCommand) where

import Control.Monad (filterM, forM, forM_, unless, void, when)
import Data.Aeson (Value(..), decodeStrict, object, toJSON, (.=))
import Data.Map.Strict (Map)
import qualified Data.Map.Strict as Map
import Data.Maybe (catMaybes, fromMaybe)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.Encoding as TE
import qualified Database.SQLite.Simple as SQL
import Options.Applicative
import System.Process (spawnProcess)

import Handler.Cli.Common
import Handler.Cmd.Status (formatDuration, parseIso, sinceSeconds)
import Handler.Config (configDefaultPath, isServiceConfigured, readConfig, resourceTypeToService)
import Handler.Db (Db(..), close, handlerHome)
import Handler.Db.Cursors (getCursor)
import Handler.Db.Events (unreadCountForSession)
import Handler.Db.ResourceState (ResourceStateWithSubscription(..), listResourceStatesForSession)
import Handler.Db.Sessions (Session(..), listSessions)
import Handler.Db.WatcherStatus (WatcherStatus(..), getWatcherStatus, hasWatcherError)
import Handler.Discover (isSessionProcess, resolveSessionId)
import Handler.Util (epochIso)

triageCommand :: Mod CommandFields NamedCommand
triageCommand = mkCommand "triage" "Aggregate what needs attention across all sessions" (pure runTriage)

data StaleResource = StaleResource
  { srType      :: Text
  , srId        :: Text
  , srUpdatedAt :: Text
  , srMinutes   :: Int
  }

runTriage :: Ctx -> IO ()
runTriage ctx = do
  db <- openReadOnlyDb
  sessions <- listSessions db False 1000 0

  -- Blocked sessions: a 'blocked' event with no later 'unblocked'
  blockedRows <- SQL.query_ db.conn
    "SELECT s.session_id, COALESCE(s.session_name,''), s.branch, e.ts as blocked_since\
    \ FROM sessions s\
    \ JOIN events e ON e.session_id = s.session_id AND e.type = 'blocked'\
    \ WHERE s.status = 'active'\
    \   AND NOT EXISTS (\
    \     SELECT 1 FROM events e2\
    \     WHERE e2.session_id = s.session_id AND e2.type = 'unblocked' AND e2.ts > e.ts\
    \   )"
    :: IO [(Text, Text, Text, Text)]

  -- Sessions with unread events
  sessionsWithUnread <- fmap catMaybes $ forM sessions $ \s ->
    if s.status /= "active"
      then pure Nothing
      else do
        (count, breakdown) <- unreadCountForSession db s.sessionId
        pure $ if count > 0 then Just (s, count, breakdown) else Nothing

  -- Watcher errors
  watcherErrors <- fmap catMaybes $ forM (["github", "jira"] :: [Text]) $ \svc -> do
    hasErr <- hasWatcherError db svc
    if not hasErr
      then pure Nothing
      else do
        mws <- getWatcherStatus db svc
        pure $ (\ws -> (svc, ws.lastErrorMessage)) <$> mws

  -- Events since last check (for the current session; best-effort)
  eventsSinceLastCheck :: Int <- do
    msid <- bestEffortSessionId
    case msid of
      Nothing -> pure 0
      Just sid -> do
        cursor0 <- getCursor db sid
        let cursor = if cursor0 == "" then epochIso else cursor0
        counts <- SQL.query db.conn "SELECT COUNT(*) FROM events WHERE ts > ?" (SQL.Only cursor)
        pure $ case counts of { (SQL.Only n : _) -> n; _ -> 0 }

  -- Dead sessions
  deadSessions <- flip filterM sessions $ \s ->
    if s.status /= "active"
      then pure False
      else if s.pid > 0
        then not <$> isSessionProcess s.pid s.sessionId
        else pure False
  let deadIds = [s.sessionId | s <- deadSessions]
      sessionsActive = length [s | s <- sessions, s.status == "active"] - length deadSessions
      sessionsBlocked = length blockedRows
      sessionsDead = length deadSessions

  -- Resource state per session + staleness (5 min threshold, deduped)
  let staleThreshold = 5 * 60 :: Double
  (sessionResources, staleResources) <- do
    resultsAndStales <- forM [s | s <- sessions, s.status == "active", s.sessionId `notElem` deadIds] $ \s -> do
      states <- listResourceStatesForSession db s.sessionId
      if null states
        then pure (Nothing, [])
        else do
          stales <- fmap catMaybes $ forM states $ \rs ->
            if rs.watcherUpdatedAt == ""
              then pure Nothing
              else case parseIso rs.watcherUpdatedAt of
                Nothing -> pure Nothing
                Just wut -> do
                  since <- sinceSeconds wut
                  pure $ if since > staleThreshold
                    then Just (StaleResource rs.resourceType rs.resourceId rs.watcherUpdatedAt (truncate (since / 60)))
                    else Nothing
          pure (Just (s, states), stales)
    let sessionResources = catMaybes (map fst resultsAndStales)
        -- dedup stale resources across sessions, preserving order
        allStales = concatMap snd resultsAndStales
        dedupStales seen = \case
          [] -> []
          (sr : rest)
            | (sr.srType <> ":" <> sr.srId) `elem` seen -> dedupStales seen rest
            | otherwise -> sr : dedupStales ((sr.srType <> ":" <> sr.srId) : seen) rest
    pure (sessionResources, dedupStales [] allStales)

  -- Trigger catch-up for stale resources (best-effort, non-blocking)
  unless (null staleResources) $ do
    cfg <- configDefaultPath >>= readConfig
    let byService = Map.fromListWith (flip (++))
          [ (svc, [sr.srId])
          | sr <- staleResources
          , let svc = resourceTypeToService sr.srType
          , svc /= ""
          , isServiceConfigured cfg svc
          ]
    forM_ (Map.toList byService) $ \(svc, resources) ->
      void $ spawnProcess "handler"
        ["watcher", "run", T.unpack svc, "--resources", T.unpack (T.intercalate "," resources)]

  if ctx.jsonOutput
    then printJson $ object
      [ "sessions_active" .= sessionsActive
      , "sessions_blocked" .= sessionsBlocked
      , "sessions_dead" .= sessionsDead
      , "blocked_sessions" .= toJSON
          [ object [ "session_id" .= sid, "session_name" .= name
                   , "branch" .= branch, "blocked_since" .= since ]
          | (sid, name, branch, since) <- blockedRows ]
      , "sessions_with_unread" .= toJSON
          [ object [ "session_id" .= s.sessionId, "session_name" .= s.sessionName
                   , "unread_count" .= count, "unread_types" .= breakdown ]
          | (s, count, breakdown) <- sessionsWithUnread ]
      , "watcher_errors" .= toJSON
          [ object [ "name" .= n, "last_error_message" .= m ] | (n, m) <- watcherErrors ]
      , "events_since_last_check" .= eventsSinceLastCheck
      , "dead_sessions" .= toJSON
          [ object [ "session_id" .= s.sessionId, "session_name" .= s.sessionName
                   , "last_active" .= s.lastActive ]
          | s <- deadSessions ]
      , "session_resources" .= toJSON
          [ object [ "session_id" .= s.sessionId, "session_name" .= s.sessionName
                   , "resources" .= toJSON (map resourceDetailJson states) ]
          | (s, states) <- sessionResources ]
      , "stale_resources" .= toJSON
          [ object [ "resource_type" .= sr.srType, "resource_id" .= sr.srId
                   , "watcher_updated_at" .= sr.srUpdatedAt, "stale_minutes" .= sr.srMinutes ]
          | sr <- staleResources ]
      ]
    else do
      putTextLn "Handler Triage\n"

      putTextLn $ "Sessions: " <> T.pack (show sessionsActive) <> " active"
        <> (if sessionsBlocked > 0 then ", " <> T.pack (show sessionsBlocked) <> " blocked" else "")
        <> (if sessionsDead > 0 then ", " <> T.pack (show sessionsDead) <> " dead" else "")

      unless (null blockedRows) $ do
        putTextLn "\nBlocked Sessions:"
        forM_ blockedRows $ \(sid, name0, branch, since) -> do
          let name = if name0 == "" then T.take 8 sid else name0
          printfT ["  ", name, " (", branch, ") - blocked since ", since]

      unless (null sessionsWithUnread) $ do
        putTextLn "\nSessions with Unread Events:"
        forM_ sessionsWithUnread $ \(s, count, breakdown) -> do
          let name = if s.sessionName == "" then T.take 8 s.sessionId else s.sessionName
              typeBreakdown = if Map.null breakdown then ""
                else " (" <> T.intercalate ", "
                       [t <> ": " <> T.pack (show c) | (t, c) <- Map.toList breakdown] <> ")"
          printfT ["  ", name, " - ", T.pack (show count), " unread", typeBreakdown]

      unless (null watcherErrors) $ do
        putTextLn "\nWatcher Errors:"
        forM_ watcherErrors $ \(n, m) -> printfT ["  ", n, ": ", m]

      when (eventsSinceLastCheck > 0) $
        printfT ["\nNew Events: ", T.pack (show eventsSinceLastCheck), " since last check"]

      unless (null deadSessions) $ do
        putTextLn "\nDead Sessions:"
        forM_ deadSessions $ \s -> do
          let name = if s.sessionName == "" then T.take 8 s.sessionId else s.sessionName
          printfT ["  ", name, " - last active ", s.lastActive]

      unless (null sessionResources) $ do
        putTextLn "\nSession Resources:"
        forM_ sessionResources $ \(s, states) -> do
          let name = if s.sessionName == "" then T.take 8 s.sessionId else s.sessionName
          forM_ states $ \rs -> do
            suffix <- case parseIso rs.watcherUpdatedAt of
              Just wut | rs.watcherUpdatedAt /= "" -> do
                since <- sinceSeconds wut
                pure (" (updated " <> formatDuration since <> " ago)")
              _ -> pure ""
            printfT ["  ", name, " → ", rs.resourceType, ":", rs.resourceId, suffix]

      unless (null staleResources) $ do
        putTextLn "\nStale Resources (catch-up triggered):"
        forM_ staleResources $ \sr ->
          printfT ["  ", sr.srType, ":", sr.srId, " — last updated ", T.pack (show sr.srMinutes), "m ago"]

      when (null blockedRows && null sessionsWithUnread && null watcherErrors
            && null deadSessions && eventsSinceLastCheck == 0) $
        putTextLn "\nAll clear - nothing needs attention."
  close db
  where
    resourceDetailJson :: ResourceStateWithSubscription -> Value
    resourceDetailJson rs = object $
      [ "resource_type" .= rs.resourceType
      , "resource_id" .= rs.resourceId
      , "state" .= stateValue
      ]
      ++ [ "resource_url" .= u | Just u <- [rs.resourceUrl] ]
      ++ [ "watcher_updated_at" .= rs.watcherUpdatedAt | rs.watcherUpdatedAt /= "" ]
      where
        stateValue = fromMaybe (String rs.stateJson) (decodeStrict (TE.encodeUtf8 rs.stateJson))

    -- resolveSessionIdOpt dies on failure; triage treats failure as "no session"
    bestEffortSessionId :: IO (Maybe Text)
    bestEffortSessionId = do
      home <- handlerHome
      either (const Nothing) Just <$> resolveSessionId home
