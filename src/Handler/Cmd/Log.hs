-- | Port of cmd/log_cmd.go: show the event log for a session.
module Handler.Cmd.Log (logCommand) where

import Control.Monad (foldM, when)
import Data.Aeson (toJSON)
import Data.Map.Strict (Map)
import qualified Data.Map.Strict as Map
import Data.Maybe (fromMaybe, isJust)
import Data.Text (Text)
import qualified Data.Text as T
import Data.Time.Clock (addUTCTime, getCurrentTime)
import Data.Time.Format (defaultTimeLocale, formatTime)
import Options.Applicative

import Handler.Cli.Common
import Handler.Db (Db, close)
import Handler.Db.Cursors (advanceBothCursors, advanceCursor, getCursor)
import Handler.Db.Events (Event(..), EventFilter(..), emptyFilter, eventToJson, queryEvents)
import Handler.Db.Sessions (Session(..), getSession)
import Handler.Util (nowIso)

data LogOpts = LogOpts
  { lSessionId   :: Maybe Text
  , lLimit       :: Int
  , lSince       :: Text
  , lGlobal      :: Bool
  , lSinceCursor :: Bool
  , lAgentOnly   :: Bool
  }

logCommand :: Mod CommandFields NamedCommand
logCommand = mkCommand "log" "Show event log for a session" (runLog <$> opts)
  where
    opts = LogOpts
      <$> sessionIdOption
      <*> option auto (long "limit" <> value 50 <> help "maximum number of events to show")
      <*> strOption (long "since" <> value "" <> help "show events since this timestamp (RFC3339)")
      <*> switch (long "global" <> help "show events from all sessions and watchers")
      <*> switch (long "since-cursor" <> help "show events since this session's cursor and advance it")
      <*> switch (long "agent-only" <> help "with --since-cursor, advance only the agent cursor (not human)")

runLog :: LogOpts -> Ctx -> IO ()
runLog o ctx = do
  db <- if o.lSinceCursor then openDb else openReadOnlyDb

  sessionId <-
    if not o.lGlobal || o.lSinceCursor
      then resolveSessionIdOpt o.lSessionId
      else pure ""

  since <-
    if o.lSinceCursor
      then do
        cursor <- getCursor db sessionId
        if cursor == ""
          then do
            -- No cursor exists, default to last 24 hours
            now <- getCurrentTime
            pure $ Just $ T.pack $ formatTime defaultTimeLocale "%Y-%m-%dT%H:%M:%SZ"
                     (addUTCTime (-24 * 3600) now)
          else pure (Just cursor)
      else pure $ if o.lSince == "" then Nothing else Just o.lSince

  let filter' = emptyFilter
        { filterSessionId = if o.lGlobal then Nothing else Just sessionId
        , filterSince = since
        , filterLimit = o.lLimit
        }
  events <- queryEvents db filter'

  when (o.lSinceCursor && not (null events)) $ do
    ts <- nowIso
    if o.lAgentOnly
      then advanceCursor db sessionId ts
      else advanceBothCursors db sessionId ts

  if ctx.jsonOutput
    then printJson (toJSON (map eventToJson events))
    else if null events
      then putTextLn "No events found"
      else do
        sessionNames <-
          if o.lGlobal
            then foldM (cacheName db) Map.empty events
            else pure Map.empty

        if o.lGlobal
          then printfT ["Global event log (showing ", T.pack (show (length events)), "):\n"]
          else printfT ["Event log for session ", sessionId, " (showing ", T.pack (show (length events)), "):\n"]

        -- Events are in DESC order from the query; reverse for timeline display
        mapM_ (printEvent sessionNames) (reverse events)
  close db
  where
    cacheName db cache e = case e.sessionId of
      Just sid | not (Map.member sid cache) -> do
        msession <- getSession db sid
        pure $ Map.insert sid (maybe sid (.sessionName) msession) cache
      _ -> pure cache

    printEvent :: Map Text Text -> Event -> IO ()
    printEvent sessionNames e = do
      let attribution
            | not o.lGlobal = ""
            | Just sid <- e.sessionId = "[" <> fromMaybe sid (Map.lookup sid sessionNames) <> "] "
            | otherwise = "[" <> e.source <> "] "
      printfT ["  ", attribution, e.ts, " [", e.eventType, "] ", e.title]
      printfT ["  Author: ", fromMaybe "-" e.author, " | Source: ", e.source]
      when (isJust e.body && e.body /= Just "") $
        printfT ["  ", fromMaybe "" e.body]
      putTextLn ""
