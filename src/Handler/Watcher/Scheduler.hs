-- | Port of watcher/scheduler.go: one-shot watcher scheduling via launchd
-- (macOS) or cron (Linux).
module Handler.Watcher.Scheduler
  ( installWatcher
  , uninstallWatcher
  , stopWatcher
  , startWatcher
  , isRunning
  , isInstalled
  , installedInterval
  , lastRunTime
  ) where

import Control.Exception (IOException, try)
import Data.Maybe (fromMaybe)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.IO as TIO
import Data.Time.Clock (UTCTime)
import System.Directory
  ( createDirectoryIfMissing
  , doesFileExist
  , findExecutable
  , getHomeDirectory
  , getModificationTime
  , removeFile
  )
import System.Exit (ExitCode(..))
import System.FilePath (takeDirectory, (</>))
import System.Info (os)
import System.Process (readProcessWithExitCode)
import Text.Read (readMaybe)

import Handler.Db (handlerHome)

launchdPlist :: Text -> Text -> Int -> Text -> Text
launchdPlist name handlerPath intervalSeconds logPath =
  "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n\
  \<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n\
  \<plist version=\"1.0\">\n\
  \<dict>\n\
  \\t<key>Label</key>\n\
  \\t<string>com.agent-handler.watcher-" <> name <> "</string>\n\
  \\t<key>ProgramArguments</key>\n\
  \\t<array>\n\
  \\t\t<string>" <> handlerPath <> "</string>\n\
  \\t\t<string>watcher</string>\n\
  \\t\t<string>run</string>\n\
  \\t\t<string>" <> name <> "</string>\n\
  \\t</array>\n\
  \\t<key>RunAtLoad</key>\n\
  \\t<true/>\n\
  \\t<key>StartInterval</key>\n\
  \\t<integer>" <> T.pack (show intervalSeconds) <> "</integer>\n\
  \\t<key>StandardOutPath</key>\n\
  \\t<string>" <> logPath <> "</string>\n\
  \\t<key>StandardErrorPath</key>\n\
  \\t<string>" <> logPath <> "</string>\n\
  \</dict>\n\
  \</plist>\n"

isDarwin :: Bool
isDarwin = os == "darwin"

-- | Installs a scheduled watcher on the current platform.
-- On macOS, creates a launchd plist. On Linux, adds a cron entry.
installWatcher :: Text -> Int -> IO (Either Text ())
installWatcher name intervalSeconds =
  if isDarwin
    then installLaunchd name intervalSeconds
    else installCron name intervalSeconds

-- | Removes the scheduled watcher from the current platform.
uninstallWatcher :: Text -> IO (Either Text ())
uninstallWatcher name =
  if isDarwin then uninstallLaunchd name else uninstallCron name

-- | Pauses a watcher without removing it; the plist\/cron entry remains
-- but is unloaded.
stopWatcher :: Text -> IO (Either Text ())
stopWatcher name = do
  installed <- isInstalled name
  if not installed
    then pure (Left ("watcher " <> T.pack (show name) <> " is not installed"))
    else if isDarwin
      then do
        plistPath <- launchdPlistPath name
        _ <- readProcessWithExitCode "launchctl" ["unload", plistPath] ""
        pure (Right ())
      else stopCron name

-- | Resumes a stopped watcher.
startWatcher :: Text -> IO (Either Text ())
startWatcher name = do
  installed <- isInstalled name
  if not installed
    then pure (Left ("watcher " <> T.pack (show name) <> " is not installed"))
    else if isDarwin
      then do
        plistPath <- launchdPlistPath name
        (code, _, errOut) <- readProcessWithExitCode "launchctl" ["load", plistPath] ""
        pure $ case code of
          ExitSuccess -> Right ()
          _ -> Left (T.pack errOut)
      else startCron name

-- | Whether the watcher is actively scheduled (installed and not stopped).
isRunning :: Text -> IO Bool
isRunning name = do
  installed <- isInstalled name
  if not installed
    then pure False
    else if isDarwin
      then do
        let label = "com.agent-handler.watcher-" <> T.unpack name
        (code, out, errOut) <- readProcessWithExitCode "launchctl" ["list", label] ""
        pure (code == ExitSuccess && not (null (out <> errOut)))
      else isRunningCron name

