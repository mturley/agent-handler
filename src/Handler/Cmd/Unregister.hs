-- | Port of cmd/unregister.go: archive a session and clean up its state.
module Handler.Cmd.Unregister (unregisterCommand) where

import Control.Exception (IOException, try)
import Control.Monad (when)
import Data.Aeson (object, (.=))
import Data.Text (Text)
import qualified Data.Text as T
import Options.Applicative
import System.Directory (removeFile)
import System.FilePath (takeDirectory, (</>))

import Handler.Cli.Common
import Handler.Db (close, defaultPath)
import Handler.Db.Events (Event(..), insertEvent)
import Handler.Db.Sessions (Session(..), archiveSessions, configureSession, getSession)
import Handler.Db.Subscriptions (softDeleteSubscriptionsForSession)
import Handler.Util (newUuid, nowIso)

newtype UnregisterOpts = UnregisterOpts
  { uSessionId :: Maybe Text
  }

unregisterCommand :: Mod CommandFields NamedCommand
unregisterCommand = mkCommand "unregister" "Unregister a Claude Code agent session" (runUnregister <$> opts)
  where
    opts = UnregisterOpts <$> sessionIdOption

runUnregister :: UnregisterOpts -> Ctx -> IO ()
runUnregister o ctx = do
  db <- openDb
  sessionId <- resolveSessionIdOpt o.uSessionId

  -- Find PID before archiving
  msession <- getSession db sessionId
  session <- maybe (dieText ("session not found: " <> sessionId)) pure msession

  archived <- archiveSessions db [sessionId]
  when (archived == 0) $ dieText ("session not found: " <> sessionId)

  -- Reset inbox mode to manual
  configureSession db sessionId "manual" Nothing Nothing

  -- Soft-delete all subscriptions for this session
  subsDeleted <- softDeleteSubscriptionsForSession db sessionId

  -- Emit session_end event (not addressed to anyone — audit trail only)
  now <- nowIso
  eid <- newUuid
  insertEvent db
    Event
      { eventId = eid, ts = now, externalTs = Nothing
      , source = "handler", sessionId = Just sessionId
      , eventType = "session_end"
      , title = "Session " <> sessionId <> " ended"
      , body = Nothing, author = Nothing, authorType = Nothing
      , broadcast = False, tags = Nothing
      }
    [] []

  -- Clean up PID cache file; ignore errors — file may not exist
  dbPath <- defaultPath
  _ <- try (removeFile (takeDirectory dbPath </> "sessions" </> show session.pid))
        :: IO (Either IOException ())

  if ctx.jsonOutput
    then printJson $ object
      [ "session_id" .= sessionId
      , "status" .= ("archived" :: Text)
      , "subscriptions_deleted" .= subsDeleted
      ]
    else do
      printfT ["✓ Unregistered session ", sessionId]
      putTextLn "  Status: archived"
      when (subsDeleted > 0) $
        printfT ["  Subscriptions deleted: ", T.pack (show subsDeleted)]
  close db
