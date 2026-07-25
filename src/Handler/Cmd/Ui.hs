-- | Port of cmd/ui.go: start the web UI server.
module Handler.Cmd.Ui (uiCommand) where

import Control.Monad (unless, void)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.IO as TIO
import Options.Applicative
import System.IO (hFlush, stdout)
import System.Info (os)
import System.Process (spawnProcess)

import Handler.Api (ApiConfig(..), serveApi)
import Handler.Cli.Common
import Handler.Terminal (detect)
import Handler.WebEmbed (webHasContent)

data UiOpts = UiOpts
  { uPort    :: Int
  , uApiOnly :: Bool
  }

uiCommand :: Mod CommandFields NamedCommand
uiCommand = mkCommand "ui" "Start the web UI server" (runUi <$> opts)
  where
    opts = UiOpts
      <$> option auto (long "port" <> value 8420 <> help "HTTP server port")
      <*> switch (long "api-only"
            <> help "serve API only (skip static file serving, for use with a separate dev server)")

runUi :: UiOpts -> Ctx -> IO ()
runUi o _ = do
  if not o.uApiOnly && not webHasContent
    then putTextLn "Web UI not built. Run 'make build-web' first."
    else do
      (backendType, _, _) <- detect
      let cmuxAvailable = backendType == "cmux"

      proceed <-
        if not cmuxAvailable && not o.uApiOnly
          then do
            putTextLn "cmux not detected. Session switching and other cmux features will not be available."
            TIO.putStr "Continue without cmux features? [y/N] "
            hFlush stdout
            answer <- getLine
            pure (answer `elem` ["y", "Y", "yes"])
          else pure True

      if not proceed
        then putTextLn "Aborted."
        else do
          db <- openReadOnlyDb
          unless o.uApiOnly $ do
            let url = "http://localhost:" <> T.pack (show o.uPort)
            putTextLn ("Opening " <> url <> " in browser...")
            openBrowser url
          serveApi ApiConfig
            { db = db
            , cmuxAvailable = cmuxAvailable
            , devMode = o.uApiOnly
            , port = o.uPort
            }

openBrowser :: Text -> IO ()
openBrowser url = case os of
  "darwin" -> void (spawnProcess "open" [T.unpack url])
  "linux"  -> void (spawnProcess "xdg-open" [T.unpack url])
  _        -> pure ()