-- | Whether the watcher is installed on the current platform.
isInstalled :: Text -> IO Bool
isInstalled name =
  if isDarwin
    then launchdPlistPath name >>= doesFileExist
    else isInstalledCron name

-- | The configured polling interval in seconds for an installed watcher;
-- 0 if not installed or undeterminable (always 0 for cron).
installedInterval :: Text -> IO Int
installedInterval name =
  if not isDarwin
    then pure 0
    else do
      plistPath <- launchdPlistPath name
      result <- try (TIO.readFile plistPath) :: IO (Either IOException Text)
      pure $ case result of
        Left _ -> 0
        Right content ->
          let (_, rest) = T.breakOn "<key>StartInterval</key>" content
          in if T.null rest
               then 0
               else
                 let afterStart = snd (T.breakOn "<integer>" rest)
                     inner = T.takeWhile (/= '<') (T.drop (T.length "<integer>") afterStart)
                 in fromMaybe 0 (readMaybe (T.unpack inner))

-- | The last run time of the watcher, from the log file's modification
-- time; Nothing if the log file does not exist.
lastRunTime :: Text -> IO (Maybe UTCTime)
lastRunTime name = do
  home <- handlerHome
  let logPath = home </> "data" </> "logs" </> ("watcher-" <> T.unpack name <> ".log")
  result <- try (getModificationTime logPath) :: IO (Either IOException UTCTime)
  pure $ either (const Nothing) Just result

watcherLogPath :: Text -> IO FilePath
watcherLogPath name = do
  home <- handlerHome
  let logDir = home </> "data" </> "logs"
  createDirectoryIfMissing True logDir
  pure (logDir </> ("watcher-" <> T.unpack name <> ".log"))

-- | Creates a launchd plist for the watcher and loads it.
installLaunchd :: Text -> Int -> IO (Either Text ())
installLaunchd name intervalSeconds = do
  mHandler <- findExecutable "handler"
  case mHandler of
    Nothing -> pure (Left "handler binary not found in PATH")
    Just handlerPath -> do
      logPath <- watcherLogPath name
      plistPath <- launchdPlistPath name
      createDirectoryIfMissing True (takeDirectory plistPath)
      TIO.writeFile plistPath
        (launchdPlist name (T.pack handlerPath) intervalSeconds (T.pack logPath))
      (code, out, errOut) <- readProcessWithExitCode "launchctl" ["load", plistPath] ""
      pure $ case code of
        ExitSuccess -> Right ()
        _ -> Left ("failed to load plist (output: " <> T.pack (out <> errOut) <> ")")

-- | Removes the launchd plist for the watcher.
uninstallLaunchd :: Text -> IO (Either Text ())
uninstallLaunchd name = do
  plistPath <- launchdPlistPath name
  exists <- doesFileExist plistPath
  if not exists
    then pure (Left ("watcher " <> T.pack (show name) <> " is not installed"))
    else do
      -- Don't fail if unload errors — the job might not be loaded
      _ <- readProcessWithExitCode "launchctl" ["unload", plistPath] ""
      result <- try (removeFile plistPath) :: IO (Either IOException ())
      pure $ case result of
        Left e -> Left ("failed to remove plist: " <> T.pack (show e))
        Right () -> Right ()

-- | The path to the launchd plist for the given watcher name.
launchdPlistPath :: Text -> IO FilePath
launchdPlistPath name = do
  home <- getHomeDirectory
  pure (home </> "Library" </> "LaunchAgents"
        </> ("com.agent-handler.watcher-" <> T.unpack name <> ".plist"))

commentMarkerFor :: Text -> Text
commentMarkerFor name = "# agent-handler-" <> name

stoppedMarkerFor :: Text -> Text
stoppedMarkerFor name = "# agent-handler-" <> name <> "-stopped"

