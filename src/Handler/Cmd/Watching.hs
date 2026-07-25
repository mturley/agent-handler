-- | Port of cmd/watching.go: show watched resources, watcher status, and
-- recent errors.
module Handler.Cmd.Watching (watchingCommand) where

import Control.Monad (forM, forM_, unless)
import Data.Aeson (Value, object, toJSON, (.=))
import Data.Maybe (catMaybes, fromMaybe)
import Data.Text (Text)
import qualified Data.Text as T
import Data.Time.Clock (UTCTime, addUTCTime)
import Data.Time.Format (defaultTimeLocale, formatTime)
import qualified Database.SQLite.Simple as SQL
import Options.Applicative

import Handler.Cli.Common
import Handler.Cmd.Status (ansiBold, ansiDim, ansiGreen, ansiRed, ansiReset, ansiYellow, formatDuration, parseIso, sinceSeconds)
import Handler.Config (configDefaultPath, isServiceConfigured, readConfig)
import Handler.Db (Db(..), close)
import Handler.Db.Subscriptions (Subscription(..), listSubscriptions, subscriptionToJson)
import Handler.Db.WatcherStatus (WatcherStatus(..), getWatcherStatus, hasWatcherError)
import Handler.Watcher.Scheduler (installedInterval, isInstalled, isRunning, lastRunTime)

data WatchingOpts = WatchingOpts
  { wSessionId :: Maybe Text
  , wGlobal    :: Bool
  }

watchingCommand :: Mod CommandFields NamedCommand
watchingCommand = mkCommand "watching" "Show watched resources, watcher status, and recent errors" (runWatching <$> opts)
  where
    opts = WatchingOpts
      <$> sessionIdOption
      <*> switch (long "global" <> help "show all watched resources across all sessions")

-- | Watcher health summary row shared by both views.
data WatcherView = WatcherView
  { wvName       :: Text
  , wvConfigured :: Bool
  , wvInstalled  :: Bool
  , wvRunning    :: Bool
  , wvLastRun    :: Text
  , wvNextRun    :: Text
  , wvInterval   :: Int
  , wvHasError   :: Bool
  , wvErrMessage :: Text
  }

isoFormat :: UTCTime -> Text
isoFormat = T.pack . formatTime defaultTimeLocale "%Y-%m-%dT%H:%M:%SZ"

buildWatcherViews :: Db -> IO [WatcherView]
buildWatcherViews db = do
  cfg <- configDefaultPath >>= readConfig
  forM ["github", "jira"] $ \name -> do
    let configured = isServiceConfigured cfg name
    installed <- isInstalled name
    running <- isRunning name
    mLastRun <- lastRunTime name
    (lastRun, nextRun, interval) <- case mLastRun of
      Nothing -> pure ("", "", 0)
      Just lr -> do
        interval <- installedInterval name
        let next = if interval > 0
              then isoFormat (addUTCTime (fromIntegral interval) lr)
              else ""
        pure (isoFormat lr, next, interval)
    hasErr <- hasWatcherError db name
    errMsg <-
      if hasErr
        then maybe "" (.lastErrorMessage) <$> getWatcherStatus db name
        else pure ""
    pure WatcherView
      { wvName = name, wvConfigured = configured, wvInstalled = installed
      , wvRunning = running, wvLastRun = lastRun, wvNextRun = nextRun
      , wvInterval = interval, wvHasError = hasErr, wvErrMessage = errMsg
      }

watcherViewJson :: Bool -> WatcherView -> Value
watcherViewJson includeInterval wv = object $
  [ "name" .= wv.wvName
  , "configured" .= wv.wvConfigured
  , "installed" .= wv.wvInstalled
  , "running" .= wv.wvRunning
  , "has_error" .= wv.wvHasError
  ]
  ++ [ "last_run" .= wv.wvLastRun | wv.wvLastRun /= "" ]
  ++ [ "next_run" .= wv.wvNextRun | wv.wvNextRun /= "" ]
  ++ [ "interval_seconds" .= wv.wvInterval | includeInterval && wv.wvInterval > 0 ]
  ++ [ "last_error_message" .= wv.wvErrMessage | wv.wvErrMessage /= "" ]

-- | \"5m ago\" / \"never\" and \", next: 2m\" / \", next: any moment\" strings.
lastNextStrings :: WatcherView -> IO (Text, Text)
lastNextStrings wv = do
  lastRun <- case parseIso wv.wvLastRun of
    Just t | wv.wvLastRun /= "" -> do
      since <- sinceSeconds t
      pure (formatDuration since <> " ago")
    _ -> pure "never"
  nextRun <- case parseIso wv.wvNextRun of
    Just t | wv.wvNextRun /= "" -> do
      since <- sinceSeconds t
      pure $ if since < 0
        then ", next: " <> formatDuration (negate since)
        else ", next: any moment"
    _ -> pure ""
  pure (lastRun, nextRun)

