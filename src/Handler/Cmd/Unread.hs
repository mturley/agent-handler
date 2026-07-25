-- | Port of cmd/unread.go: list unread events for a session.
module Handler.Cmd.Unread (unreadCommand) where

import Control.Monad (unless, when)
import Data.Aeson (toJSON)
import Data.Maybe (fromMaybe, isJust)
import Data.Text (Text)
import qualified Data.Text as T
import Options.Applicative

import Handler.Cli.Common
import Handler.Db (close)
import Handler.Db.Cursors (advanceBothCursors, advanceCursor)
import Handler.Db.Events
  ( Event(..), eventToJson, globalUnreadCountForSession, globalUnreadForSession
  , unreadCountForSession, unreadForSession )
import Handler.Util (nowIso)

data UnreadOpts = UnreadOpts
  { uSessionId :: Maybe Text
  , uAck       :: Bool
  , uAgentOnly :: Bool
  , uCount     :: Bool
  , uGlobal    :: Bool
  }

unreadCommand :: Mod CommandFields NamedCommand
unreadCommand = mkCommand "unread" "List unread events for a session" (runUnread <$> opts)
  where
    opts = UnreadOpts
      <$> sessionIdOption
      <*> switch (long "ack" <> help "acknowledge events after reading")
      <*> switch (long "agent-only" <> help "with --ack, advance only the agent cursor (not human cursor)")
      <*> switch (long "count" <> help "only print the unread count")
      <*> switch (long "global" <> help "show all events since cursor, not just those targeted at this session (for handler sessions)")

runUnread :: UnreadOpts -> Ctx -> IO ()
runUnread o ctx = do
  db <- if o.uAck then openDb else openReadOnlyDb
  sessionId <- resolveSessionIdOpt o.uSessionId

  if o.uCount
    then do
      (count, _) <- if o.uGlobal
        then globalUnreadCountForSession db sessionId
        else unreadCountForSession db sessionId
      putTextLn (T.pack (show count))
    else do
      allEvents <- if o.uGlobal
        then globalUnreadForSession db sessionId
        else unreadForSession db sessionId

      -- In global mode, filter out events originated by this session
      let events = if o.uGlobal
            then [e | e <- allEvents, e.sessionId /= Just sessionId]
            else allEvents

      when (o.uAck && not (null events)) $ do
        ts <- nowIso
        if o.uAgentOnly
          then advanceCursor db sessionId ts
          else advanceBothCursors db sessionId ts

      if ctx.jsonOutput
        then printJson (toJSON (map eventToJson events))
        else if null events
          then putTextLn "No unread events"
          else do
            printfT ["Unread events (", T.pack (show (length events)), "):\n"]
            mapM_ printEvent events
  close db
  where
    printEvent e = do
      printfT ["  [", e.eventType, "] ", e.title]
      printfT ["  Author: ", fromMaybe "-" e.author, " | Time: ", e.ts]
      unless (not (isJust e.body) || e.body == Just "") $
        printfT ["  ", fromMaybe "" e.body]
      putTextLn ""