readCrontab :: IO (Either Text Text)
readCrontab = do
  (code, out, errOut) <- readProcessWithExitCode "crontab" ["-l"] ""
  pure $ case code of
    ExitSuccess -> Right (T.pack out)
    _ -> Left (T.pack (out <> errOut))

writeCrontab :: Text -> IO (Either Text ())
writeCrontab content = do
  (code, out, errOut) <- readProcessWithExitCode "crontab" ["-"] (T.unpack content)
  pure $ case code of
    ExitSuccess -> Right ()
    _ -> Left ("failed to write crontab (output: " <> T.pack (out <> errOut) <> ")")

-- | Filters out the marker line for a watcher and the line after it.
-- Also drops blank lines, matching the Go filtering.
filterWatcherEntry :: Text -> [Text] -> ([Text], Bool)
filterWatcherEntry marker = go False False []
  where
    go found _ acc [] = (reverse acc, found)
    go found skipNext acc (line : rest)
      | T.strip line == marker = go True True acc rest
      | skipNext = go found False acc rest
      | T.strip line == "" = go found skipNext acc rest
      | otherwise = go found False (line : acc) rest

-- | Adds a cron entry for the watcher.
installCron :: Text -> Int -> IO (Either Text ())
installCron name intervalSeconds = do
  mHandler <- findExecutable "handler"
  case mHandler of
    Nothing -> pure (Left "handler binary not found in PATH")
    Just handlerPath -> do
      logPath <- watcherLogPath name
      let intervalMinutes = max 1 (intervalSeconds `div` 60)
          cronSchedule = "*/" <> T.pack (show intervalMinutes) <> " * * * *"
      existing <- either (const "") id <$> readCrontab
      let (filtered, _) = filterWatcherEntry (commentMarkerFor name) (T.lines existing)
          newEntry = commentMarkerFor name <> "\n"
                     <> cronSchedule <> " " <> T.pack handlerPath
                     <> " watcher run " <> name <> " >> " <> T.pack logPath <> " 2>&1"
      writeCrontab (T.intercalate "\n" (filtered ++ [newEntry]) <> "\n")

-- | Removes the cron entry for the watcher.
uninstallCron :: Text -> IO (Either Text ())
uninstallCron name = do
  existing <- readCrontab
  case existing of
    Left err -> pure (Left ("failed to read crontab: " <> err))
    Right content -> do
      let (filtered, found) = filterWatcherEntry (commentMarkerFor name) (T.lines content)
      if not found
        then pure (Left ("watcher " <> T.pack (show name) <> " is not installed"))
        else do
          let joined = T.intercalate "\n" filtered
              newCrontab = if T.strip joined == "" then joined else joined <> "\n"
          writeCrontab newCrontab

-- | Whether a cron entry exists for the watcher.
isInstalledCron :: Text -> IO Bool
isInstalledCron name = do
  result <- readCrontab
  pure $ case result of
    Left _ -> False
    Right content -> commentMarkerFor name `T.isInfixOf` content

stopCron :: Text -> IO (Either Text ())
stopCron name = do
  existing <- readCrontab
  case existing of
    Left err -> pure (Left ("failed to read crontab: " <> err))
    Right content -> do
      let swapped =
            [ if T.strip line == commentMarkerFor name then stoppedMarkerFor name else line
            | line <- T.splitOn "\n" content
            ]
      writeCrontab (T.intercalate "\n" swapped)

startCron :: Text -> IO (Either Text ())
startCron name = do
  existing <- readCrontab
  case existing of
    Left err -> pure (Left ("failed to read crontab: " <> err))
    Right content -> do
      let swapped =
            [ if T.strip line == stoppedMarkerFor name then commentMarkerFor name else line
            | line <- T.splitOn "\n" content
            ]
      writeCrontab (T.intercalate "\n" swapped)

isRunningCron :: Text -> IO Bool
isRunningCron name = do
  result <- readCrontab
  pure $ case result of
    Left _ -> False
    Right content ->
      commentMarkerFor name `T.isInfixOf` content
      && not (stoppedMarkerFor name `T.isInfixOf` content)
