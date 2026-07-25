-- | Port of cmd/claude.go: start a peekable Claude session.
module Handler.Cmd.Claude (claudeCommand) where

import Control.Monad (void)
import Data.Text (Text)
import qualified Data.Text as T
import Options.Applicative
import System.Directory (findExecutable)
import System.Environment (setEnv)
import System.Exit (exitWith)
import System.IO (hFlush, stdout)
import System.Posix.Process (executeFile)
import System.Process (readProcessWithExitCode, spawnProcess, waitForProcess)
import System.Random (randomRIO)
import Text.Printf (printf)

import Handler.Cli.Common (Ctx, NamedCommand, dieText, putTextLn)
import Handler.Terminal (detect)

-- | Flag parsing is disabled (cobra DisableFlagParsing): everything after
-- \"claude\" is forwarded verbatim, so no helper wrapper here.
claudeCommand :: Mod CommandFields NamedCommand
claudeCommand =
  command "claude" $ info
    (fmap ((,) "claude") (runClaude <$> many (strArgument (metavar "CLAUDE-ARGS"))))
    ( progDesc "Start a peekable Claude session"
    <> forwardOptions <> noIntersperse )

runClaude :: [Text] -> Ctx -> IO ()
runClaude args _ = do
  mclaudeBin <- findExecutable "claude"
  case mclaudeBin of
    Nothing -> dieText "claude not found on PATH"
    Just claudeBin -> do
      (backendType, _, _) <- detect
      let execClaude = do
            setEnv "HANDLER_MANAGED" "1"
            executeFile claudeBin False (map T.unpack args) Nothing
      case backendType of
        "cmux" -> execClaude
        "tmux" -> do
          -- Set pane title to handler:pending
          void $ readProcessWithExitCode "tmux" ["select-pane", "-T", "handler:pending"] ""
          execClaude
        _ -> do
          -- Outside both — prompt user
          putTextLn "No tmux or cmux detected. Start a tmux session for peek support? [y/N]"
          hFlush stdout
          answer <- T.toLower . T.strip . T.pack <$> getLine
          if answer == "y" || answer == "yes"
            then do
              suffix <- randomRIO (0, 99999 :: Int)
              let sessionName = printf "handler-%05d" suffix :: String
              ph <- spawnProcess "tmux"
                ([ "new-session", "-s", sessionName
                 , "-e", "HANDLER_MANAGED=1", claudeBin
                 ] ++ map T.unpack args)
              code <- waitForProcess ph
              exitWith code
            else execClaude
