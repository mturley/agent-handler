-- | Port of cmd/whoami.go: print the current session ID.
module Handler.Cmd.Whoami (whoamiCommand) where

import qualified Data.Text.IO as TIO
import Options.Applicative

import Handler.Cli.Common
import Handler.Db (handlerHome)
import Handler.Discover (resolveSessionId)

whoamiCommand :: Mod CommandFields NamedCommand
whoamiCommand = mkCommand "whoami" "Print the current session ID" (pure runWhoami)

runWhoami :: Ctx -> IO ()
runWhoami _ = do
  home <- handlerHome
  resolveSessionId home >>= \case
    Right sessionId -> TIO.putStr sessionId
    Left _ -> dieText "no session registered for this process. Run 'handler register' or start a new prompt first"
