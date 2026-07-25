-- | Port of cmd/statusline.go: renders the Claude Code statusline, records
-- cost snapshots, and handles hook-mode session registration.
module Handler.Cmd.Statusline (statuslineCommand) where

import Control.Concurrent.Async (concurrently)
import Control.Exception (IOException, try)
import Control.Monad (forM_, unless, when)
import Data.Aeson ((.:?), (.!=))
import qualified Data.Aeson as A
import qualified Data.Aeson.KeyMap as KM
import qualified Data.ByteString.Lazy as BL
import Data.List (sortOn)
import qualified Data.Map.Strict as Map
import Data.Maybe (fromMaybe)
import qualified Data.Set as Set
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.Encoding as TE
import qualified Data.Text.IO as TIO
import Data.Time.Calendar (gregorianMonthLength, toGregorian)
import Data.Time.Clock (UTCTime, diffUTCTime, getCurrentTime, utctDay)
import Data.Time.Clock.POSIX (posixSecondsToUTCTime)
import Data.Time.Format (defaultTimeLocale, formatTime, parseTimeM)
import qualified Database.SQLite.Simple as SQL
import Options.Applicative
import System.Directory (createDirectoryIfMissing, doesFileExist, getCurrentDirectory, getHomeDirectory, removeFile)
import System.Exit (ExitCode(..))
import System.FilePath (takeDirectory, (</>))
import System.Posix.Files (FileStatus, fileMode, getFileStatus, intersectFileModes, modificationTimeHiRes, ownerExecuteMode)
import System.Posix.Types (FileMode)
import System.Process (readProcessWithExitCode)
import Text.Printf (printf)
import Text.Read (readMaybe)

import Handler.Cli.Common
import Handler.Config (Config(..), configDefaultPath, defaultResourceUrl, emptyConfig, experimentalCostDisplay, isServiceConfigured, readConfig)
import Handler.Db (Db(..), close, defaultPath, handlerHome)
import Handler.Db.Cost (CostSnapshot(..), DailyCost(..), getCostSnapshot, getDailyCostForSession, getTotalAdjustment, insertCostAdjustment, queryTotalCost, upsertCostSnapshot, upsertDailyCost)
import Handler.Db.Cursors (advanceCursor, autoDeliveredCount, autoDeliveredCountAll, getCursor)
import Handler.Db.Events (directCountForSession, globalUnreadCountForSession, unreadCountForSession, unreadResourcesForSession)
import Handler.Db.Sessions (Session(..), bumpLastActive, getSession, listSessions, listSessionsByName, upsertSession)
import Handler.Db.Subscriptions (Subscription(..), listSubscriptions, restoreSubscriptionsForSession, subscribeIfNew)
import Handler.Db.WatcherStatus (hasWatcherError)
import Handler.Discover (cleanStalePidCaches, isSessionProcess, writePidCache)
import Handler.Git (GitStatus(..), getStatus)
import Handler.Terminal (Backend(..), detect, newBackend)
import Handler.Util (newUuid, nowIso)
import Handler.Watcher.Framework (eventTypeDisplayName)
import Handler.Worktree (Resource(..), parseResourceId, readResources)
import Handler.Cmd.Peek (findSessionsAwaitingApproval)

-- ANSI color constants
colorCyan, colorYellow, colorBoldYellow, colorGreen, colorRed, colorPurple,
  colorBlue, colorHint, colorBoldWhite, colorBoldGreen, colorDim,
  colorDimGreen, colorClaudeOrange, colorUnderline, colorReset, colorBold :: Text
colorCyan         = "\ESC[36m"
colorYellow       = "\ESC[33m"
colorBoldYellow   = "\ESC[1;33m"
colorGreen        = "\ESC[32m"
colorRed          = "\ESC[31m"
colorPurple       = "\ESC[35m"
colorBlue         = "\ESC[34m"
colorHint         = "\ESC[35m"
colorBoldWhite    = "\ESC[1;37m"
colorBoldGreen    = "\ESC[1;32m"
colorDim          = "\ESC[2m"
colorDimGreen     = "\ESC[2;32m"
colorClaudeOrange = "\ESC[38;2;218;119;86m"
colorUnderline    = "\ESC[4m"
colorReset        = "\ESC[0m"
colorBold         = "\ESC[1m"

-- | The JSON passed on stdin by Claude Code's statusline hook.
data HookInput = HookInput
  { hiSessionId        :: Text
  , hiSessionName      :: Text
  , hiTranscriptPath   :: Text
  , hiCwd              :: Text
  , hiModelId          :: Text
  , hiModelDisplayName :: Text
  , hiUsedPercentage   :: Int
  , hiInputTokens      :: Int
  , hiOutputTokens     :: Int
  , hiTotalCostUsd     :: Double
  }

