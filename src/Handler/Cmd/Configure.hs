-- | Port of cmd/configure.go: configure session settings.
module Handler.Cmd.Configure (configureCommand) where

import Control.Monad (when)
import Data.Aeson (object, (.=))
import Data.Maybe (fromMaybe)
import Data.Text (Text)
import qualified Data.Text as T
import Options.Applicative

import Handler.Cli.Common
import Handler.Db (close)
import Handler.Db.Cursors (catchUpHumanCursor, clearHumanCursor)
import Handler.Db.Sessions (Session(..), configureSession, getSession)
import Handler.Util (textShow)

data ConfigureOpts = ConfigureOpts
  { cSessionId :: Maybe Text
  , cInboxMode :: Text
  , cInterval  :: Int
  , cRole      :: Maybe Text
  , cGet       :: Text
  }

configureCommand :: Mod CommandFields NamedCommand
configureCommand = mkCommand "configure" "Configure session settings" (runConfigure <$> opts)
  where
    opts = ConfigureOpts
      <$> sessionIdOption
      <*> strOption (long "inbox-mode" <> value "" <> help "inbox mode (manual, on-submit, auto)")
      <*> option auto (long "auto-poll-interval" <> value 0
            <> help "auto-poll interval in seconds (for auto mode)")
      <*> optional (strOption (long "role" <> help "session role (handler, or empty to clear)"))
      <*> strOption (long "get" <> value ""
            <> help "get a specific setting value (inbox-mode, auto-poll-interval, role)")

runConfigure :: ConfigureOpts -> Ctx -> IO ()
runConfigure o ctx = do
  sessionId <- resolveSessionIdOpt o.cSessionId

  if o.cGet /= ""
    then do
      db <- openReadOnlyDb
      msession <- getSession db sessionId
      case msession of
        Nothing -> dieText ("session " <> T.pack (show sessionId) <> " not found")
        Just session -> case o.cGet of
          "inbox-mode" -> putTextLn session.inboxMode
          g | g == "auto-poll-interval" || g == "auto_poll_interval" ->
            putTextLn (maybe "null" textShow session.autoPollInterval)
          "role" -> putTextLn session.role
          other -> dieText ("unknown setting: " <> other
                            <> " (valid: inbox-mode, auto-poll-interval, role)")
      close db
    else do
      when (o.cInboxMode == "" && o.cInterval == 0 && o.cRole == Nothing) $
        dieText "at least one of --inbox-mode, --auto-poll-interval, or --role must be provided"

      db <- openDb
      msession <- getSession db sessionId
      case msession of
        Nothing -> dieText ("session " <> T.pack (show sessionId) <> " not found")
        Just session -> do
          let finalInboxMode = if o.cInboxMode == "" then session.inboxMode else o.cInboxMode
              finalAutoPoll = if o.cInterval > 0 then Just o.cInterval else session.autoPollInterval
          configureSession db sessionId finalInboxMode finalAutoPoll o.cRole

          -- Initialize human cursor when entering auto mode
          when (o.cInboxMode == "auto" && session.inboxMode /= "auto") $
            catchUpHumanCursor db sessionId
          -- Clear human cursor when leaving auto mode
          when (o.cInboxMode /= "" && o.cInboxMode /= "auto" && session.inboxMode == "auto") $
            clearHumanCursor db sessionId

          -- Re-fetch session to get final state
          mfinal <- getSession db sessionId
          case mfinal of
            Nothing -> dieText "failed to get session"
            Just final ->
              if ctx.jsonOutput
                then printJson $ object
                  [ "session_id" .= sessionId
                  , "inbox_mode" .= final.inboxMode
                  , "auto_poll_interval" .= final.autoPollInterval
                  , "role" .= final.role
                  ]
                else do
                  printfT ["✓ Configured session ", sessionId]
                  printfT ["  Inbox mode: ", final.inboxMode]
                  case final.autoPollInterval of
                    Just interval -> printfT ["  Auto-poll interval: ", textShow interval, " seconds"]
                    Nothing -> pure ()
                  when (final.role /= "") $
                    printfT ["  Role: ", final.role]
      close db
