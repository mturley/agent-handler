-- | Port of cmd/emit.go: emit a new event.
module Handler.Cmd.Emit (emitCommand) where

import Control.Monad (forM)
import Data.Aeson (object, (.=))
import Data.Text (Text)
import Options.Applicative

import Handler.Cli.Common
import Handler.Db (close, handlerHome)
import Handler.Db.Events (Event(..), EventRecipient(..), insertEvent)
import Handler.Discover (resolveSessionId)
import Handler.Util (newUuid, nowIso)

data EmitOpts = EmitOpts
  { eType      :: Text
  , eTitle     :: Text
  , eMessage   :: Text
  , eBody      :: Text
  , eDetail    :: Text
  , eSessionId :: Maybe Text
  , eSource    :: Text
  , eBroadcast :: Bool
  , eTags      :: Text
  , eTo        :: [Text]
  }

emitCommand :: Mod CommandFields NamedCommand
emitCommand = mkCommand "emit" "Emit a new event" (runEmit <$> opts)
  where
    opts = EmitOpts
      <$> strOption (long "type" <> metavar "TYPE"
            <> help "event type: milestone, decision, blocked, unblocked, handoff, followup, status, message (required)")
      <*> strOption (long "title" <> value "" <> help "event title (required)")
      <*> strOption (long "message" <> value "" <> help "alias for --title")
      <*> strOption (long "body" <> value "" <> help "event body")
      <*> strOption (long "detail" <> value "" <> help "alias for --body")
      <*> sessionIdOption
      <*> strOption (long "source" <> value "agent" <> help "event source")
      <*> switch (long "broadcast" <> help "broadcast to all sessions")
      <*> strOption (long "tags" <> value "" <> help "comma-separated tags")
      <*> many (strOption (long "to" <> metavar "TARGET"
            <> help "recipient session IDs or branch names (can specify multiple)"))

runEmit :: EmitOpts -> Ctx -> IO ()
runEmit o ctx = do
  let title = if o.eTitle == "" then o.eMessage else o.eTitle
      body = if o.eBody == "" then o.eDetail else o.eBody
  if title == ""
    then dieText "required flag \"title\" not set (--message is also accepted)"
    else do
      db <- openDb
      eventId <- newUuid
      ts <- nowIso

      -- Session attribution is best-effort: emit works outside a session too.
      sessionId <- case o.eSessionId of
        Just sid | sid /= "" -> pure (Just sid)
        _ -> do
          home <- handlerHome
          either (const Nothing) Just <$> resolveSessionId home

      recipients <- forM o.eTo $ \to -> do
        (rType, rValue) <- resolveRecipient db to
        pure EventRecipient { recipientType = rType, recipientValue = rValue }

      insertEvent db
        Event
          { eventId = eventId
          , ts = ts
          , externalTs = Nothing
          , source = o.eSource
          , sessionId = sessionId
          , eventType = o.eType
          , title = title
          , body = if body == "" then Nothing else Just body
          , author = Nothing
          , authorType = Nothing
          , broadcast = o.eBroadcast
          , tags = if o.eTags == "" then Nothing else Just o.eTags
          }
        recipients
        []
      close db

      if ctx.jsonOutput
        then printJson $ object [ "event_id" .= eventId, "timestamp" .= ts ]
        else do
          putTextLn "✓ Event emitted"
          printfT ["  ID: ", eventId]
          printfT ["  Timestamp: ", ts]
