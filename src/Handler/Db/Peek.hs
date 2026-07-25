-- | Port of db/peek.go: cached per-session "peek" content for the statusline.
module Handler.Db.Peek
  ( PeekState(..)
  , peekStateToJson
  , upsertPeekState
  , getPeekState
  , listPeekStates
  , peekStatesAgeSeconds
  , deletePeekStatesForSessions
  ) where

import Control.Monad (unless)
import Data.Aeson (Value, object, (.=))
import Data.Text (Text)
import qualified Data.Text as T
import Data.Time.Clock (diffUTCTime, getCurrentTime)
import Data.Time.Format (defaultTimeLocale, parseTimeM)
import qualified Database.SQLite.Simple as SQL

import Handler.Db (Db(..))

data PeekState = PeekState
  { sessionId  :: Text
  , content    :: Text
  , needsInput :: Bool
  , reason     :: Text
  , updatedAt  :: Text
  } deriving (Show, Eq)

instance SQL.FromRow PeekState where
  fromRow = do
    sessionId <- SQL.field
    content <- SQL.field
    needsInputInt <- SQL.field
    reason <- SQL.field
    updatedAt <- SQL.field
    pure PeekState { needsInput = (needsInputInt :: Int) == 1, .. }

peekStateToJson :: PeekState -> Value
peekStateToJson p = object
  [ "session_id" .= p.sessionId
  , "content" .= p.content
  , "needs_input" .= p.needsInput
  , "reason" .= p.reason
  , "updated_at" .= p.updatedAt
  ]

upsertPeekState :: Db -> Text -> Text -> Bool -> Text -> Text -> IO ()
upsertPeekState db sid content needsInput reason updatedAt =
  SQL.execute db.conn
    "INSERT INTO peek_state (session_id, content, needs_input, reason, updated_at)\
    \ VALUES (?, ?, ?, ?, ?)\
    \ ON CONFLICT(session_id) DO UPDATE SET\
    \  content = excluded.content,\
    \  needs_input = excluded.needs_input,\
    \  reason = excluded.reason,\
    \  updated_at = excluded.updated_at"
    (sid, content, if needsInput then 1 else 0 :: Int, reason, updatedAt)

getPeekState :: Db -> Text -> IO (Maybe PeekState)
getPeekState db sid = do
  rows <- SQL.query db.conn
    "SELECT session_id, content, needs_input, COALESCE(reason, ''), updated_at\
    \ FROM peek_state WHERE session_id = ?"
    (SQL.Only sid)
  pure $ case rows of { (r : _) -> Just r; [] -> Nothing }

listPeekStates :: Db -> IO [PeekState]
listPeekStates db =
  SQL.query_ db.conn
    "SELECT session_id, content, needs_input, COALESCE(reason, ''), updated_at\
    \ FROM peek_state ORDER BY session_id"

-- | Seconds since the newest updated_at in peek_state; 24h if empty/unparseable.
peekStatesAgeSeconds :: Db -> IO Double
peekStatesAgeSeconds db = do
  rows <- SQL.query_ db.conn "SELECT MAX(updated_at) FROM peek_state"
  let day = 24 * 3600
  case rows of
    (SQL.Only (Just newest) : _) | not (T.null newest) ->
      case parseTimeM True defaultTimeLocale "%Y-%m-%dT%H:%M:%S%QZ" (T.unpack newest) of
        Nothing -> pure day
        Just t -> do
          now <- getCurrentTime
          pure (realToFrac (diffUTCTime now t))
    _ -> pure day

deletePeekStatesForSessions :: Db -> [Text] -> IO ()
deletePeekStatesForSessions db sids =
  unless (null sids) $ do
    let placeholders = T.intercalate ", " (replicate (length sids) "?")
    SQL.execute db.conn
      (SQL.Query ("DELETE FROM peek_state WHERE session_id IN (" <> placeholders <> ")"))
      sids
