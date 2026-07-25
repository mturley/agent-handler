-- | Port of cmd/debug.go: toggle debug info in the statusline.
module Handler.Cmd.Debug (debugCommand) where

import Data.Text (Text)
import Options.Applicative

import Handler.Cli.Common
import Handler.Config (Config(..), configDefaultPath, readConfig, writeConfig)

debugCommand :: Mod CommandFields NamedCommand
debugCommand = mkCommand "debug" "Toggle debug info in the statusline"
  (runDebug <$> strArgument (metavar "enable|disable"))

runDebug :: Text -> Ctx -> IO ()
runDebug action _
  | action /= "enable" && action /= "disable" =
      dieText "usage: handler debug [enable|disable]"
  | otherwise = do
      cfgPath <- configDefaultPath
      cfg <- readConfig cfgPath
      let cfg' = cfg { debug = action == "enable" }
      writeConfig cfgPath cfg'
      putTextLn $ if cfg'.debug
        then "✓ Debug mode enabled — statusline will show debug info for all sessions"
        else "✓ Debug mode disabled"
