-- | Port of cmd/watcher/: the `handler watcher` subcommand tree
-- (auth, auth status, list, logs, run, install, start, stop, uninstall).
module Handler.Cmd.WatcherCli (watcherCommand) where

import Control.Concurrent (threadDelay)
import Control.Exception (IOException, try)
import Control.Monad (foldM, forM, forM_, unless, when)
import Data.Aeson (object, toJSON, (.=))
import qualified Data.ByteString as BS
import Data.Char (isDigit)
import Data.IORef (newIORef, readIORef, writeIORef)
import Data.List (sortOn)
import Data.Maybe (catMaybes, fromMaybe)
import Data.Text (Text)
import Data.Time.Format (defaultTimeLocale, formatTime)
import qualified Data.Text as T
import qualified Data.Text.Encoding as TE
import qualified Data.Text.IO as TIO
import qualified Data.Map.Strict as Map
import Options.Applicative
import System.FilePath ((</>))
import System.IO (hFlush, stdout)

import Handler.Cli.Common
import Handler.Config
import Handler.Db (close, handlerHome)
import qualified Handler.Db as Db
import Handler.Watcher.Framework (WatchedResource(..), activeResources)
import qualified Handler.Watcher.GitHub as GitHub
import qualified Handler.Watcher.Jira as Jira
import qualified Handler.Watcher.Scheduler as Scheduler

knownWatchers :: [Text]
knownWatchers = ["github", "jira"]

-- | Default polling intervals in seconds (github 3m, jira 5m).
defaultIntervals :: Text -> Int
defaultIntervals = \case
  "github" -> 180
  "jira"   -> 300
  _        -> 0

watcherCommand :: Mod CommandFields NamedCommand
watcherCommand = mkCommand "watcher" "Manage external event watchers" $ hsubparser $ mconcat
  [ command "auth" (info (authParser <**> helper)
      (progDesc "Configure authentication for external services"))
  , command "list" (info (pure runList <**> helper)
      (progDesc "List installed watchers"))
  , command "logs" (info (logsParser <**> helper)
      (progDesc "View watcher logs"))
  , command "run" (info (runParser <**> helper)
      (progDesc "Run a watcher once (one-shot poll)"))
  , command "install" (info (installParser <**> helper)
      (progDesc "Set up and install watchers"))
  , command "start" (info (startParser <**> helper)
      (progDesc "Resume paused watchers"))
  , command "stop" (info (stopParser <**> helper)
      (progDesc "Pause installed watchers"))
  , command "uninstall" (info (uninstallParser <**> helper)
      (progDesc "Remove scheduled watchers"))
  ]

-- Scheduler call sites are isolated here so signature reconciliation with
-- Handler.Watcher.Scheduler is a one-liner each.
schedIsInstalled :: Text -> IO Bool
schedIsInstalled = Scheduler.isInstalled

schedInstall :: Text -> Int -> IO (Either Text ())
schedInstall = Scheduler.installWatcher

schedUninstall :: Text -> IO (Either Text ())
schedUninstall = Scheduler.uninstallWatcher

schedStart :: Text -> IO (Either Text ())
schedStart = Scheduler.startWatcher

schedStop :: Text -> IO (Either Text ())
schedStop = Scheduler.stopWatcher

schedIsRunning :: Text -> IO Bool
schedIsRunning = Scheduler.isRunning

schedLastRun :: Text -> IO (Maybe Text)
schedLastRun name = fmap fmt <$> Scheduler.lastRunTime name
  where fmt = T.pack . formatTime defaultTimeLocale "%Y-%m-%dT%H:%M:%SZ"

-- auth ------------------------------------------------------------------

authParser :: Parser CommandAction
authParser =
  hsubparser
    (command "status" (info (pure runAuthStatus <**> helper)
      (progDesc "Show authentication status for external services")))
  <|> (runAuth <$> optional (strArgument (metavar "SERVICE" <> help "github or jira")))

