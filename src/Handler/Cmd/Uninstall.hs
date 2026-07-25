-- | Port of cmd/uninstall.go: remove agent-handler configuration.
module Handler.Cmd.Uninstall
  ( uninstallCommand
  , skillNames
  , confirm
  , isAgentHandlerHook
  ) where

import Control.Exception (IOException, try)
import Control.Monad (filterM, forM_, unless, when)
import qualified Data.Aeson as A
import qualified Data.Aeson.Key as Key
import qualified Data.Aeson.KeyMap as KM
import Data.Aeson.Encode.Pretty (Config(..), Indent(..), defConfig, encodePretty')
import qualified Data.ByteString.Lazy as BL
import qualified Data.ByteString.Lazy.Char8 as BLC
import Data.Maybe (fromMaybe, mapMaybe)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.Encoding as TE
import qualified Data.Vector as V
import Options.Applicative
import System.Directory
  ( canonicalizePath, doesDirectoryExist, doesFileExist, getHomeDirectory
  , listDirectory, pathIsSymbolicLink, removeDirectoryRecursive, removeFile
  , getSymbolicLinkTarget )
import System.Environment (getExecutablePath, lookupEnv)
import System.FilePath (takeFileName, (</>))
import System.IO (hFlush, stdout)

import Handler.Cli.Common
import Handler.CmuxConfig (cmuxConfigFilePath, handlerCmuxActionIDs, hasCmuxActions, removeCmuxActions)
import Handler.Db (handlerHome)
import Handler.Watcher.Scheduler (isInstalled, uninstallWatcher)

-- | Skills installed by handler setup. Update when adding/removing skills.
skillNames :: [Text]
skillNames =
  [ "inbox"
  , "inbox-clear"
  , "inbox-mode"
  , "message"
  , "watch"
  , "unwatch"
  , "watching"
  , "handler"
  , "catchup"
  , "done"
  , "handler-debug"
  ]

-- | y/N confirmation prompt on stdin.
confirm :: Text -> IO Bool
confirm prompt = do
  printfT' [prompt, " [y/N] "]
  hFlush stdout
  result <- try getLine :: IO (Either IOException String)
  pure $ case result of
    Left _ -> False
    Right response ->
      let r = T.toLower (T.strip (T.pack response))
      in r == "y" || r == "yes"
  where printfT' = putStr . T.unpack . T.concat

-- | Whether a settings.json hook (or statusLine) entry belongs to
-- agent-handler, judged by its JSON serialization mentioning agent-handler.
isAgentHandlerHook :: A.Value -> Bool
isAgentHandlerHook v = "agent-handler" `T.isInfixOf` TE.decodeUtf8Lenient (BLC.toStrict (A.encode v))

uninstallCommand :: Mod CommandFields NamedCommand
uninstallCommand = mkCommand "uninstall" "Remove agent-handler configuration" (pure runUninstall)

runUninstall :: Ctx -> IO ()
runUninstall _ = do
  home <- getHomeDirectory
  agentHandlerDir <- handlerHome
  let claudeSkillsDir = home </> ".claude" </> "skills"
      claudeRulesDir = home </> ".claude" </> "rules"
      settingsPath = home </> ".claude" </> "settings.json"
      hooksPath = agentHandlerDir </> "hooks"
      skillsPath = agentHandlerDir </> "skills"
      rulesPath = agentHandlerDir </> "rules"

  putTextLn "agent-handler uninstall will:"
  putTextLn ""

  symlinkTargets <- findAgentHandlerSkills claudeSkillsDir
  unless (null symlinkTargets) $ do
    printfT ["  Remove ", T.pack (show (length symlinkTargets)), " skill symlinks from ", T.pack claudeSkillsDir, ":"]
    forM_ symlinkTargets $ \name -> printfT ["    - ", name]

  hookNames <- findAgentHandlerHooks settingsPath
  unless (null hookNames) $ do
    printfT ["  Remove ", T.pack (show (length hookNames)), " hooks from ", T.pack settingsPath, ":"]
    forM_ hookNames $ \name -> printfT ["    - ", name]

  cmuxActionsPresent <- hasCmuxActions
  insideCmux <- (/= Nothing) <$> lookupEnv "CMUX_SURFACE_ID"
  when cmuxActionsPresent $
    if not insideCmux
      then putTextLn "  \ESC[33m⚠ cmux actions found but not running inside cmux — cannot remove them\ESC[0m"
      else do
        cmuxPath <- cmuxConfigFilePath
        printfT ["  Remove cmux actions from ", T.pack cmuxPath, ":"]
        forM_ handlerCmuxActionIDs $ \actionId -> printfT ["    - ", actionId]

  installedWatchers <- filterM isInstalled ["github", "jira"]
  forM_ installedWatchers $ \name ->
    printfT ["  Uninstall ", name, " watcher schedule"]

  binaryPath <- getExecutablePath
  realBinaryPath <- fromMaybe binaryPath . either (const Nothing) Just
    <$> (try (canonicalizePath binaryPath) :: IO (Either IOException FilePath))
  if binaryPath /= realBinaryPath
    then printfT ["  Remove ", T.pack binaryPath, " -> ", T.pack realBinaryPath]
    else printfT ["  Remove ", T.pack realBinaryPath]

  forM_ [(hooksPath, "hooks"), (skillsPath, "skills"), (rulesPath, "rules")] $ \(p, what) -> do
    exists <- doesDirectoryExist p
    when exists $ printfT ["  Remove extracted ", what, " from ", T.pack p]

  installedRules <- do
    entries <- either (const []) id
      <$> (try (listDirectory rulesPath) :: IO (Either IOException [FilePath]))
    filterM (\name -> doesFileExist (claudeRulesDir </> name)) entries
  unless (null installedRules) $ do
    printfT ["  Remove ", T.pack (show (length installedRules)), " global rule(s) from ", T.pack claudeRulesDir, ":"]
    forM_ installedRules $ \name -> printfT ["    - ", T.pack name]

  hasPerm <- hasHandlerPermission settingsPath
  when hasPerm $
    printfT ["  Remove Bash(handler *) permission from ", T.pack settingsPath]

  putTextLn ""
  proceed <- confirm "Proceed?"
  if not proceed
    then putTextLn "Aborted."
    else do
      putTextLn ""

      forM_ symlinkTargets $ \name -> do
        _ <- try (removeFile (claudeSkillsDir </> T.unpack name)) :: IO (Either IOException ())
        printfT ["  ✓ Removed skill symlink ", name]

      when (cmuxActionsPresent && insideCmux) removeCmuxActions

      forM_ installedWatchers $ \name -> do
        uninstallWatcher name
        printfT ["  ✓ Uninstalled ", name, " watcher"]

      forM_ installedRules $ \name -> do
        _ <- try (removeFile (claudeRulesDir </> name)) :: IO (Either IOException ())
        printfT ["  ✓ Removed global rule ", T.pack name]

      unless (null hookNames) $ removeHooks home

      -- Remove binary last (since we're running from it)
      when (binaryPath /= realBinaryPath) $ do
        _ <- try (removeFile binaryPath) :: IO (Either IOException ())
        printfT ["  ✓ Removed ", T.pack binaryPath]
      _ <- try (removeFile realBinaryPath) :: IO (Either IOException ())
      printfT ["  ✓ Removed ", T.pack realBinaryPath]

      forM_ ["hooks", "skills", "rules"] $ \dir -> do
        let dirPath = agentHandlerDir </> dir
        exists <- doesDirectoryExist dirPath
        when exists $ do
          removeDirectoryRecursive dirPath
          printfT ["  ✓ Removed ", T.pack dirPath]

      putTextLn "\n✓ Uninstallation complete!"
      let dataDir = agentHandlerDir </> "data"
      dataExists <- doesDirectoryExist dataDir
      when dataExists $ do
        printfT ["\n  Your event history, session data, and database are still at ", T.pack dataDir]
        putTextLn "  To fully remove all data: rm -rf ~/.agent-handler"

-- | Skill symlinks in ~\/.claude\/skills that point into agent-handler.
findAgentHandlerSkills :: FilePath -> IO [Text]
findAgentHandlerSkills claudeSkillsDir =
  flip filterM skillNames $ \name -> do
    let dst = claudeSkillsDir </> T.unpack name
    isLink <- either (const False) id
      <$> (try (pathIsSymbolicLink dst) :: IO (Either IOException Bool))
    if not isLink
      then pure False
      else do
        target <- try (getSymbolicLinkTarget dst) :: IO (Either IOException FilePath)
        pure $ case target of
          Right t -> "agent-handler" `T.isInfixOf` T.pack t
          Left _  -> False

readSettings :: FilePath -> IO (Maybe (KM.KeyMap A.Value))
readSettings settingsPath = do
  result <- try (BL.readFile settingsPath) :: IO (Either IOException BL.ByteString)
  pure $ case result of
    Left _ -> Nothing
    Right raw -> case A.decode raw of
      Just (A.Object o) -> Just o
      _ -> Nothing

writeSettings :: FilePath -> KM.KeyMap A.Value -> IO ()
writeSettings settingsPath settings =
  BL.writeFile settingsPath (encodePretty' cfg (A.Object settings) <> "\n")
  where cfg = defConfig { confIndent = Spaces 2, confCompare = compare }

hookEvents :: [Text]
hookEvents = ["SessionEnd", "UserPromptSubmit", "PreCompact"]

-- | Hook events in settings.json that contain agent-handler matcher groups.
findAgentHandlerHooks :: FilePath -> IO [Text]
findAgentHandlerHooks settingsPath = do
  msettings <- readSettings settingsPath
  pure $ case msettings of
    Nothing -> []
    Just settings -> case KM.lookup "hooks" settings of
      Just (A.Object hooks) ->
        [ event
        | event <- hookEvents
        , Just (A.Array groups) <- [KM.lookup (Key.fromText event) hooks]
        , any isAgentHandlerHook (V.toList groups)
        ]
      _ -> []

-- | Removes agent-handler hook groups, statusLine, and the Bash(handler *)
-- permission from settings.json.
removeHooks :: FilePath -> IO ()
removeHooks home = do
  let settingsPath = home </> ".claude" </> "settings.json"
  msettings <- readSettings settingsPath
  case msettings of
    Nothing -> pure ()
    Just settings0 -> do
      let hooks0 = case KM.lookup "hooks" settings0 of
            Just (A.Object h) -> h
            _ -> KM.empty
      hooks1 <- foldHooks hooks0 ["SessionStart", "SessionEnd", "UserPromptSubmit", "PreCompact"]
      let settings1 =
            if KM.null hooks1
              then KM.delete "hooks" settings0
              else KM.insert "hooks" (A.Object hooks1) settings0

      settings2 <- case KM.lookup "statusLine" settings1 of
        Just sl | isAgentHandlerHook sl -> do
          putTextLn "  ✓ Removed status line configuration"
          pure (KM.delete "statusLine" settings1)
        _ -> pure settings1

      settings3 <- case KM.lookup "permissions" settings2 of
        Just (A.Object perms) | Just (A.Array allow) <- KM.lookup "allow" perms -> do
          let kept = V.filter (/= A.String "Bash(handler *)") allow
          if V.length kept /= V.length allow
            then do
              putTextLn "  ✓ Removed Bash(handler *) permission"
              pure (KM.insert "permissions"
                     (A.Object (KM.insert "allow" (A.Array kept) perms)) settings2)
            else pure settings2
        _ -> pure settings2

      writeSettings settingsPath settings3
  where
    foldHooks hooks [] = pure hooks
    foldHooks hooks (event : rest) =
      case KM.lookup (Key.fromText event) hooks of
        Just (A.Array groups) -> do
          let kept = V.filter (not . isAgentHandlerHook) groups
          if V.length kept /= V.length groups
            then do
              printfT ["  ✓ Removed ", event, " hook"]
              let hooks' = if V.null kept
                    then KM.delete (Key.fromText event) hooks
                    else KM.insert (Key.fromText event) (A.Array kept) hooks
              foldHooks hooks' rest
            else foldHooks hooks rest
        _ -> foldHooks hooks rest

-- | Whether Bash(handler *) is in the settings.json allow list.
hasHandlerPermission :: FilePath -> IO Bool
hasHandlerPermission settingsPath = do
  msettings <- readSettings settingsPath
  pure $ case msettings of
    Just settings
      | Just (A.Object perms) <- KM.lookup "permissions" settings
      , Just (A.Array allow) <- KM.lookup "allow" perms
      -> V.elem (A.String "Bash(handler *)") allow
    _ -> False
