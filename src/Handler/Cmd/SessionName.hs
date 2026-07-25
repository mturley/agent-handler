-- | Port of cmd/session_name.go: print the current session's name.
module Handler.Cmd.SessionName (sessionNameCommand) where

import Data.Text (Text)
import Options.Applicative

import Handler.Cli.Common
import Handler.Db (close)
import Handler.Db.Sessions (Session(..), getSession)

newtype SessionNameOpts = SessionNameOpts
  { snSessionId :: Maybe Text
  }

sessionNameCommand :: Mod CommandFields NamedCommand
sessionNameCommand = mkCommand "session-name" "Print the current session's name" (runSessionName <$> opts)
  where
    opts = SessionNameOpts <$> sessionIdOption

runSessionName :: SessionNameOpts -> Ctx -> IO ()
runSessionName o _ctx = do
  sessionId <- resolveSessionIdOpt o.snSessionId
  db <- openReadOnlyDb
  msession <- getSession db sessionId
  session <- maybe (dieText ("session not found: " <> sessionId)) pure msession
  putTextLn session.sessionName
  close db