runAuth :: Maybe Text -> Ctx -> IO ()
runAuth mservice _ = do
  configPath <- configDefaultPath
  cfg <- readConfig configPath
  services <- case T.toLower <$> mservice of
    Nothing -> pure ["github", "jira"]
    Just s
      | s `elem` knownWatchers -> pure [s]
      | otherwise -> dieText ("unknown service: " <> s <> " (must be 'github' or 'jira')")
  (cfg', modified) <- foldM step (cfg, False) services
  when modified $ do
    writeConfig configPath cfg'
    putTextLn "\nConfiguration saved."
  where
    step (c, changed) "github" = do
      (c', ch) <- configureGitHubAuth c
      pure (c', changed || ch)
    step (c, changed) "jira" = do
      (c', ch) <- configureJiraAuth c
      pure (c', changed || ch)
    step acc _ = pure acc

prompt :: Text -> IO Text
prompt msg = do
  TIO.putStr msg
  hFlush stdout
  T.strip <$> TIO.getLine

-- | Interactive GitHub token setup; returns (config, changed).
configureGitHubAuth :: Config -> IO (Config, Bool)
configureGitHubAuth cfg = do
  putTextLn "\n=== GitHub Configuration ==="
  alreadyOk <- case cfg.services.github of
    Just g | g.token /= "" -> do
      putTextLn "GitHub is already configured."
      validateGitHubToken g.token Nothing >>= \case
        Right username -> do
          putTextLn ("✓ Valid token for user: " <> username)
          pure (Just True)
        Left err -> do
          putTextLn ("⚠ Token validation failed: " <> err)
          response <- prompt "Would you like to reconfigure? (y/N): "
          pure (if T.toLower response == "y" then Nothing else Just False)
    _ -> pure Nothing
  case alreadyOk of
    Just _ -> pure (cfg, False)
    Nothing -> do
      putTextLn "Create a personal access token at: https://github.com/settings/tokens"
      putTextLn "Required scopes: repo"
      token <- prompt "\nEnter GitHub token (or press Enter to skip): "
      if token == ""
        then do
          putTextLn "Skipped GitHub configuration."
          pure (cfg, False)
        else validateGitHubToken token Nothing >>= \case
          Left err -> dieText ("token validation failed: " <> err)
          Right username -> do
            putTextLn ("✓ Valid token for user: " <> username)
            let services' = cfg.services { github = Just (GitHubConfig token) }
            pure (cfg { services = services' }, True)

-- | Interactive Jira credential setup; returns (config, changed).
configureJiraAuth :: Config -> IO (Config, Bool)
configureJiraAuth cfg = do
  putTextLn "\n=== Jira Configuration ==="
  alreadyOk <- case cfg.services.jira of
    Just j | j.token /= "" -> do
      putTextLn "Jira is already configured."
      validateJiraToken j.url j.email j.token >>= \case
        Right displayName -> do
          putTextLn ("✓ Valid credentials for: " <> displayName)
          putTextLn ("  URL: " <> j.url)
          putTextLn ("  Email: " <> j.email)
          pure (Just True)
        Left err -> do
          putTextLn ("⚠ Token validation failed: " <> err)
          response <- prompt "Would you like to reconfigure? (y/N): "
          pure (if T.toLower response == "y" then Nothing else Just False)
    _ -> pure Nothing
  case alreadyOk of
    Just _ -> pure (cfg, False)
    Nothing -> do
      putTextLn "Create an API token at: https://id.atlassian.com/manage-profile/security/api-tokens"
      url0 <- prompt "\nEnter Jira base URL (e.g., https://your-domain.atlassian.net) or press Enter to skip: "
      if url0 == ""
        then do
          putTextLn "Skipped Jira configuration."
          pure (cfg, False)
        else do
          let url = fromMaybe url0 (T.stripSuffix "/" url0)
          email <- prompt "Enter Jira email: "
          token <- prompt "Enter Jira API token: "
          validateJiraToken url email token >>= \case
            Left err -> dieText ("credential validation failed: " <> err)
            Right displayName -> do
              putTextLn ("✓ Valid credentials for: " <> displayName)
              putTextLn ""
              putTextLn "  Custom Jira fields can be configured in config.yaml under services.jira.custom_fields."
              putTextLn "  Adding custom fields (e.g. priority, blocked status, epic links) provides additional"
              putTextLn "  context when the handler session triages work across sessions."
              putTextLn "  See the commented examples in config.yaml for common fields."
              let existing = maybe Map.empty (.customFields) cfg.services.jira
                  customFields =
                    if Map.null existing
                      then Map.fromList
                        [ ("blocked", "customfield_10517")
                        , ("blocked_reason", "customfield_10483")
                        , ("epic_key", "customfield_10014")
                        , ("flagged", "customfield_10021")
                        , ("story_points", "customfield_10028")
                        , ("git_pull_request", "customfield_10875")
                        ]
                      else existing
                  botUsernames = maybe [] (.botUsernames) cfg.services.jira
              when (Map.null existing) $ do
                putTextLn ""
                putTextLn "  Default custom fields have been added to your config. These are common"
                putTextLn "  field IDs for Jira Cloud — adjust them in ~/.agent-handler/config.yaml"
                putTextLn "  if your instance uses different field IDs."
              let services' = cfg.services
                    { jira = Just JiraConfig
                        { url = url, email = email, token = token
                        , botUsernames = botUsernames, customFields = customFields
                        }
                    }
              pure (cfg { services = services' }, True)

runAuthStatus :: Ctx -> IO ()
runAuthStatus ctx = do
  configPath <- configDefaultPath
  cfg <- readConfig configPath
  let gh = isServiceConfigured cfg "github"
      jr = isServiceConfigured cfg "jira"
  if ctx.jsonOutput
    then printJson (object ["github" .= gh, "jira" .= jr])
    else do
      putTextLn (if gh then "✓ GitHub: configured" else "✗ GitHub: not configured")
      putTextLn (if jr then "✓ Jira: configured" else "✗ Jira: not configured")

-- list ------------------------------------------------------------------

runList :: Ctx -> IO ()
runList ctx = do
  infos <- forM knownWatchers $ \name -> do
    installed <- schedIsInstalled name
    lastRun <- schedLastRun name
    pure (name, installed, lastRun)
  if ctx.jsonOutput
    then printJson $ toJSON
      [ object $ ["name" .= name, "installed" .= installed]
                 ++ catMaybes [("last_run" .=) <$> lastRun]
      | (name, installed, lastRun) <- infos
      ]
    else do
      putTextLn "Watchers:"
      forM_ infos $ \(name, installed, lastRun) ->
        printfT [ "  ", name, ": "
                , if installed then "installed" else "not installed"
                , " (last run: ", fromMaybe "never" lastRun, ")"
                ]

-- logs ------------------------------------------------------------------

data LogsOpts = LogsOpts
  { loName  :: Maybe Text
  , loLines :: Int
  , loTail  :: Bool
  }

logsParser :: Parser CommandAction
logsParser = fmap runLogs $ LogsOpts
  <$> optional (strArgument (metavar "NAME" <> help "watcher name"))
  <*> option auto (long "lines" <> value 50 <> help "number of lines to show")
  <*> switch (long "tail" <> help "follow logs in real-time")

logPath :: Text -> IO FilePath
logPath name = do
  home <- handlerHome
  pure (home </> "data" </> "logs" </> ("watcher-" <> T.unpack name <> ".log"))

readLogLines :: Text -> IO (Maybe [Text])
readLogLines name = do
  path <- logPath name
  result <- try (BS.readFile path) :: IO (Either IOException BS.ByteString)
  pure $ case result of
    Left _ -> Nothing
    Right bytes ->
      Just (T.lines (T.dropWhileEnd (== '\n') (TE.decodeUtf8Lenient bytes)))

runLogs :: LogsOpts -> Ctx -> IO ()
runLogs o _
  | o.loTail = tailLogs (maybe knownWatchers pure o.loName)
  | Just name <- o.loName = showSingleLog name o.loLines
  | otherwise = showCombinedLogs o.loLines

showSingleLog :: Text -> Int -> IO ()
showSingleLog name numLines =
  readLogLines name >>= \case
    Nothing -> printfT ["No logs found for ", name, " watcher"]
    Just fileLines ->
      mapM_ putTextLn (takeEnd numLines fileLines)

showCombinedLogs :: Int -> IO ()
showCombinedLogs numLines = do
  all' <- fmap concat $ forM knownWatchers $ \name ->
    readLogLines name >>= \case
      Nothing -> pure []
      Just ls -> pure [(name, l) | l <- ls, l /= ""]
  if null all'
    then putTextLn "No watcher logs found"
    else
      -- Sort by timestamp (log lines start with the date prefix)
      forM_ (takeEnd numLines (sortOn snd all')) $ \(name, l) ->
        printfT ["[", name, "] ", l]

takeEnd :: Int -> [a] -> [a]
takeEnd n xs = drop (max 0 (length xs - n)) xs

-- | Follows watcher log files, polling once per second. Interrupt with
-- Ctrl+C (the Go version catches SIGINT; here the default handler exits).
tailLogs :: [Text] -> IO ()
tailLogs targets = do
  let prefix = length targets > 1
  offsets <- forM targets $ \name -> do
    path <- logPath name
    size <- fileSize path
    ref <- newIORef size
    pure (name, path, ref)
  putTextLn "Tailing watcher logs... (Ctrl+C to stop)"
  let loop = do
        forM_ offsets $ \(name, path, ref) -> do
          offset <- readIORef ref
          size <- fileSize path
          when (size > offset) $ do
            result <- try (BS.readFile path) :: IO (Either IOException BS.ByteString)
            case result of
              Left _ -> pure ()
              Right bytes -> do
                let fresh = TE.decodeUtf8Lenient (BS.drop (fromIntegral offset) bytes)
                forM_ (filter (/= "") (T.lines fresh)) $ \l ->
                  if prefix then printfT ["[", name, "] ", l] else putTextLn l
                writeIORef ref (fromIntegral (BS.length bytes))
        threadDelay 1000000
        loop
  loop
  where
    fileSize path = do
      r <- try (BS.readFile path) :: IO (Either IOException BS.ByteString)
      pure $ case r of
        Left _ -> 0 :: Integer
        Right b -> fromIntegral (BS.length b)

-- run -------------------------------------------------------------------

data RunOpts = RunOpts
  { roName      :: Text
  , roResources :: Text
  }

runParser :: Parser CommandAction
runParser = fmap runRun $ RunOpts
  <$> strArgument (metavar "NAME" <> help "watcher to run (github or jira)")
  <*> strOption (long "resources" <> value ""
        <> help "Comma-separated resource IDs for catch-up mode")

-- | Maps service names to resource types.
serviceToResourceType :: Text -> Text
serviceToResourceType = \case
  "github" -> "pr"
  "jira"   -> "jira"
  _        -> ""

runRun :: RunOpts -> Ctx -> IO ()
runRun o _ = do
  let name = T.toLower o.roName
  unless (name `elem` knownWatchers) $
    dieText ("unknown watcher: " <> name <> " (must be 'github' or 'jira')")
  configPath <- configDefaultPath
  cfg <- readConfig configPath
  unless (isServiceConfigured cfg name) $
    dieText ("service " <> T.pack (show name) <> " is not configured. Run 'handler watcher auth " <> name <> "' first")
  db <- Db.defaultPath >>= Db.open
  let resourceType = serviceToResourceType name
  resources <-
    if o.roResources /= ""
      then pure
        [ WatchedResource { resourceType = resourceType, resourceId = rid, resourceUrl = "" }
        | rid <- map T.strip (T.splitOn "," o.roResources), rid /= ""
        ]
      else activeResources db resourceType
  if null resources
    then printfT ["No resources to poll for ", name, " watcher."]
    else do
      printfT ["Polling ", T.pack (show (length resources)), " resources for ", name, " watcher..."]
      result <- case name of
        "github" -> GitHub.poll db cfg resources
        "jira"   -> Jira.poll db cfg resources
        _        -> pure (Left ("unknown watcher: " <> name))
      close db
      case result of
        Right () -> pure ()
        Left err -> dieText err

-- install ---------------------------------------------------------------

data InstallOpts = InstallOpts
  { ioName     :: Maybe Text
  , ioInterval :: Maybe Text
  }

installParser :: Parser CommandAction
installParser = fmap runInstall $ InstallOpts
  <$> optional (strArgument (metavar "NAME" <> help "watcher to install"))
  <*> optional (strOption (long "interval" <> metavar "DURATION"
        <> help "polling interval (e.g. 3m, 5m)"))

-- | Parses Go-style durations (30s, 3m, 1h, 1h30m) to seconds.
parseDuration :: Text -> Maybe Int
parseDuration = go 0
  where
    go acc t
      | T.null t = if acc > 0 then Just acc else Nothing
      | otherwise = do
          let (numT, rest) = T.span isDigit t
          n <- if T.null numT then Nothing else Just (read (T.unpack numT))
          (unit, rest') <- T.uncons rest
          mult <- case unit of
            'h' -> Just 3600
            'm' -> Just 60
            's' -> Just 1
            _   -> Nothing
          go (acc + n * mult) rest'

-- | Renders seconds the way Go prints a Duration (e.g. "3m0s").
formatInterval :: Int -> Text
formatInterval secs
  | secs >= 3600 = T.pack (show (secs `div` 3600)) <> "h"
      <> formatInterval (secs `mod` 3600)
  | secs >= 60 = T.pack (show (secs `div` 60)) <> "m"
      <> T.pack (show (secs `mod` 60)) <> "s"
  | otherwise = T.pack (show secs) <> "s"

runInstall :: InstallOpts -> Ctx -> IO ()
runInstall o _ = case o.ioName of
  Just name -> installSingle (T.toLower name) o.ioInterval
  Nothing   -> installAll

installAll :: IO ()
installAll = do
  configPath <- configDefaultPath
  cfg0 <- readConfig configPath
  (cfg1, ch1) <- configureGitHubAuth cfg0
  (cfg2, ch2) <- configureJiraAuth cfg1
  when (ch1 || ch2) $ writeConfig configPath cfg2
  cfg <- readConfig configPath
  installed <- fmap sum $ forM knownWatchers $ \name ->
    if not (isServiceConfigured cfg name)
      then pure (0 :: Int)
      else do
        already <- schedIsInstalled name
        if already
          then do
            printfT ["  ✓ ", name, " watcher already installed"]
            pure 1
          else do
            let interval = defaultIntervals name
            schedInstall name interval >>= \case
              Left err -> do
                printfT ["  ⚠ Failed to install ", name, " watcher: ", err]
                pure 0
              Right () -> do
                printfT ["  ✓ Installed ", name, " watcher (polling every ", formatInterval interval, ")"]
                pure 1
  if installed == 0
    then putTextLn "\nNo services configured. Watchers not installed."
    else do
      putTextLn "\nTo check status: handler watcher list"
      putTextLn "To stop:         handler watcher stop"

installSingle :: Text -> Maybe Text -> IO ()
installSingle name mInterval = do
  unless (name `elem` knownWatchers) $
    dieText ("unknown watcher: " <> name <> " (valid: " <> T.intercalate ", " knownWatchers <> ")")
  configPath <- configDefaultPath
  cfg <- readConfig configPath
  unless (isServiceConfigured cfg name) $
    dieText (name <> " is not configured. Run 'handler watcher auth " <> name <> "' first")
  interval <- case mInterval of
    Nothing -> pure (defaultIntervals name)
    Just t -> case parseDuration t of
      Just secs -> pure secs
      Nothing -> dieText ("invalid interval: " <> t)
  schedInstall name interval >>= \case
    Left err -> dieText ("installing watcher: " <> err)
    Right () -> do
      printfT ["✓ Installed ", name, " watcher (polling every ", formatInterval interval, ")"]
      putTextLn "\nTo check status: handler watcher list"
      printfT ["To run now:      handler watcher run ", name]
      putTextLn "To stop:         handler watcher stop"

-- start / stop / uninstall ----------------------------------------------

targetsParser :: Parser (Maybe Text)
targetsParser = optional (strArgument (metavar "NAME" <> help "watcher name"))

startParser :: Parser CommandAction
startParser = runStartStop "start" <$> targetsParser

stopParser :: Parser CommandAction
stopParser = runStartStop "stop" <$> targetsParser

runStartStop :: Text -> Maybe Text -> Ctx -> IO ()
runStartStop verb mname _ = do
  let targets = maybe knownWatchers pure mname
  n <- fmap sum $ forM targets $ \name -> do
    installed <- schedIsInstalled name
    running <- schedIsRunning name
    let eligible = installed && (if verb == "start" then not running else running)
    if not eligible
      then pure (0 :: Int)
      else do
        result <- if verb == "start" then schedStart name else schedStop name
        case result of
          Left err -> do
            printfT ["  ⚠ Failed to ", verb, " ", name, ": ", err]
            pure 0
          Right () -> do
            printfT ["  ✓ ", if verb == "start" then "Started " else "Stopped ", name, " watcher"]
            pure 1
  when (n == 0) $
    if verb == "start"
      then do
        putTextLn "No paused watchers to start."
        anyInstalled <- or <$> mapM schedIsInstalled targets
        unless anyInstalled $
          putTextLn "Install watchers with: handler watcher install"
      else do
        putTextLn "No running watchers to stop."
        putTextLn "Install watchers with: handler watcher install"

uninstallParser :: Parser CommandAction
uninstallParser = runUninstall <$> targetsParser

runUninstall :: Maybe Text -> Ctx -> IO ()
runUninstall mname _ = do
  let targets = maybe knownWatchers pure mname
  n <- fmap sum $ forM targets $ \name -> do
    installed <- schedIsInstalled name
    if not installed
      then pure (0 :: Int)
      else schedUninstall name >>= \case
        Left err -> do
          printfT ["  ⚠ Failed to uninstall ", name, ": ", err]
          pure 0
        Right () -> do
          printfT ["  ✓ Uninstalled ", name, " watcher"]
          pure 1
  when (n == 0) $ putTextLn "No installed watchers to uninstall."