instance A.FromJSON HookInput where
  parseJSON = A.withObject "HookInput" $ \o -> do
    model <- o .:? "model" .!= A.Object KM.empty
    ctx <- o .:? "context_window" .!= A.Object KM.empty
    cost <- o .:? "cost" .!= A.Object KM.empty
    HookInput
      <$> o .:? "session_id" .!= ""
      <*> o .:? "session_name" .!= ""
      <*> o .:? "transcript_path" .!= ""
      <*> o .:? "cwd" .!= ""
      <*> nested model "id" ""
      <*> nested model "display_name" ""
      <*> nestedNum ctx "used_percentage"
      <*> nestedNum ctx "total_input_tokens"
      <*> nestedNum ctx "total_output_tokens"
      <*> nestedDouble cost "total_cost_usd"
    where
      nested (A.Object o') k dflt = o' .:? k .!= dflt
      nested _ _ dflt = pure dflt
      nestedNum (A.Object o') k = o' .:? k .!= 0
      nestedNum _ _ = pure (0 :: Int)
      nestedDouble (A.Object o') k = o' .:? k .!= 0
      nestedDouble _ _ = pure (0 :: Double)

data StatuslineOpts = StatuslineOpts
  { sSession  :: Text
  , sFromHook :: Bool
  }

statuslineCommand :: Mod CommandFields NamedCommand
statuslineCommand = mkCommand "statusline" "Output statusline info for a session" (runStatusline <$> opts)
  where
    opts = StatuslineOpts
      <$> strOption (long "session" <> value "" <> metavar "ID" <> help "session ID")
      <*> switch (long "from-hook" <> help "read session data from stdin JSON (statusline hook mode)")

runStatusline :: StatuslineOpts -> Ctx -> IO ()
runStatusline o ctx
  | o.sFromHook = runStatuslineFromHook ctx
  | o.sSession == "" = dieText "either --session or --from-hook is required"
  | otherwise = runStatuslineDirect o.sSession

tshow :: Show a => a -> Text
tshow = T.pack . show

fmt2 :: Double -> Text
fmt2 x = T.pack (printf "%.2f" x)

-- | Tracks reported cost deltas into snapshots and the daily rollup;
-- restarts (reported cost below snapshot) are recorded as adjustments.
recordCostSnapshot :: Db -> HookInput -> IO ()
recordCostSnapshot wd input = do
  now <- nowIso
  today <- T.pack . formatTime defaultTimeLocale "%Y-%m-%d" <$> getCurrentTime
  let sid = input.hiSessionId
      reportedCost = input.hiTotalCostUsd
      reportedInput = input.hiInputTokens
      reportedOutput = input.hiOutputTokens
      newSnap = CostSnapshot
        { sessionId = sid
        , reportedCostUsd = reportedCost
        , totalInputTokens = reportedInput
        , totalOutputTokens = reportedOutput
        , model = Just input.hiModelId
        , updatedAt = now
        }
  msnap <- getCostSnapshot wd sid
  case msnap of
    Nothing -> do
      upsertCostSnapshot wd newSnap
      when (reportedCost > 0) $
        upsertDailyCost wd sid today reportedCost reportedInput reportedOutput
    Just snap
      | reportedCost == snap.reportedCostUsd -> pure ()
      | otherwise -> do
          (costDelta, inputDelta, outputDelta) <-
            if reportedCost < snap.reportedCostUsd
              then do
                insertCostAdjustment wd sid snap.reportedCostUsd "restart_reset" now
                pure (reportedCost, reportedInput, reportedOutput)
              else pure ( reportedCost - snap.reportedCostUsd
                        , reportedInput - snap.totalInputTokens
                        , reportedOutput - snap.totalOutputTokens )
          upsertCostSnapshot wd newSnap
          when (costDelta > 0) $
            upsertDailyCost wd sid today costDelta inputDelta outputDelta

-- | Hook mode: read stdin JSON and produce the complete statusline.
runStatuslineFromHook :: Ctx -> IO ()
runStatuslineFromHook _ = do
  raw <- BL.getContents
  input <- case A.eitherDecode raw of
    Left err -> dieText ("failed to parse stdin JSON: " <> T.pack err)
    Right i -> pure i
  when (input.hiSessionId == "") $ dieText "no session_id in stdin"

  -- Brief writable connection for heartbeat + cost tracking + registration
  wdResult <- try openDb :: IO (Either IOException Db)
  case wdResult of
    Left _ -> pure ()
    Right wd -> do
      now <- nowIso
      existing <- getSession wd input.hiSessionId
      case existing of
        Nothing -> registerSessionFromHook wd input
        Just s | s.status == "archived" -> reactivateSession wd input s
        Just _ -> bumpLastActive wd input.hiSessionId now
      (termType, termId, workspaceId) <- detect
      pid <- claudePid
      syncSessionMetadata wd input.hiSessionId input.hiSessionName pid termType termId workspaceId input.hiCwd
      recordCostSnapshot wd input
      close wd

  d <- openReadOnlyDb
  msession <- getSession d input.hiSessionId
  case msession of
    Just session | session.status /= "archived" -> do
      let isHandler = session.role == "handler"
      cfg <- readConfigSafe

      -- Cost display values (only when experimental cost display is enabled)
      (trueCost, todayCost) <-
        if experimentalCostDisplay cfg && input.hiTotalCostUsd > 0
          then do
            adj <- getTotalAdjustment d input.hiSessionId
            today <- T.pack . formatTime defaultTimeLocale "%Y-%m-%d" <$> getCurrentTime
            mdc <- getDailyCostForSession d input.hiSessionId today
            pure (input.hiTotalCostUsd + adj, maybe 0 (.costUsd) mdc)
          else pure (input.hiTotalCostUsd, 0)

      -- Parallel expensive lookups, mirroring the Go goroutines
      (gitStatus, (awaitingNames, unreadSessionNames)) <-
        concurrently
          (if isHandler then pure Nothing else Just <$> getStatus (T.unpack input.hiCwd))
          (concurrently
            (scanAwaitingApproval d session.sessionId)
            (do unreads <- findSessionsWithUnreads d session.sessionId
                pure [ if s.sessionName == "" then T.take 8 s.sessionId else s.sessionName
                     | s <- unreads ]))

      if isHandler
        then renderHandlerStatusline d session cfg (Just input) trueCost todayCost awaitingNames unreadSessionNames
        else renderWorkerStatusline d session cfg (Just input) trueCost todayCost gitStatus awaitingNames unreadSessionNames

      when cfg.debug $ renderDebugInfo d session (Just input)
    _ -> putTextLn "Session not registered with handler. Say hello to register."
  close d

-- | Legacy --session mode for direct CLI use (no git, no model line).
runStatuslineDirect :: Text -> IO ()
runStatuslineDirect sid = do
  d <- openReadOnlyDb
  msession <- getSession d sid
  case msession of
    Just session | session.status /= "archived" -> do
      cfg <- readConfigSafe
      if session.role == "handler"
        then do
          awaitingNames <- scanAwaitingApproval d session.sessionId
          renderHandlerStatusline d session cfg Nothing 0 0 awaitingNames []
        else renderWorkerStatusline d session cfg Nothing 0 0 Nothing [] []
    _ -> putTextLn "not registered"
  close d

readConfigSafe :: IO Config
readConfigSafe = do
  path <- configDefaultPath
  result <- try (readConfig path) :: IO (Either IOException Config)
  pure (either (const emptyConfig) id result)

-- | Display names of sessions needing input (excluding self).
scanAwaitingApproval :: Db -> Text -> IO [Text]
scanAwaitingApproval d selfSessionId = do
  awaiting <- findSessionsAwaitingApproval d
  pure [ if s.sessionName == "" then T.take 8 s.sessionId else s.sessionName
       | s <- awaiting, s.sessionId /= selfSessionId ]

-- | The complete statusline for a regular session.
renderWorkerStatusline :: Db -> Session -> Config -> Maybe HookInput -> Double -> Double -> Maybe GitStatus -> [Text] -> [Text] -> IO ()
renderWorkerStatusline d session cfg input trueCost todayCost gs awaitingNames unreadSessionNames = do
  renderDuplicateNameWarning d session

  -- Line 1: Git status
  case gs of
    Just g | g.inGit -> putTextLn (formatGitLine g)
    _ -> pure ()

  -- Line 2: Model/context/cost
  case input of
    Just i | i.hiModelDisplayName /= "" -> putTextLn (formatModelLine i trueCost todayCost)
    _ -> pure ()

  -- Handler lines (inbox, inbox-mode, watching)
  (unreadCount, unreadMsg) <- renderInboxLine d session False
  renderAutoDeliveredLine d session
  renderInboxModeLine session
  renderWatchingLine d session cfg False

  dispatchNotification session unreadCount unreadMsg

  shortcuts <- if session.terminalType == "cmux" then getCmuxShortcuts else pure Nothing
  when (not (null awaitingNames) || not (null unreadSessionNames)) $ do
    printfT [colorDim, "⠀", colorReset]
    renderAwaitingLine awaitingNames shortcuts
    renderUnreadSessionsLine unreadSessionNames shortcuts
    printfT [colorDim, "⠀", colorReset]

  when (session.terminalType == "cmux") $ renderCmuxShortcutsLine shortcuts
  printfT [ colorDim, "Use ", colorHint, "/done", colorReset, colorDim
          , " before closing the session to log a summary", colorReset ]

-- | The complete statusline for a handler session.
renderHandlerStatusline :: Db -> Session -> Config -> Maybe HookInput -> Double -> Double -> [Text] -> [Text] -> IO ()
renderHandlerStatusline d session cfg input trueCost todayCost awaitingNames unreadSessionNames = do
  renderDuplicateNameWarning d session

  sessions <- listSessions d False 1000 0
  activeCount <- countAlive [ s | s <- sessions, s.status == "active", s.sessionId /= session.sessionId ]

  blockedRows <- SQL.query_ d.conn
    "SELECT COUNT(*) FROM (\
    \  SELECT s.session_id\
    \  FROM sessions s\
    \  JOIN events e ON e.session_id = s.session_id AND e.type = 'blocked'\
    \  WHERE s.status = 'active'\
    \    AND NOT EXISTS (\
    \      SELECT 1 FROM events e2\
    \      WHERE e2.session_id = s.session_id AND e2.type = 'unblocked' AND e2.ts > e.ts\
    \    )\
    \)"
  let blockedCount = case blockedRows of { (SQL.Only n : _) -> n; _ -> 0 :: Int }

  -- Line 1: Sessions overview
  printfT [ colorPurple, "[Handler]", colorReset, " ", colorBold, "Sessions", colorReset, ": "
          , tshow activeCount, " active, ", tshow blockedCount, " blocked "
          , colorDim, "· ", colorHint, "/handler", colorReset, " ", colorDim, "to summarize all sessions", colorReset ]

  case input of
    Just i | i.hiModelDisplayName /= "" -> putTextLn (formatModelLine i trueCost todayCost)
    _ -> pure ()

  -- Aggregate cost line (experimental)
  when (experimentalCostDisplay cfg) $ do
    now <- getCurrentTime
    let (y, m, _) = toGregorian (utctDay now)
        today = T.pack (formatTime defaultTimeLocale "%Y-%m-%d" now)
        pad2 n = T.justifyRight 2 '0' (tshow n)
        monthStart = tshow y <> "-" <> pad2 m <> "-01"
        monthEnd = tshow y <> "-" <> pad2 m <> "-" <> pad2 (gregorianMonthLength y m)
        (ly, lm) = if m == 1 then (y - 1, 12) else (y, m - 1)
        lastMonthStart = tshow ly <> "-" <> pad2 lm <> "-01"
        lastMonthEnd = tshow ly <> "-" <> pad2 lm <> "-" <> pad2 (gregorianMonthLength ly lm)
        lastMonthName = T.pack (monthAbbrev lm)
    (todayTotal, _, _) <- queryTotalCost d today today
    (monthTotal, _, _) <- queryTotalCost d monthStart monthEnd
    (lastMonthTotal, _, _) <- queryTotalCost d lastMonthStart lastMonthEnd
    when (todayTotal > 0 || monthTotal > 0) $
      printfT [ colorBoldWhite, "Cost (all sessions)", colorReset, ": $", fmt2 todayTotal
              , " today · $", fmt2 monthTotal, " this month · $", fmt2 lastMonthTotal, " ", lastMonthName ]

  (unreadCount, unreadMsg) <- renderInboxLine d session True
  renderAutoDeliveredLine d session
  renderInboxModeLine session
  renderWatchingLine d session cfg True

  dispatchNotification session unreadCount unreadMsg

  shortcuts <- if session.terminalType == "cmux" then getCmuxShortcuts else pure Nothing
  when (not (null awaitingNames) || not (null unreadSessionNames)) $ do
    printfT [colorDim, "⠀", colorReset]
    renderAwaitingLine awaitingNames shortcuts
    renderUnreadSessionsLine unreadSessionNames shortcuts
    printfT [colorDim, "⠀", colorReset]

  when (session.terminalType == "cmux") $ renderCmuxShortcutsLine shortcuts
  where
    countAlive ss = length <$> filterAliveSessions ss
    filterAliveSessions [] = pure []
    filterAliveSessions (s : rest) = do
      ok <- if s.pid > 0 then isSessionProcess s.pid s.sessionId else pure True
      others <- filterAliveSessions rest
      pure (if ok then s : others else others)

monthAbbrev :: Int -> String
monthAbbrev m =
  ["Jan","Feb","Mar","Apr","May","Jun","Jul","Aug","Sep","Oct","Nov","Dec"] !! (m - 1)

renderAwaitingLine :: [Text] -> Maybe CmuxShortcuts -> IO ()
renderAwaitingLine [] _ = pure ()
renderAwaitingLine awaitingNames shortcuts = do
  let count = length awaitingNames
      label = if count > 1 then "sessions" else "session"
  case shortcuts of
    Just sc | sc.switchToAwaiting /= "" ->
      printfT [ colorBoldYellow, tshow count, " other ", label, " awaiting approval", colorReset, " "
              , colorDim, "· ", colorHint, sc.switchToAwaiting, colorReset <> colorDim, " to switch", colorReset ]
    _ ->
      printfT [ colorBoldYellow, tshow count, " other ", label, " awaiting approval", colorReset ]
  printfT [ colorDim, "  ↳ ", colorBoldYellow, formatNameList awaitingNames 5, colorReset ]

renderUnreadSessionsLine :: [Text] -> Maybe CmuxShortcuts -> IO ()
renderUnreadSessionsLine [] _ = pure ()
renderUnreadSessionsLine unreadSessionNames shortcuts = do
  let count = length unreadSessionNames
      label = if count > 1 then "sessions" else "session"
  case shortcuts of
    Just sc | sc.switchToUnread /= "" ->
      printfT [ colorCyan, tshow count, " other ", label, " with unread messages", colorReset, " "
              , colorDim, "· ", colorHint, sc.switchToUnread, colorReset <> colorDim, " to switch", colorReset ]
    _ ->
      printfT [ colorCyan, tshow count, " other ", label, " with unread messages", colorReset ]
  printfT [ colorDim, "  ↳ ", colorCyan, formatNameList unreadSessionNames 5, colorReset ]

renderCmuxShortcutsLine :: Maybe CmuxShortcuts -> IO ()
renderCmuxShortcutsLine Nothing =
  printfT [ colorDim, "Run ", colorHint, "handler setup", colorReset, colorDim
          , " from within cmux to set up keyboard shortcuts", colorReset ]
renderCmuxShortcutsLine (Just sc) = do
  let parts = concat
        [ [ colorHint <> sc.switchToSession <> colorReset <> colorDim <> " to switch sessions"
          | sc.switchToSession /= "" ]
        , [ colorHint <> sc.focusBack <> colorReset <> colorDim <> " and "
            <> colorHint <> sc.focusForward <> colorReset <> colorDim <> " for focus back and forward"
          | sc.focusBack /= "" && sc.focusForward /= "" ]
        ]
  unless (null parts) $
    printfT [ colorDim, T.intercalate " · " parts, colorReset ]

-- --- Shared rendering helpers ---

formatGitLine :: GitStatus -> Text
formatGitLine gs =
  let branchPart
        | gs.rebasing = colorYellow <> "rebasing" <> colorReset <> " " <> colorBoldWhite <> gs.branch <> colorReset
        | gs.branch == gs.defaultBranch = "on " <> colorBoldWhite <> gs.branch <> colorReset
        | otherwise = colorBoldWhite <> gs.branch <> colorReset

      aheadParts =
        [ colorGreen <> "↑" <> tshow gs.ahead <> colorReset
          <> (if gs.committedAdds > 0 || gs.committedDels > 0
                then " (" <> colorGreen <> "+" <> tshow gs.committedAdds <> colorReset
                     <> " " <> colorRed <> "−" <> tshow gs.committedDels <> colorReset <> ")"
                else "")
        | gs.ahead > 0 ]

      dirty = gs.modified + gs.untracked
      dirtyPart
        | dirty > 0 =
            let dirtyParts = concat
                  [ [colorYellow <> tshow gs.modified <> " modified" <> colorReset | gs.modified > 0]
                  , [colorYellow <> tshow gs.untracked <> " untracked" <> colorReset | gs.untracked > 0]
                  ]
                base = T.intercalate ", " dirtyParts
            in base <> (if gs.uncommittedAdds > 0 || gs.uncommittedDels > 0
                          then " (" <> colorGreen <> "+" <> tshow gs.uncommittedAdds <> colorReset
                               <> " " <> colorRed <> "−" <> tshow gs.uncommittedDels <> colorReset <> ")"
                          else "")
        | otherwise = colorDimGreen <> "clean" <> colorReset

      behindParts =
        [ colorDim <> "↓" <> tshow gs.behind <> " behind " <> gs.defaultBranch <> colorReset
        | gs.behind > 0 ]

      parts = [branchPart] ++ aheadParts ++ [dirtyPart] ++ behindParts
  in case (gs.ahead > 0, parts) of
       (True, p0 : p1 : rest) ->
         p0 <> " " <> p1 <> (if null rest then "" else " · " <> T.intercalate " " rest)
       (_, p0 : rest) ->
         p0 <> (if null rest then "" else " · " <> T.intercalate " " rest)
       _ -> ""

formatModelLine :: HookInput -> Double -> Double -> Text
formatModelLine input trueCost todayCost =
  let pct = input.hiUsedPercentage
      filled = pct * 20 `div` 100
      empty = 20 - filled
      bar = T.replicate filled "▓" <> T.replicate empty "░"
      barColor
        | pct >= 80 = colorRed
        | pct >= 50 = colorYellow
        | otherwise = colorGreen
      costStr = "$" <> fmt2 trueCost
                <> (if todayCost > 0 then " ($" <> fmt2 todayCost <> " today)" else "")
  in colorClaudeOrange <> input.hiModelDisplayName <> colorReset <> " "
     <> barColor <> bar <> colorReset <> " "
     <> tshow pct <> "% ctx " <> colorDim <> "· " <> costStr <> colorReset

-- | Prints the inbox line; returns (unread count, notification message).
renderInboxLine :: Db -> Session -> Bool -> IO (Int, Text)
renderInboxLine d session global = do
  (unreadCount, breakdown) <-
    if global
      then globalUnreadCountForSession d session.sessionId
      else unreadCountForSession d session.sessionId
  directCount <- directCountForSession d session.sessionId

  notifyMsg <-
    if unreadCount == 0
      then do
        let noMsgLabel = if global then "No new events" else "No new messages"
        TIO.putStr $ T.concat
          [ colorHint, "/inbox", colorReset, ": ", noMsgLabel, " ", colorDim, "· "
          , colorDim, colorHint, "/message", colorReset, colorDim, " to talk to other sessions", colorReset ]
        pure ""
      else do
        let breakdownParts =
              [ tshow count <> " " <> eventTypeDisplayName eventType
              | (eventType, count) <- Map.toList breakdown ]
            breakdownStr =
              if null breakdownParts then ""
              else " (" <> T.intercalate ", " breakdownParts <> ")"
        TIO.putStr $ T.concat
          [ "📬 ", colorCyan, "/inbox", colorReset, ": ", colorYellow, "● "
          , tshow unreadCount, " unread", colorReset, breakdownStr ]
        pure (tshow unreadCount <> " unread" <> breakdownStr)

  when (directCount > 0) $
    TIO.putStr $ T.concat
      [ " ", colorDim, "·", colorReset, " ", colorYellow, "● ", tshow directCount, " direct", colorReset ]
  when (unreadCount > 0) $
    TIO.putStr $ T.concat
      [ " ", colorDim, "· ", colorHint, "/inbox-clear", colorReset, colorDim, " to dismiss", colorReset ]
  putTextLn ""
  pure (unreadCount, notifyMsg)

renderAutoDeliveredLine :: Db -> Session -> IO ()
renderAutoDeliveredLine d session =
  when (session.inboxMode == "auto") $ do
    autoCount <-
      if session.role == "handler"
        then autoDeliveredCountAll d session.sessionId
        else autoDeliveredCount d session.sessionId
    when (autoCount > 0) $ do
      let msgWord = if autoCount == 1 then "message" else "messages"
      printfT [ colorYellow, "  ● ", tshow autoCount, " ", msgWord, " auto-delivered", colorReset
              , " ", colorDim, "· ", colorHint, "/catchup", colorReset, " ", colorDim, "for a summary", colorReset ]

renderInboxModeLine :: Session -> IO ()
renderInboxModeLine session = do
  let renderMode mode
        | session.inboxMode == mode = colorBoldGreen <> mode <> colorReset
        | otherwise = colorDim <> mode <> colorReset
      rendered = T.intercalate (colorDim <> " | " <> colorReset)
        (map renderMode ["manual", "on-submit", "auto"])
  printfT [ colorHint, "/inbox-mode", colorReset, ": ", rendered ]

renderWatchingLine :: Db -> Session -> Config -> Bool -> IO ()
renderWatchingLine d session cfg global = do
  (prCount, jiraCount, unreadResources, breakdown) <-
    if global
      then do
        rows <- SQL.query_ d.conn
          "SELECT resource_type, COUNT(*) as count\
          \ FROM subscriptions WHERE deleted_at IS NULL GROUP BY resource_type"
        let counts = Map.fromList (rows :: [(Text, Int)])
        pure ( Map.findWithDefault 0 "pr" counts
             , Map.findWithDefault 0 "jira" counts
             , Set.empty, Map.empty )
      else do
        subs <- listSubscriptions d session.sessionId False
        (_, breakdown) <- unreadCountForSession d session.sessionId
        unreadResources <- unreadResourcesForSession d session.sessionId
        pure ( length [() | s <- subs, s.resourceType == "pr"]
             , length [() | s <- subs, s.resourceType == "jira"]
             , unreadResources, breakdown )

  let prTypes = [ "pr_comment", "pr_review_comment", "pr_review_requested", "pr_approved"
                , "pr_closed", "pr_merged", "pr_reopened", "pr_new_commits"
                , "ci_check_passed", "ci_check_failed" ]
      jiraTypes = [ "jira_comment", "jira_status_change", "jira_assigned"
                  , "jira_description_changed", "jira_labels_changed" ]
      breakdownTypes = Map.keys breakdown
      prUnread = not global &&
        (any (`elem` prTypes) breakdownTypes
         || ("watcher_error" `elem` breakdownTypes && prCount > 0))
      jiraUnread = not global &&
        (any (`elem` jiraTypes) breakdownTypes
         || ("watcher_error" `elem` breakdownTypes && jiraCount > 0))

      subParts = concat
        [ [ let label = if prCount > 1 then tshow prCount <> " PRs" else "1 PR"
            in if prUnread then colorYellow <> "● " <> label <> colorReset else label
          | prCount > 0 ]
        , [ let label = if jiraCount > 1 then tshow jiraCount <> " Jira" else "1 Jira"
            in if jiraUnread then colorYellow <> "● " <> label <> colorReset else label
          | jiraCount > 0 ]
        ]
      subSummary =
        if null subParts
          then colorDim <> "no active subscriptions" <> colorReset
          else T.intercalate ", " subParts

  -- Watcher status
  services <- fmap concat $ mapM (\svc ->
    if isServiceConfigured cfg svc
      then do
        installed <- watcherIsInstalled svc
        if installed
          then do
            mago <- watcherLastRunAgo svc
            hasErr <- hasWatcherError d svc
            let ago = maybe "" (\a -> " (" <> a <> " ago)") mago
                mark = if hasErr then colorRed <> "✗" <> colorReset else colorGreen <> "✓" <> colorReset
            pure [mark <> colorDim <> " " <> svc <> ago]
          else pure []
      else pure []) ["github", "jira"]
  let watcherStatus = if null services then "" else " · " <> T.intercalate " " services

  if global
    then printfT [ colorHint, "/watching", colorReset, ": ", subSummary, colorDim, watcherStatus, colorReset ]
    else do
      subs <- listSubscriptions d session.sessionId False
      if not (null subs)
        then do
          let sorted = sortOn (\sub -> not (Set.member (sub.resourceType <> ":" <> sub.resourceId) unreadResources)) subs
              links =
                [ let label = shortResourceLabel sub.resourceType sub.resourceId
                      resKey = sub.resourceType <> ":" <> sub.resourceId
                      hasUnread = Set.member resKey unreadResources
                      url0 = fromMaybe "" sub.resourceUrl
                      url = if url0 == "" then defaultResourceUrl cfg sub.resourceType sub.resourceId else url0
                      linkColor = if hasUnread then colorYellow else colorBlue
                  in if url /= ""
                       then linkColor <> colorUnderline <> "\ESC]8;;" <> url <> "\ESC\\" <> label <> "\ESC]8;;\ESC\\" <> colorReset
                       else linkColor <> label <> colorReset
                | sub <- sorted ]
              resourceLinks = T.intercalate (colorDim <> ", " <> colorReset) links
          if length subs <= 2
            then printfT [ colorHint, "/watching", colorReset, ": ", subSummary
                         , " ", colorDim, "| ", colorReset, resourceLinks
                         , colorDim, watcherStatus, colorReset ]
            else do
              printfT [ colorHint, "/watching", colorReset, ": ", subSummary, colorDim, watcherStatus, colorReset ]
              printfT [ colorDim, "  ↳ ", colorReset, resourceLinks ]
        else printfT [ colorHint, "/watching", colorReset, ": ", subSummary
                     , " ", colorDim, "· ", colorHint, "/watch", colorReset, colorDim
                     , " to follow PRs or Jira issues", colorReset
                     , colorDim, watcherStatus, colorReset ]

-- | A compact label for a resource (\"#123\" for PRs).
shortResourceLabel :: Text -> Text -> Text
shortResourceLabel resourceType resourceId
  | resourceType == "pr"
  , (before, matched) <- T.breakOnEnd "#" resourceId
  , not (T.null before)
  = "#" <> matched
  | otherwise = resourceId

formatNameList :: [Text] -> Int -> Text
formatNameList names maxN
  | length names <= maxN = T.intercalate ", " names
  | otherwise = T.intercalate ", " (take maxN names)
                <> ", +" <> tshow (length names - maxN) <> " more"

renderDuplicateNameWarning :: Db -> Session -> IO ()
renderDuplicateNameWarning d session =
  when (session.sessionName /= "") $ do
    dupes <- listSessionsByName d session.sessionName
    when (length dupes > 1) $ do
      printfT [ "\ESC[1;31m", tshow (length dupes), " sessions are active with the name "
              , tshow session.sessionName, ". You may have resumed multiple times and caused a fork.\ESC[0m" ]
      forM_ dupes $ \s -> do
        promptInfo <-
          if s.lastPrompt == ""
            then pure "no prompts yet"
            else do
              mago <- agoFromIso s.lastPrompt
              pure (maybe "no prompts yet" ("last prompt " <>) ((<> " ago") <$> mago))
        if s.sessionId == session.sessionId
          then printfT [ colorDim, "  ↳ This session: ", s.sessionId, " (", promptInfo
                       , ", pid ", tshow s.pid, ")", colorReset ]
          else printfT [ colorDim, "  ↳ ", s.sessionId, " (", promptInfo
                       , ", pid ", tshow s.pid, ")", colorReset ]
      printfT [colorDim, "⠀", colorReset]

renderDebugInfo :: Db -> Session -> Maybe HookInput -> IO ()
renderDebugInfo d session input = do
  cursor <- getCursor d session.sessionId
  let peekable = if session.terminalType /= "" && session.terminalId /= "" then "yes" else "no"
  state <-
    if session.status == "active"
      then do
        running <- isSessionProcess session.pid session.sessionId
        pure (if running then session.status else "dead")
      else pure session.status
  printfT [colorDim, "—", colorReset]
  printfT [ colorDim, "[debug] id=", T.take 12 session.sessionId, " name=", tshow session.sessionName
          , " state=", state, " pid=", tshow session.pid, colorReset ]
  printfT [ colorDim, "[debug] terminal=", session.terminalType, ":", session.terminalId
          , " peekable=", peekable, colorReset ]
  printfT [ colorDim, "[debug] workspace=", tshow session.cmuxWorkspaceName
          , " id=", session.cmuxWorkspaceId, colorReset ]
  printfT [ colorDim, "[debug] role=", session.role, " cursor=", cursor, colorReset ]
  forM_ input $ \i ->
    printfT [ colorDim, "[debug] reported_cost=$", fmt2 i.hiTotalCostUsd, colorReset ]
  printfT [colorDim, "—", colorReset]

-- --- Hook-mode registration ---

gitOut :: FilePath -> [String] -> IO (Maybe Text)
gitOut cwd args = do
  result <- try (readProcessWithExitCode "git" (["-C", cwd] ++ args) "")
  pure $ case result :: Either IOException (ExitCode, String, String) of
    Right (ExitSuccess, out, _) -> Just (T.strip (T.pack out))
    _ -> Nothing

registerSessionFromHook :: Db -> HookInput -> IO ()
registerSessionFromHook d input = do
  cwd <- if input.hiCwd == "" then T.pack <$> getCurrentDirectory else pure input.hiCwd
  branch <- fromMaybe "unknown" <$> gitOut (T.unpack cwd) ["rev-parse", "--abbrev-ref", "HEAD"]
  repo <- do
    morigin <- gitOut (T.unpack cwd) ["remote", "get-url", "origin"]
    pure $ case morigin of
      Just r | (_, after) <- T.breakOn "github.com" r, not (T.null after) ->
        let r1 = T.drop (T.length "github.com") after
            r2 = fromMaybe r1 (T.stripPrefix ":" r1)
            r3 = fromMaybe r2 (T.stripPrefix "/" r2)
        in fromMaybe r3 (T.stripSuffix ".git" r3)
      _ -> "unknown"

  (termType, termId, workspaceId) <- detect
  existingCursor <- getCursor d input.hiSessionId
  now <- nowIso
  pid <- claudePid
  upsertSession d Session
    { sessionId = input.hiSessionId
    , harness = "claude-code"
    , repo = repo
    , branch = branch
    , sessionName = input.hiSessionName
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
    , cwd = cwd
    , registeredAt = now
    , jsonlPath = input.hiTranscriptPath
    }

  dbPath <- defaultPath
  let sessionsDir = takeDirectory dbPath </> "sessions"
  createDirectoryIfMissing True sessionsDir
  writePidCache sessionsDir pid input.hiSessionId

  -- Only initialize cursor for brand new sessions. Re-registered sessions
  -- keep their old cursor so queued inbox messages aren't lost.
  let isNewSession = existingCursor == ""
  when isNewSession $ do
    advanceCursor d input.hiSessionId now

    -- Auto-subscribe from .worktree-resources (first registration only)
    resources <- readResources (T.unpack cwd </> ".worktree-resources")
    unless (null resources) $ do
      cfg <- readConfigSafe
      forM_ resources $ \r -> do
        let (resourceType, resourceId) = parseResourceId r.resId
        unless (resourceType == "") $ do
          let url0 = r.resUrl
              url = if url0 == "" then defaultResourceUrl cfg resourceType resourceId else url0
          subId <- newUuid
          subscribeIfNew d Subscription
            { subId = subId
            , sessionId = input.hiSessionId
            , resourceType = resourceType
            , resourceId = resourceId
            , resourceUrl = if url == "" then Nothing else Just url
            , createdAt = now
            , deletedAt = Nothing
            }

  -- Migrate subscriptions and cursor from archived session with same name
  when (input.hiSessionName /= "") $
    migrateSubscriptionsFromArchived d input.hiSessionId input.hiSessionName
  when (isNewSession && input.hiSessionName /= "") $
    migrateOldCursor d input.hiSessionId input.hiSessionName

  _ <- cleanStalePidCaches sessionsDir
  pure ()

reactivateSession :: Db -> HookInput -> Session -> IO ()
reactivateSession d input existing = do
  now <- nowIso
  (termType, termId, workspaceId) <- detect
  cwd <- if input.hiCwd == "" then T.pack <$> getCurrentDirectory else pure input.hiCwd
  branch <- fromMaybe existing.branch <$> gitOut (T.unpack cwd) ["rev-parse", "--abbrev-ref", "HEAD"]
  pid <- claudePid
  upsertSession d Session
    { sessionId = input.hiSessionId
    , harness = "claude-code"
    , repo = existing.repo
    , branch = branch
    , sessionName = input.hiSessionName
    , pid = pid
    , status = "active"
    , inboxMode = existing.inboxMode
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
    , registeredAt = existing.registeredAt
    , jsonlPath = input.hiTranscriptPath
    }

  restoreSubscriptionsForSession d input.hiSessionId

  dbPath <- defaultPath
  let sessionsDir = takeDirectory dbPath </> "sessions"
  createDirectoryIfMissing True sessionsDir
  writePidCache sessionsDir pid input.hiSessionId

migrateSubscriptionsFromArchived :: Db -> Text -> Text -> IO ()
migrateSubscriptionsFromArchived d newSessionId sessionName = do
  activeCounts <- SQL.query d.conn
    "SELECT COUNT(*) FROM sessions WHERE session_name = ? AND status = 'active'"
    (SQL.Only sessionName)
  let activeCount = case activeCounts of { (SQL.Only n : _) -> n; _ -> 0 :: Int }
  existingSubs <- listSubscriptions d newSessionId False
  when (activeCount <= 1 && null existingSubs) $ do
    archivedIds <- SQL.query d.conn
      "SELECT session_id FROM sessions\
      \ WHERE session_name = ? AND status = 'archived' AND session_id != ?\
      \ ORDER BY last_active DESC LIMIT 1"
      (sessionName, newSessionId)
    case archivedIds of
      (SQL.Only archivedId : _) | archivedId /= ("" :: Text) -> do
        rows <- SQL.query d.conn
          "SELECT resource_type, resource_id, resource_url\
          \ FROM subscriptions WHERE session_id = ? ORDER BY created_at DESC"
          (SQL.Only archivedId)
        now <- nowIso
        forM_ (rows :: [(Text, Text, Maybe Text)]) $ \(resType, resId, resUrl) -> do
          subId <- newUuid
          subscribeIfNew d Subscription
            { subId = subId
            , sessionId = newSessionId
            , resourceType = resType
            , resourceId = resId
            , resourceUrl = resUrl
            , createdAt = now
            , deletedAt = Nothing
            }
      _ -> pure ()

migrateOldCursor :: Db -> Text -> Text -> IO ()
migrateOldCursor d newSessionId sessionName = do
  activeCounts <- SQL.query d.conn
    "SELECT COUNT(*) FROM sessions WHERE session_name = ? AND status = 'active'"
    (SQL.Only sessionName)
  let activeCount = case activeCounts of { (SQL.Only n : _) -> n; _ -> 0 :: Int }
  when (activeCount <= 1) $ do
    cursors <- SQL.query d.conn
      "SELECT sc.last_seen_ts\
      \ FROM session_cursors sc\
      \ JOIN sessions s ON sc.session_id = s.session_id\
      \ WHERE s.session_name = ? AND s.status = 'archived' AND s.session_id != ?\
      \ ORDER BY s.last_active DESC LIMIT 1"
      (sessionName, newSessionId)
    case cursors of
      (SQL.Only oldCursor : _) | oldCursor /= ("" :: Text) ->
        advanceCursor d newSessionId oldCursor
      _ -> pure ()

-- --- Terminal notification (private copy of cmd/notify.go's dispatcher;
-- the notify command owns the canonical port) ---

notifiedCountPath :: Text -> IO FilePath
notifiedCountPath sessionId = do
  home <- handlerHome
  pure (home </> "sessions" </> (T.unpack sessionId <> ".notified_count"))

dispatchNotification :: Session -> Int -> Text -> IO ()
dispatchNotification session unreadCount message
  | unreadCount == 0 = do
      countFile <- notifiedCountPath session.sessionId
      _ <- try (removeFile countFile) :: IO (Either IOException ())
      pure ()
  | session.terminalType == "" || session.terminalId == "" = pure ()
  | otherwise = do
      countFile <- notifiedCountPath session.sessionId
      cachedResult <- try (readFile countFile) :: IO (Either IOException String)
      let cachedCount = either (const 0) (fromMaybe 0 . readMaybe) cachedResult
      unless (unreadCount <= cachedCount) $
        case newBackend session.terminalType of
          Left _ -> pure ()
          Right backend -> do
            let body = if message == "" then tshow unreadCount <> " unread event(s)" else message
            _ <- backend.notify session.terminalId "handler" body
            _ <- backend.flash session.terminalId
            createDirectoryIfMissing True (takeDirectory countFile)
            writeFile countFile (show unreadCount)

-- --- cmux shortcuts (private; cmd/cmux_config.go's full port is owned by
-- the setup cluster) ---

data CmuxShortcuts = CmuxShortcuts
  { switchToAwaiting :: Text
  , switchToSession  :: Text
  , switchToUnread   :: Text
  , focusBack        :: Text
  , focusForward     :: Text
  }

findCmuxSettings :: IO (Maybe FilePath)
findCmuxSettings = do
  home <- getHomeDirectory
  let candidates =
        [ home </> ".agents/skills/cmux-settings/scripts/cmux-settings"
        , home </> ".codex/skills/cmux-settings/scripts/cmux-settings"
        ]
  firstExecutable candidates
  where
    firstExecutable [] = pure Nothing
    firstExecutable (c : rest) = do
      st <- try (getFileStatus c) :: IO (Either IOException FileStatus)
      case st of
        Right s | isExecutable (fileMode s) -> pure (Just c)
        _ -> firstExecutable rest
    isExecutable :: FileMode -> Bool
    isExecutable m = intersectFileModes m ownerExecuteMode /= 0

cmdOut :: FilePath -> [String] -> IO (Maybe Text)
cmdOut bin args = do
  result <- try (readProcessWithExitCode bin args "")
  pure $ case result :: Either IOException (ExitCode, String, String) of
    Right (ExitSuccess, out, _) | not (null out) -> Just (T.pack out)
    _ -> Nothing

-- | Reads the configured shortcuts from the cmux config; Nothing if the
-- cmux-settings helper is unavailable or actions aren't configured.
getCmuxShortcuts :: IO (Maybe CmuxShortcuts)
getCmuxShortcuts = do
  msettings <- findCmuxSettings
  case msettings of
    Nothing -> pure Nothing
    Just cmuxSettings -> do
      mout <- cmdOut cmuxSettings ["get", "actions"]
      case mout >>= decodeActions of
        Nothing -> pure Nothing
        Just actions -> do
          let shortcutOf aid = fromMaybe "" $ do
                A.Object a <- KM.lookup aid actions
                A.String s <- KM.lookup "shortcut" a
                pure s
          back0 <- readBinding cmuxSettings "shortcuts.bindings.browserBack"
          fwd0 <- readBinding cmuxSettings "shortcuts.bindings.browserForward"
          let sc = CmuxShortcuts
                { switchToAwaiting = shortcutOf "handler-switch-to-awaiting"
                , switchToSession = shortcutOf "handler-switch-to-session"
                , switchToUnread = shortcutOf "handler-switch-to-unread"
                , focusBack = if back0 == "" then "cmd+[" else back0
                , focusForward = if fwd0 == "" then "cmd+]" else fwd0
                }
          pure $ if sc.switchToAwaiting == "" && sc.switchToSession == ""
            then Nothing
            else Just sc
  where
    decodeActions t = case A.decode (BL.fromStrict (TE.encodeUtf8 t)) of
      Just (A.Object o) -> Just o
      _ -> Nothing
    readBinding bin key = do
      mout <- cmdOut bin ["get", key]
      pure $ maybe "" (T.strip . T.dropAround (== '"') . T.strip) mout

-- --- Watcher install/last-run checks (private; the scheduler port owns the
-- canonical versions) ---

watcherIsInstalled :: Text -> IO Bool
watcherIsInstalled name = do
  home <- getHomeDirectory
  let plist = home </> "Library" </> "LaunchAgents"
              </> ("com.agent-handler.watcher-" <> T.unpack name <> ".plist")
  plistExists <- doesFileExist plist
  if plistExists
    then pure True
    else do
      result <- try (readProcessWithExitCode "crontab" ["-l"] "")
      pure $ case result :: Either IOException (ExitCode, String, String) of
        Right (ExitSuccess, out, _) ->
          ("watcher run " <> name) `T.isInfixOf` T.pack out
        _ -> False

watcherLastRunAgo :: Text -> IO (Maybe Text)
watcherLastRunAgo name = do
  home <- handlerHome
  let logPath = home </> "data" </> "logs" </> ("watcher-" <> T.unpack name <> ".log")
  st <- try (getFileStatus logPath) :: IO (Either IOException FileStatus)
  case st of
    Left _ -> pure Nothing
    Right s -> do
      now <- getCurrentTime
      let modTime = posixSecondsToUTCTime (modificationTimeHiRes s)
      pure (Just (formatDurationSecs (realToFrac (diffUTCTime now modTime))))

-- | Seconds since an ISO timestamp, rendered like Go's formatDuration.
agoFromIso :: Text -> IO (Maybe Text)
agoFromIso iso =
  case parseTimeM True defaultTimeLocale "%Y-%m-%dT%H:%M:%S%QZ" (T.unpack iso) :: Maybe UTCTime of
    Nothing -> pure Nothing
    Just t -> do
      now <- getCurrentTime
      pure (Just (formatDurationSecs (realToFrac (diffUTCTime now t))))

-- | Port of cmd/status.go's formatDuration (candidate for Common).
formatDurationSecs :: Double -> Text
formatDurationSecs secs
  | secs < 60 = tshow (floor secs :: Int) <> "s"
  | secs < 3600 = tshow (floor (secs / 60) :: Int) <> "m"
  | secs < 86400 = tshow (floor (secs / 3600) :: Int) <> "h"
  | otherwise = tshow (floor (secs / 86400) :: Int) <> "d"
