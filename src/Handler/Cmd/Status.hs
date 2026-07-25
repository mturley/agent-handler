-- | Port of cmd/status.go: list sessions and their status.
-- Also hosts display helpers shared with switch/triage (renderSessionList,
-- formatDuration) and the peek-cache scan from cmd/peek_cache.go.
module Handler.Cmd.Status
  ( statusCommand
  , SessionStatus(..)
  , buildSessionStatuses
  , renderSessionList
  , hexToAnsi
  , hexToDimAnsi
  , formatDuration
  , parseIso
  , sinceSeconds
  , peekScanWithCache
  , findSessionsAwaitingApproval
  , ansiDim, ansiReset, ansiBold, ansiGreen, ansiYellow, ansiRed, ansiUnderline
  ) where

import Control.Exception (SomeException, try)
import Control.Monad (forM, forM_, unless, when)
import Data.Aeson (object, toJSON, (.=))
import Data.Aeson.Types (Pair)
import Data.Char (isHexDigit)
import Data.IORef (modifyIORef', newIORef, readIORef)
import Data.Map.Strict (Map)
import qualified Data.Map.Strict as Map
import Data.Maybe (catMaybes, fromMaybe, mapMaybe)
import Data.Text (Text)
import qualified Data.Text as T
import Data.Time.Clock (UTCTime, diffUTCTime, getCurrentTime)
import Data.Time.Format.ISO8601 (iso8601ParseM)
import qualified Database.SQLite.Simple as SQL
import Numeric (readHex)
import Options.Applicative

import Handler.Cli.Common
import Handler.Config (configDefaultPath, isServiceConfigured, readConfig)
import Handler.Db (Db(..), close)
import Handler.Db.Events (unreadCountForSession)
import Handler.Db.Peek (PeekState(..), listPeekStates, peekStatesAgeSeconds, upsertPeekState)
import Handler.Db.Sessions (Session(..), listSessions)
import Handler.Db.Subscriptions (Subscription(..), listSubscriptions)
import Handler.Db.WatcherStatus (WatcherStatus(..), getWatcherStatus, hasWatcherError)
import Handler.Discover (isSessionProcess)
import Handler.Terminal (Backend(..), needsInput, newBackend)
import Handler.Util (nowIso)
import Handler.Watcher.Scheduler (installedInterval, isInstalled, isRunning, lastRunTime)

data StatusOpts = StatusOpts
  { sAll   :: Bool
  , sLimit :: Int
  }

statusCommand :: Mod CommandFields NamedCommand
statusCommand = mkCommand "status" "List sessions and their status" (runStatus <$> opts)
  where
    opts = StatusOpts
      <$> switch (long "all" <> help "include archived sessions")
      <*> option auto (long "limit" <> value 20 <> help "maximum number of sessions to show")

-- | Per-session status row shared by status/switch rendering and --json.
data SessionStatus = SessionStatus
  { sessionId    :: Text
  , sessionName  :: Text
  , branch       :: Text
  , pid          :: Int
  , status       :: Text
  , displayState :: Text
  , inboxMode    :: Text
  , peekable     :: Bool
  , terminalType :: Text
  , unreadCount  :: Int
  , lastActive   :: Text
  , lastPrompt   :: Text
  , breakdown    :: Map Text Int
  }

ansiDim, ansiReset, ansiBold, ansiGreen, ansiYellow, ansiRed, ansiUnderline :: Text
ansiDim = "\ESC[2m"
ansiReset = "\ESC[0m"
ansiBold = "\ESC[1m"
ansiGreen = "\ESC[32m"
ansiYellow = "\ESC[33m"
ansiRed = "\ESC[31m"
ansiUnderline = "\ESC[4m"

-- | Parses an RFC3339 timestamp.
parseIso :: Text -> Maybe UTCTime
parseIso = iso8601ParseM . T.unpack

-- | Seconds elapsed since the given timestamp.
sinceSeconds :: UTCTime -> IO Double
sinceSeconds t = do
  now <- getCurrentTime
  pure (realToFrac (diffUTCTime now t))

-- | Go-style compact duration: 42s, 5m, 3h, 2d.
formatDuration :: Double -> Text
formatDuration d
  | d < 60 = T.pack (show (truncate d :: Int)) <> "s"
  | d < 3600 = T.pack (show (truncate (d / 60) :: Int)) <> "m"
  | d < 24 * 3600 = T.pack (show (truncate (d / 3600) :: Int)) <> "h"
  | otherwise = T.pack (show (truncate (d / (24 * 3600)) :: Int)) <> "d"

statusToJson :: SessionStatus -> [Pair]
statusToJson st =
  [ "session_id" .= st.sessionId
  , "session_name" .= st.sessionName
  , "branch" .= st.branch
  , "pid" .= st.pid
  , "status" .= st.status
  , "display_state" .= st.displayState
  , "inbox_mode" .= st.inboxMode
  , "peekable" .= st.peekable
  , "unread_count" .= st.unreadCount
  , "last_active" .= st.lastActive
  ]
  ++ [ "terminal_type" .= st.terminalType | st.terminalType /= "" ]
  ++ [ "last_prompt" .= st.lastPrompt | st.lastPrompt /= "" ]
  ++ [ "unread_breakdown" .= st.breakdown | not (Map.null st.breakdown) ]

runStatus :: StatusOpts -> Ctx -> IO ()
runStatus o ctx = do
  db <- openReadOnlyDb
  sessions <- listSessions db o.sAll o.sLimit 0
  statuses <- buildSessionStatuses sessions (Just db)

  if ctx.jsonOutput
    then printJson $ toJSON
      [ object $ statusToJson st
          ++ [ "repo" .= s.repo ]
          ++ [ "cmux_workspace" .= s.cmuxWorkspaceName | s.cmuxWorkspaceName /= "" ]
      | (st, s) <- zip statuses sessions
      ]
    else if null statuses
      then putTextLn "No sessions found"
      else do
        renderSessionList sessions statuses

        -- Watcher and resource summary
        printfT ["\n", ansiDim, "─── Watchers ───", ansiReset]
        cfg <- configDefaultPath >>= readConfig
        forM_ (["github", "jira"] :: [Text]) $ \svc -> do
          status <- do
            if not (isServiceConfigured cfg svc)
              then pure (ansiRed <> "✗ not configured" <> ansiReset)
              else do
                installed <- isInstalled svc
                if installed
                  then do
                    mLastRun <- lastRunTime svc
                    (runInfo, nextInfo) <- case mLastRun of
                      Nothing -> pure ("never", "")
                      Just lastRun -> do
                        since <- sinceSeconds lastRun
                        interval <- installedInterval svc
                        nextInfo <-
                          if interval > 0
                            then do
                              let untilNext = fromIntegral interval - since
                              pure $ if untilNext > 0
                                then ", next: " <> formatDuration untilNext
                                else ", next: any moment"
                            else pure ""
                        pure (formatDuration since <> " ago", nextInfo)
                    running <- isRunning svc
                    if running
                      then do
                        hasError <- hasWatcherError db svc
                        if hasError
                          then do
                            mws <- getWatcherStatus db svc
                            let errMsg = case mws of
                                  Just ws | ws.lastErrorMessage /= "" ->
                                    "\n  " <> ansiDim <> "         " <> ws.lastErrorMessage <> ansiReset
                                  _ -> ""
                            pure (ansiRed <> "✗ error" <> ansiReset <> " " <> ansiDim
                                  <> "(last run: " <> runInfo <> nextInfo <> ")" <> ansiReset <> errMsg)
                          else
                            pure (ansiGreen <> "✓ running" <> ansiReset <> " " <> ansiDim
                                  <> "(last run: " <> runInfo <> nextInfo <> ")" <> ansiReset)
                      else
                        pure (ansiYellow <> "⏸ stopped" <> ansiReset <> " " <> ansiDim
                              <> "(last run: " <> runInfo <> " — run 'handler watcher start')" <> ansiReset)
                  else
                    pure (ansiYellow <> "✓ configured" <> ansiReset <> " " <> ansiDim
                          <> "(not installed — run 'handler watcher install " <> svc <> "')" <> ansiReset)
          printfT ["  ", T.justifyLeft 8 ' ' svc, " ", status]

        -- Active resources being watched
        allSubs <- do
          countsRef <- newIORef (Map.empty :: Map Text Int)
          forM_ sessions $ \s ->
            unless (s.status == "archived") $ do
              subs <- listSubscriptions db s.sessionId False
              forM_ subs $ \sub ->
                modifyIORef' countsRef (Map.insertWith (+) (sub.resourceType <> ":" <> sub.resourceId) 1)
          readIORef countsRef
        unless (Map.null allSubs) $ do
          let byType = Map.fromListWith (+)
                [ (T.takeWhile (/= ':') key, 1 :: Int) | key <- Map.keys allSubs ]
              typeSummary = T.intercalate ", "
                [ T.pack (show c) <> " " <> t | (t, c) <- Map.toList byType ]
          printfT ["\n", ansiDim, "─── Resources ───", ansiReset]
          printfT ["  ", ansiBold, typeSummary, ansiReset, " being watched across all sessions"]
          printfT ["  ", ansiDim, "Run 'handler watching --global' for details", ansiReset]

        -- Count dead sessions
        let deadCount = length [() | st <- statuses, st.displayState == "dead"]
        when (deadCount > 0) $
          printfT ["\n  ", ansiDim, T.pack (show deadCount), " dead session(s). Run 'handler cleanup' to archive.", ansiReset]
  close db

-- | Computes display state and unread counts for each session.
-- Pass Nothing for the Db to skip unread counting (switch's interactive list).
buildSessionStatuses :: [Session] -> Maybe Db -> IO [SessionStatus]
buildSessionStatuses sessions mdb =
  forM sessions $ \s -> do
    displayState <-
      if s.status == "archived"
        then pure "archived"
        else do
          processAlive <- isSessionProcess s.pid s.sessionId
          if not processAlive
            then pure "dead"
            else case parseIso s.lastPrompt of
              Just lastPrompt -> do
                since <- sinceSeconds lastPrompt
                pure $ if since < 24 * 3600 then "active" else "idle"
              Nothing -> pure "idle"

    (unreadCount, breakdown) <- case mdb of
      Just db | displayState == "active" || displayState == "idle" -> do
        -- Go ignores the error and falls back to zero values
        r <- try (unreadCountForSession db s.sessionId)
        pure $ either (\(_ :: SomeException) -> (0, Map.empty)) id r
      _ -> pure (0, Map.empty)

    pure SessionStatus
      { sessionId = s.sessionId
      , sessionName = s.sessionName
      , branch = s.branch
      , pid = s.pid
      , status = s.status
      , displayState = displayState
      , inboxMode = s.inboxMode
      , peekable = s.terminalType /= ""
      , terminalType = s.terminalType
      , unreadCount = unreadCount
      , lastActive = s.lastActive
      , lastPrompt = s.lastPrompt
      , breakdown = breakdown
      }
-- | Renders the grouped repo → workspace → session list (shared with switch).
renderSessionList :: [Session] -> [SessionStatus] -> IO ()
renderSessionList sessions statuses = do
  -- Group by repo, then by cmux workspace, preserving first-seen order.
  let entries = zip statuses sessions
      repoNames = dedup [s.repo | (_, s) <- entries]
      dedup = foldl (\acc x -> if x `elem` acc then acc else acc ++ [x]) []
  forM_ (zip [0 :: Int ..] repoNames) $ \(ri, repoName) -> do
    when (ri > 0) $ putTextLn ""
    printfT ["Repo: ", repoName]
    let repoEntries = [e | e@(_, s) <- entries, s.repo == repoName]
        wsNames = dedup [s.cmuxWorkspaceName | (_, s) <- repoEntries]
    forM_ wsNames $ \wsName -> do
      let wsEntries = [e | e@(_, s) <- repoEntries, s.cmuxWorkspaceName == wsName]
      when (wsName /= "") $ do
        putTextLn ""
        let wsColor = case wsEntries of
              ((_, s0) : _) | s0.cmuxWorkspaceColor /= "" -> hexToAnsi s0.cmuxWorkspaceColor
              _ -> "\ESC[35m"
        printfT ["    ", wsColor, "● Workspace:", ansiReset, " ", wsName]
      forM_ wsEntries $ \(st, _) -> do
        putTextLn ""
        let stateColor = case st.displayState of
              "active" -> ansiGreen
              "idle"   -> ansiYellow
              "dead"   -> ansiRed
              _        -> ansiDim
            name = if st.sessionName == "" then T.take 8 st.sessionId else st.sessionName
            peekableStr = if st.peekable then " " <> ansiDim <> "👁" <> ansiReset else ""
            indent = if wsName /= "" then "        " else "    "
        printfT [indent, "Session: ", ansiBold, ansiUnderline, name, ansiReset, " ",
                 stateColor, st.displayState, ansiReset, peekableStr]
        when (st.branch /= name) $
          printfT [indent, ansiDim, "@ ", st.branch, ansiReset]
        when (st.unreadCount > 0) $ do
          let parts = T.intercalate ", "
                [ T.pack (show c) <> " " <> t | (t, c) <- Map.toList st.breakdown ]
          printfT [indent, T.pack (show st.unreadCount), " unread (", parts, ")"]
        case parseIso st.lastPrompt of
          Just lastPrompt | st.lastPrompt /= "" -> do
            since <- sinceSeconds lastPrompt
            printfT [indent, ansiDim, "Last prompt: ", formatDuration since, " ago  |  ID: ", st.sessionId, ansiReset]
          _ ->
            printfT [indent, ansiDim, "ID: ", st.sessionId, ansiReset]

-- | 24-bit foreground escape from \"#rrggbb\" (magenta fallback).
hexToAnsi :: Text -> Text
hexToAnsi hex0 = maybe "\ESC[35m" render (parseHexRgb hex0)
  where render (r, g, b) = "\ESC[38;2;" <> T.pack (show r) <> ";" <> T.pack (show g) <> ";" <> T.pack (show b) <> "m"

-- | Dim 24-bit foreground escape from \"#rrggbb\".
hexToDimAnsi :: Text -> Text
hexToDimAnsi hex0 = maybe "\ESC[2;35m" render (parseHexRgb hex0)
  where render (r, g, b) = "\ESC[2;38;2;" <> T.pack (show r) <> ";" <> T.pack (show g) <> ";" <> T.pack (show b) <> "m"

parseHexRgb :: Text -> Maybe (Int, Int, Int)
parseHexRgb hex0 =
  let hex = fromMaybe hex0 (T.stripPrefix "#" hex0)
  in if T.length hex == 6 && T.all isHexDigit hex
       then (,,) <$> byte 0 hex <*> byte 2 hex <*> byte 4 hex
       else Nothing
  where
    byte i h = case readHex (T.unpack (T.take 2 (T.drop i h))) of
      [(v, "")] -> Just v
      _ -> Nothing

-- | Port of cmd/peek_cache.go PeekScanWithCache: cached peek states if fresh
-- (within maxAgeSeconds), else a fresh capture-pane scan of peekable sessions.
peekScanWithCache :: Db -> Double -> IO [PeekState]
peekScanWithCache db maxAgeSeconds = do
  age <- peekStatesAgeSeconds db
  if age <= maxAgeSeconds
    then listPeekStates db
    else do
      sessions <- listSessions db False 1000 0
      now <- nowIso
      fmap catMaybes $ forM sessions $ \s ->
        if s.terminalType == "" || s.terminalId == "" || s.role == "handler"
          then pure Nothing
          else do
            aliveOk <- if s.pid > 0 then isSessionProcess s.pid s.sessionId else pure True
            if not aliveOk
              then pure Nothing
              else case newBackend s.terminalType of
                Left _ -> pure Nothing
                Right backend ->
                  backend.capture s.terminalId 0 >>= \case
                    Left _ -> pure Nothing
                    Right content -> do
                      let (needs, reason) = needsInput content
                      upsertPeekState db s.sessionId content needs reason now
                      pure $ Just PeekState
                        { sessionId = s.sessionId
                        , content = content
                        , needsInput = needs
                        , reason = reason
                        , updatedAt = now
                        }

-- | Sessions that need input, using the peek cache (cmd/peek_cache.go).
findSessionsAwaitingApproval :: Db -> IO [Session]
findSessionsAwaitingApproval db = do
  states <- peekScanWithCache db 5
  sessions <- listSessions db False 1000 0
  let sessionMap = Map.fromList [(s.sessionId, s) | s <- sessions]
  pure $ mapMaybe (\ps -> if ps.needsInput then Map.lookup ps.sessionId sessionMap else Nothing) states
