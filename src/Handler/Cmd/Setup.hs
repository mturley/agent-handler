-- | Port of cmd/setup.go: set up or update agent-handler skills, hooks,
-- and database.
module Handler.Cmd.Setup (setupCommand) where

import Control.Exception (IOException, try)
import Control.Monad (forM_, unless, void, when)
import qualified Data.Aeson as A
import qualified Data.Aeson.Key as Key
import qualified Data.Aeson.KeyMap as KM
import Data.Aeson.Encode.Pretty (Config(..), Indent(..), defConfig, encodePretty')
import qualified Data.ByteString as BS
import qualified Data.ByteString.Lazy as BL
import Data.Maybe (fromMaybe, isJust)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Vector as V
import Options.Applicative
import System.Directory
  ( createDirectoryIfMissing, createDirectoryLink, doesDirectoryExist
  , doesFileExist, findExecutable, getHomeDirectory, listDirectory
  , pathIsSymbolicLink, removeDirectoryLink, removeDirectoryRecursive
  , removeFile )
import System.Environment (getExecutablePath, lookupEnv)
import System.Exit (ExitCode(..))
import System.FilePath (takeDirectory, takeFileName, (</>))
import System.Posix.Files (setFileMode)
import System.Process (readProcessWithExitCode, spawnProcess, waitForProcess)

import Handler.Cli.Common
import Handler.CmuxConfig
  ( actionShortcut, cmuxConfigFilePath, configureCmuxActions, handlerCmuxActionIDs )
import qualified Handler.Db as Db
import Handler.Db (defaultPath, handlerHome)
import Handler.Embedded (embeddedHooks, embeddedRules, embeddedSkills)
import Handler.Cmd.Uninstall (confirm, isAgentHandlerHook, skillNames)

newtype SetupOpts = SetupOpts { yes :: Bool }

setupCommand :: Mod CommandFields NamedCommand
setupCommand =
  mkCommand "setup" "Set up or update agent-handler skills, hooks, and database"
    (runSetup <$> opts)
  where
    opts = SetupOpts
      <$> switch (long "yes" <> short 'y'
            <> help "skip confirmation prompts (non-interactive mode)")

runSetup :: SetupOpts -> Ctx -> IO ()
runSetup o _ = do
  home <- getHomeDirectory
  handlerDir <- handlerHome
  dbPath <- defaultPath
  let hooksDir = handlerDir </> "hooks"
      skillsDir = handlerDir </> "skills"
      rulesDir = handlerDir </> "rules"
      claudeSkillsDir = home </> ".claude" </> "skills"
      claudeRulesDir = home </> ".claude" </> "rules"
      settingsPath = home </> ".claude" </> "settings.json"

  -- Detect cmux availability early
  insideCmux <- isJust <$> lookupEnv "CMUX_SURFACE_ID"
  cmuxAvailable0 <- do
    onPath <- findExecutable "cmux"
    case onPath of
      Nothing -> pure False
      Just _ -> cmuxConfigFilePath >>= doesFileExist

  cmuxAvailable <-
    if cmuxAvailable0 && not insideCmux && not o.yes
      then do
        putTextLn "\ESC[33m⚠ Not running inside cmux.\ESC[0m"
        putTextLn "cmux was detected but this setup is not running from inside cmux."
        putTextLn "cmux session-switching shortcuts can only be configured from inside cmux."
        putTextLn ""
        ok <- confirm "Continue without cmux actions? (Run 'handler setup' from inside cmux later to add them)"
        if not ok
          then do
            putTextLn "Aborted. Re-run handler setup from inside cmux."
            pure Nothing
          else do
            putTextLn ""
            pure (Just False)
      else pure (Just cmuxAvailable0)

  case cmuxAvailable of
    Nothing -> pure ()
    Just cmuxOk -> do
      putTextLn "agent-handler setup will:"
      putTextLn ""
      printfT ["  Create directory structure at ", T.pack handlerDir]
      printfT ["  Initialize SQLite database at ", T.pack dbPath]
      printfT ["  Extract hooks to ", T.pack hooksDir]
      printfT ["  Extract skills to ", T.pack skillsDir]
      printfT ["  Symlink ", T.pack (show (length skillNames)), " skills into ", T.pack claudeSkillsDir, ":"]
      forM_ skillNames $ \name -> printfT ["    - /", name]
      printfT ["  Install global rules to ", T.pack claudeRulesDir, ":"]
      forM_ embeddedRules $ \(path, _) -> printfT ["    - ", T.pack (takeFileName path)]
      printfT ["  Configure Claude Code hooks in ", T.pack settingsPath, ":"]
      forM_ (["SessionEnd", "UserPromptSubmit", "PreCompact"] :: [Text]) $ \hook ->
        printfT ["    - ", hook]
      printfT ["  Configure status line widget in ", T.pack settingsPath]
      when cmuxOk $ do
        cmuxPath <- cmuxConfigFilePath
        printfT ["  Add cmux actions to ", T.pack cmuxPath, ":"]
        forM_ handlerCmuxActionIDs $ \actionId ->
          printfT ["    - ", actionId, " (", fromMaybe "" (actionShortcut actionId), ")"]
      (completionShell, completionTarget) <- detectCompletion
      when (completionTarget /= "") $ do
        exists <- doesFileExist completionTarget
        when exists $ printfT ["  Update shell completion: ", T.pack completionTarget]
      putTextLn "  Offer to auto-allow handler CLI commands (Bash permission)"
      putTextLn "  Offer to configure external service API tokens (GitHub, Jira)"
      putTextLn "  Offer to install watchers for configured services"
      putTextLn ""

      proceed <- if o.yes then pure True else confirm "Proceed?"
      if not proceed
        then putTextLn "Aborted."
        else do
          putTextLn ""

          -- 1. Create directory structure
          let dataDir = handlerDir </> "data"
          forM_ [handlerDir, dataDir, dataDir </> "sessions", dataDir </> "logs", hooksDir, skillsDir] $
            createDirectoryIfMissing True
          printfT ["  ✓ Created directory structure at ", T.pack handlerDir]

          -- 2. Initialize database
          database <- Db.open dbPath
          Db.close database
          printfT ["  ✓ Initialized database at ", T.pack dbPath, "\n"]

          -- 3. Extract hooks
          when (null embeddedHooks) $ dieText "no hooks found in embedded data"
          forM_ embeddedHooks $ \(hookPath, contents) -> do
            let dst = hooksDir </> takeFileName hookPath
            BS.writeFile dst contents
            setFileMode dst 0o755
            printfT ["  ✓ Extracted ", T.pack (takeFileName hookPath)]

          -- 4. Clean stale skills from previous installs
          putTextLn ""
          entries <- either (const []) id
            <$> (try (listDirectory skillsDir) :: IO (Either IOException [FilePath]))
          forM_ entries $ \entry -> do
            isDir <- doesDirectoryExist (skillsDir </> entry)
            when (isDir && T.pack entry `notElem` skillNames) $ do
              removeDirectoryRecursive (skillsDir </> entry)
              let staleSymlink = claudeSkillsDir </> entry
              isLink <- either (const False) id
                <$> (try (pathIsSymbolicLink staleSymlink) :: IO (Either IOException Bool))
              when isLink $ void (try (removeDirectoryLink staleSymlink) :: IO (Either IOException ()))
              printfT ["  ✓ Removed stale skill ", T.pack entry]

          -- 5. Extract skills and create symlinks
          createDirectoryIfMissing True claudeSkillsDir
          forM_ skillNames $ \skillName -> do
            let srcPath = T.unpack skillName </> "SKILL.md"
            case lookup srcPath embeddedSkills of
              Nothing -> dieText ("reading embedded skill " <> skillName <> ": not found")
              Just contents -> do
                let dstDir = skillsDir </> T.unpack skillName
                createDirectoryIfMissing True dstDir
                BS.writeFile (dstDir </> "SKILL.md") contents
                let symlinkDst = claudeSkillsDir </> T.unpack skillName
                existsLink <- either (const False) id
                  <$> (try (pathIsSymbolicLink symlinkDst) :: IO (Either IOException Bool))
                existsFile <- doesFileExist symlinkDst
                existsDir <- doesDirectoryExist symlinkDst
                when (existsLink || existsFile || existsDir) $ do
                  void (try (removeDirectoryLink symlinkDst) :: IO (Either IOException ()))
                  void (try (removeFile symlinkDst) :: IO (Either IOException ()))
                createDirectoryLink dstDir symlinkDst
                printfT ["  ✓ ", skillName, " -> ", T.pack dstDir]

          -- 6. Extract rules and install to ~/.claude/rules/
          putTextLn ""
          createDirectoryIfMissing True rulesDir
          createDirectoryIfMissing True claudeRulesDir
          forM_ embeddedRules $ \(rulePath, contents) -> do
            let baseName = takeFileName rulePath
            BS.writeFile (rulesDir </> baseName) contents
            BS.writeFile (claudeRulesDir </> baseName) contents
            printfT ["  ✓ Installed rule ", T.pack baseName]

          -- 8. Configure Claude Code hooks and status line
          putTextLn ""
          configureHooks home hooksDir
          configureStatusLine home handlerDir

          -- 9. Update or suggest shell completion
          when (completionTarget /= "") $ do
            exists <- doesFileExist completionTarget
            if exists
              then do
                writeCompletion completionShell completionTarget
                printfT ["  ✓ Updated shell completion: ", T.pack completionTarget]
              else do
                putTextLn "\n  \ESC[2mtip:\ESC[0m Shell completion is not installed."
                putTextLn "       Run \ESC[1mhandler completion --help\ESC[0m for setup instructions."

          -- 10. Configure cmux actions (if cmux is available)
          if cmuxOk
            then configureCmuxActions
            else putTextLn "\n  \ESC[2mcmux not detected. Optional cmux features (session switching shortcuts)\n  are available — run 'handler setup' again after installing cmux.\ESC[0m"

          -- 11. Offer to auto-allow handler commands
          putTextLn ""
          configurePermissions o.yes home

          -- 12. Set up external service watchers (auth + install)
          if o.yes
            then putTextLn "\n  Skipping watcher setup (non-interactive mode). Run 'handler watcher install' to configure."
            else do
              putTextLn "\nSetting up external service watchers..."
              result <- try (spawnProcess "handler" ["watcher", "install"] >>= waitForProcess)
                :: IO (Either IOException ExitCode)
              void (pure result)

          putTextLn "\n✓ Installation complete!"
          printfT ["\n  All files installed to ", T.pack handlerDir]
          putTextLn "  To update, run 'handler update'."
          putTextLn "  To uninstall, run 'handler uninstall'."
          putTextLn "\nTest with: handler status"

readSettingsOrDie :: FilePath -> IO (KM.KeyMap A.Value)
readSettingsOrDie settingsPath = do
  result <- try (BL.readFile settingsPath) :: IO (Either IOException BL.ByteString)
  case result of
    Left _ -> pure KM.empty
    Right raw -> case A.decode raw of
      Just (A.Object obj) -> pure obj
      _ -> dieText ("failed to parse " <> T.pack settingsPath)

writeSettings :: FilePath -> KM.KeyMap A.Value -> IO ()
writeSettings settingsPath settings = do
  createDirectoryIfMissing True (takeDirectory settingsPath)
  BL.writeFile settingsPath (encodePretty' cfg (A.Object settings) <> "\n")
  where cfg = defConfig { confIndent = Spaces 2, confCompare = compare }

-- | Installs the agent-handler hook matcher groups in settings.json,
-- preserving matcher groups from other tools.
configureHooks :: FilePath -> FilePath -> IO ()
configureHooks home hooksDir = do
  let settingsPath = home </> ".claude" </> "settings.json"
      hookEntries :: [(Text, FilePath, Int)]
      hookEntries =
        [ ("SessionEnd", "session_end.sh", 10)
        , ("UserPromptSubmit", "user_prompt_submit.sh", 5)
        , ("PreCompact", "pre_compact.sh", 10)
        ]
  settings <- readSettingsOrDie settingsPath
  let existingHooks0 = case KM.lookup "hooks" settings of
        Just (A.Object h) -> h
        _ -> KM.empty
  existingHooks <- foldHookEntries existingHooks0 hookEntries
  writeSettings settingsPath (KM.insert "hooks" (A.Object existingHooks) settings)
  where
    foldHookEntries hooks [] = pure hooks
    foldHookEntries hooks ((event, script, timeout) : rest) = do
      let scriptPath = hooksDir </> script
          newMatcherGroup = A.object
            [ "matcher" A..= ("" :: Text)
            , "hooks" A..=
                [ A.object
                    [ "type" A..= ("command" :: Text)
                    , "command" A..= T.pack scriptPath
                    , "timeout" A..= timeout
                    ]
                ]
            ]
          kept = case KM.lookup (Key.fromText event) hooks of
            Just (A.Array groups) -> V.toList (V.filter (not . isAgentHandlerHook) groups)
            _ -> []
      printfT ["  ✓ ", event, " -> ", T.pack scriptPath]
      foldHookEntries
        (KM.insert (Key.fromText event) (A.Array (V.fromList (kept ++ [newMatcherGroup]))) hooks)
        rest

-- | Points the Claude Code status line at the handler statusline script.
configureStatusLine :: FilePath -> FilePath -> IO ()
configureStatusLine home handlerDir = do
  let settingsPath = home </> ".claude" </> "settings.json"
      statuslineScript = handlerDir </> "hooks" </> "statusline.sh"
  settings <- readSettingsOrDie settingsPath
  let statusLine = A.object
        [ "type" A..= ("command" :: Text)
        , "command" A..= T.pack statuslineScript
        , "refreshInterval" A..= (10 :: Int)
        ]
  printfT ["  ✓ Status line -> ", T.pack statuslineScript, " (refresh every 10s)"]
  writeSettings settingsPath (KM.insert "statusLine" statusLine settings)

-- | Offers to add the Bash(handler *) permission to settings.json.
configurePermissions :: Bool -> FilePath -> IO ()
configurePermissions yes home = do
  let settingsPath = home </> ".claude" </> "settings.json"
      permission = "Bash(handler *)" :: Text
  result <- try (BL.readFile settingsPath) :: IO (Either IOException BL.ByteString)
  case result of
    Left _ -> pure ()
    Right raw -> case A.decode raw of
      Just (A.Object settings) -> do
        let mperms = case KM.lookup "permissions" settings of
              Just (A.Object p) -> Just p
              _ -> Nothing
            alreadyAllowed = case mperms >>= KM.lookup "allow" of
              Just (A.Array allow) -> V.elem (A.String permission) allow
              _ -> False
        if alreadyAllowed
          then printfT ["  ✓ Permission already configured: ", permission]
          else do
            putTextLn "  Auto-allow all handler CLI commands in Claude Code sessions?"
            printfT ["  This adds \"", permission, "\" to your Claude Code permissions so agents"]
            putTextLn "  can run handler commands without prompting for approval.\n"
            proceed <- if yes then pure True else confirm "  Add permission?"
            if not proceed
              then putTextLn "  Skipped. You can add it manually later in ~/.claude/settings.json"
              else do
                let perms = fromMaybe KM.empty mperms
                    allow = case KM.lookup "allow" perms of
                      Just (A.Array a) -> a
                      _ -> V.empty
                    perms' = KM.insert "allow" (A.Array (V.snoc allow (A.String permission))) perms
                writeSettings settingsPath (KM.insert "permissions" (A.Object perms') settings)
                printfT ["  ✓ Added permission: ", permission]
      _ -> pure ()

-- | Shell completion detection: (shell, target path or \"\").
detectCompletion :: IO (Text, FilePath)
detectCompletion = do
  shell <- detectShell
  path <- completionPath shell
  pure (shell, path)

detectShell :: IO Text
detectShell = do
  s <- lookupEnv "SHELL"
  pure $ case s of
    Just sh | sh /= "" -> T.pack (takeFileName sh)
    _ -> ""

completionPath :: Text -> IO FilePath
completionPath = \case
  "zsh" -> do
    brewPath <- do
      (code, out, _) <- readProcessWithExitCode "brew" ["--prefix"] ""
      case code of
        ExitSuccess -> do
          let p = T.unpack (T.strip (T.pack out)) </> "share" </> "zsh" </> "site-functions" </> "_handler"
          ok <- doesDirectoryExist (takeDirectory p)
          pure (if ok then Just p else Nothing)
        _ -> pure Nothing
    case brewPath of
      Just p -> pure p
      Nothing -> do
        home <- getHomeDirectory
        let p = home </> ".zsh" </> "completions" </> "_handler"
        ok <- doesDirectoryExist (takeDirectory p)
        pure (if ok then p else "")
  "bash" -> do
    hits <- mapM (\d -> (,) d <$> doesDirectoryExist d)
      ["/etc/bash_completion.d", "/usr/local/etc/bash_completion.d"]
    pure $ case [d | (d, True) <- hits] of
      (d : _) -> d </> "handler"
      [] -> ""
  "fish" -> do
    home <- getHomeDirectory
    let dir = home </> ".config" </> "fish" </> "completions"
    ok <- doesDirectoryExist dir
    pure (if ok then dir </> "handler.fish" else "")
  _ -> pure ""

-- | Regenerates the shell completion script using optparse-applicative's
-- built-in --\<shell\>-completion-script support.
writeCompletion :: Text -> FilePath -> IO ()
writeCompletion shell path = do
  exe <- getExecutablePath
  (code, out, _) <- readProcessWithExitCode exe
    ["--" <> T.unpack shell <> "-completion-script", exe] ""
  case code of
    ExitSuccess -> writeFile path (T.unpack (T.strip (T.pack out)))
    _ -> dieText "generating completion script failed"
