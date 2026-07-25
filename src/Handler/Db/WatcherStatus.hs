-- | Port of db/watcher_status.go: per-watcher health bookkeeping.
module Handler.Db.WatcherStatus
  ( WatcherStatus(..)
  , getWatcherStatus
  , recordWatcherSuccess
  , recordWatcherError
  , hasWatcherError
  ) where

import Data.Maybe (fromMaybe)
import Data.Text (Text)
import qualified Database.SQLite.Simple as SQL

import Handler.Db (Db(..))

data WatcherStatus = WatcherStatus
  { name             :: Text
  , lastSuccess      :: Text
  , lastError        :: Text
  , lastErrorMessage :: Text
  } deriving (Show, Eq)

getWatcherStatus :: Db -> Text -> IO (Maybe WatcherStatus)
getWatcherStatus db name = do
  rows <- SQL.query db.conn
    "SELECT name, last_success, last_error, last_error_message\
    \ FROM watcher_status WHERE name = ?"
    (SQL.Only name)
  pure $ case rows :: [(Text, Maybe Text, Maybe Text, Maybe Text)] of
    ((n, ls, le, lem) : _) -> Just WatcherStatus
      { name = n
      , lastSuccess = fromMaybe "" ls
      , lastError = fromMaybe "" le
      , lastErrorMessage = fromMaybe "" lem
      }
    [] -> Nothing

recordWatcherSuccess :: Db -> Text -> IO ()
recordWatcherSuccess db name =
  SQL.execute db.conn
    "INSERT INTO watcher_status (name, last_success) VALUES (?, datetime('now'))\
    \ ON CONFLICT(name) DO UPDATE SET last_success = datetime('now')"
    (SQL.Only name)

recordWatcherError :: Db -> Text -> Text -> IO ()
recordWatcherError db name message =
  SQL.execute db.conn
    "INSERT INTO watcher_status (name, last_error, last_error_message)\
    \ VALUES (?, datetime('now'), ?)\
    \ ON CONFLICT(name) DO UPDATE SET last_error = datetime('now'), last_error_message = ?"
    (name, message, message)

-- | True if the watcher's last_error is more recent than last_success.
hasWatcherError :: Db -> Text -> IO Bool
hasWatcherError db name = do
  mws <- getWatcherStatus db name
  pure $ case mws of
    Nothing -> False
    Just ws
      | ws.lastError == "" -> False
      | ws.lastSuccess == "" -> True
      | otherwise -> ws.lastError > ws.lastSuccess
