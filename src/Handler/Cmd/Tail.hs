-- | Port of cmd/tail.go: watch for new events in real-time.
module Handler.Cmd.Tail (tailCommand) where

import Control.Concurrent (threadDelay)
import Control.Exception (AsyncException(..), catch, throwIO)
import Control.Monad (foldM, unless, when)
import qualified Data.Aeson as A
import qualified Data.ByteString.Lazy.Char8 as BL
import Data.Maybe (fromMaybe, isJust)
import Data.Text (Text)
import Options.Applicative

import Handler.Cli.Common
import Handler.Db (Db)
import Handler.Db.Events (Event(..), EventFilter(..), emptyFilter, eventToJson, queryEvents)
import Handler.Util (nowIso)

data TailOpts = TailOpts
  { tSource  :: Text
  , tType    :: Text
  , tSession :: Text
  }

tailCommand :: Mod CommandFields NamedCommand
tailCommand = mkCommand "tail" "Watch for new events in real-time" (runTail <$> opts)
  where
    opts = TailOpts
      <$> strOption (long "source" <> value "" <> help "filter by event source")
      <*> strOption (long "type" <> value "" <> help "filter by event type")
      <*> strOption (long "session" <> value "" <> help "filter by session ID")

runTail :: TailOpts -> Ctx -> IO ()
runTail o ctx = do
  db <- openReadOnlyDb
  cursor0 <- nowIso

  let filter' = emptyFilter
        { filterSource = if o.tSource == "" then Nothing else Just o.tSource
        , filterType = if o.tType == "" then Nothing else Just o.tType
        , filterSessionId = if o.tSession == "" then Nothing else Just o.tSession
        , filterLimit = 100
        }

  putTextLn "Watching for events... (Ctrl+C to stop)"
  when (o.tSource /= "") $ printfT ["  Source filter: ", o.tSource]
  when (o.tType /= "") $ printfT ["  Type filter: ", o.tType]
  when (o.tSession /= "") $ printfT ["  Session filter: ", o.tSession]
  putTextLn ""

  loop db filter' cursor0 `catch` \case
    UserInterrupt -> putTextLn "\nStopped"
    e -> throwIO e
  where
    loop :: Db -> EventFilter -> Text -> IO a
    loop db filter' cursor = do
      threadDelay 1_000_000
      events <- queryEvents db filter' { filterSince = Just cursor }
      -- Events are in DESC order, reverse for chronological display
      cursor' <- foldM printAndAdvance cursor (reverse events)
      loop db filter' cursor'

    printAndAdvance cursor e = do
      if ctx.jsonOutput
        then BL.putStrLn (A.encode (eventToJson e))
        else do
          printfT [e.ts, " [", e.eventType, "] ", e.title, " (by ", fromMaybe "-" e.author, ")"]
          unless (not (isJust e.body) || e.body == Just "") $
            printfT ["  ", fromMaybe "" e.body]
      pure (max cursor e.ts)