runWatching :: WatchingOpts -> Ctx -> IO ()
runWatching o ctx = do
  db <- openReadOnlyDb
  if o.wGlobal
    then runWatchingGlobal db ctx
    else do
      sessionId <- resolveSessionIdOpt o.wSessionId
      subs <- listSubscriptions db sessionId False
      watchers <- buildWatcherViews db

      if ctx.jsonOutput
        then printJson $ object
          [ "subscriptions" .= map subscriptionToJson subs
          , "watchers" .= map (watcherViewJson True) watchers
          ]
        else do
          if null subs
            then putTextLn "No resources are currently being watched in this session."
            else do
              printfT ["Watched resources (", T.pack (show (length subs)), "):"]
              forM_ subs $ \sub -> do
                let url = maybe "" ("  " <>) sub.resourceUrl
                printfT ["  ", sub.resourceType, ":", sub.resourceId, url]

          putTextLn ""
          putTextLn "Watchers:"
          forM_ watchers $ \wv ->
            if | not wv.wvConfigured -> printfT ["  ", wv.wvName, ": not configured"]
               | not wv.wvInstalled -> printfT ["  ", wv.wvName, ": configured but not installed"]
               | otherwise -> do
                   (lastRun, nextRun) <- lastNextStrings wv
                   let state = if wv.wvRunning then "running" else "stopped"
                   if wv.wvHasError
                     then do
                       printfT ["  ", wv.wvName, ": ", state, ", last run ", lastRun, nextRun, " — ERROR"]
                       unless (wv.wvErrMessage == "") $
                         printfT ["    ", wv.wvErrMessage]
                     else printfT ["  ", wv.wvName, ": ", state, ", last run ", lastRun, nextRun]
  close db

runWatchingGlobal :: Db -> Ctx -> IO ()
runWatchingGlobal db ctx = do
  resRows <- SQL.query_ db.conn
    "SELECT DISTINCT sub.resource_type, sub.resource_id, COALESCE(sub.resource_url, '')\
    \ FROM subscriptions sub\
    \ JOIN sessions s ON s.session_id = sub.session_id\
    \ WHERE sub.deleted_at IS NULL AND s.status = 'active'\
    \ ORDER BY sub.resource_type, sub.resource_id"
    :: IO [(Text, Text, Text)]

  results <- forM resRows $ \(rType, rId, rUrl) -> do
    sessRows <- SQL.query db.conn
      "SELECT s.session_id, COALESCE(s.session_name, ''), s.branch, s.last_active\
      \ FROM subscriptions sub\
      \ JOIN sessions s ON s.session_id = sub.session_id\
      \ WHERE sub.resource_type = ? AND sub.resource_id = ?\
      \   AND sub.deleted_at IS NULL AND s.status = 'active'\
      \ ORDER BY s.last_active DESC"
      (rType, rId)
      :: IO [(Text, Text, Text, Text)]
    pure (rType, rId, rUrl, sessRows)

  watchers <- buildWatcherViews db

  if ctx.jsonOutput
    then printJson $ object
      [ "subscriptions" .= toJSON
          [ object $
              [ "resource_type" .= rType
              , "resource_id" .= rId
              , "sessions" .= toJSON
                  [ object $
                      [ "session_id" .= sid, "branch" .= branch, "last_active" .= lastActive ]
                      ++ [ "session_name" .= name | name /= "" ]
                  | (sid, name, branch, lastActive) <- sessions ]
              ]
              ++ [ "resource_url" .= rUrl | rUrl /= "" ]
          | (rType, rId, rUrl, sessions) <- results ]
      , "watchers" .= map (watcherViewJson False) watchers
      ]
    else do
      if null results
        then putTextLn "No active subscriptions across any session"
        else do
          printfT ["Watched resources across all sessions (", T.pack (show (length results)), "):\n"]
          forM_ results $ \(rType, rId, rUrl, sessions) -> do
            printfT ["  ", ansiBold, rType, ":", rId, ansiReset]
            unless (rUrl == "") $
              printfT ["  ", ansiDim, rUrl, ansiReset]
            forM_ sessions $ \(sid, name0, branch, lastActive0) -> do
              let name = if name0 == "" then branch else name0
              lastActive <- case parseIso lastActive0 of
                Just t -> do
                  since <- sinceSeconds t
                  pure (formatDuration since <> " ago")
                Nothing -> pure lastActive0
              printfT ["  ", ansiDim, "└ ", name, " (", T.take 12 sid, ") — ", lastActive, ansiReset]
            putTextLn ""

      printfT [ansiDim, "─── Watchers ───", ansiReset]
      forM_ watchers $ \wv ->
        if | not wv.wvConfigured ->
               printfT ["  ", wv.wvName, ansiRed, "✗", ansiReset, " ", ansiDim, "not configured", ansiReset]
           | not wv.wvInstalled ->
               printfT ["  ", wv.wvName, " ", ansiYellow, "✗", " ", ansiReset, " ", ansiDim, "(not installed)", ansiReset]
           | otherwise -> do
               (lastRun, nextRun) <- lastNextStrings wv
               if | wv.wvHasError -> do
                      printfT ["  ", ansiRed, "✗", ansiReset, " ", wv.wvName, " ", ansiDim,
                               "(last run: ", lastRun, nextRun, ")", ansiReset]
                      unless (wv.wvErrMessage == "") $
                        printfT ["  ", ansiDim, "  ", wv.wvErrMessage, ansiReset]
                  | not wv.wvRunning ->
                      printfT ["  ", ansiYellow, "⏸", ansiReset, " ", wv.wvName, " ", ansiDim,
                               "(stopped, last run: ", lastRun, ")", ansiReset]
                  | otherwise ->
                      printfT ["  ", ansiGreen, "✓", ansiReset, " ", wv.wvName, " ", ansiDim,
                               "(last run: ", lastRun, nextRun, ")", ansiReset]
