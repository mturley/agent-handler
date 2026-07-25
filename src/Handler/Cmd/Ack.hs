-- | Port of cmd/ack.go: acknowledge all unread events for a session.
module Handler.Cmd.Ack (ackCommand) where

import Data.Aeson (object, (.=))
import Data.Text (Text)
import qualified Data.Text as T
import Options.Applicative

import Handler.Cli.Common
import Handler.Db (close)
import Handler.Db.Cursors (advanceBothCursors)
import Handler.Db.Events (unreadCountForSession)
import Handler.Util (nowIso)

ackCommand :: Mod CommandFields NamedCommand
ackCommand = mkCommand "ack" "Acknowledge all unread events for a session"
  (runAck <$> sessionIdOption)

runAck :: Maybe Text -> Ctx -> IO ()
runAck msid ctx = do
  db <- openDb
  sessionId <- resolveSessionIdOpt msid
  (unreadCount, _) <- unreadCountForSession db sessionId
  ts <- nowIso
  advanceBothCursors db sessionId ts
  close db
  if ctx.jsonOutput
    then printJson $ object [ "acknowledged" .= unreadCount, "cursor" .= ts ]
    else do
      printfT ["✓ Acknowledged ", T.pack (show unreadCount), " event(s)"]
      printfT ["  Cursor advanced to: ", ts]
