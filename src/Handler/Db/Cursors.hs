-- | Port of db/cursors.go: agent and human read cursors per session.
module Handler.Db.Cursors
  ( getCursor
  , getHumanCursor
  , advanceCursor
  , advanceBothCursors
  , catchUpHumanCursor
  , clearHumanCursor
  , autoDeliveredCount
  , autoDeliveredCountAll
  ) where

import Data.Text (Text)
import qualified Database.SQLite.Simple as SQL

import Handler.Db (Db(..))
import Handler.Db.Sessions (Session(..), getSession)

-- | The last_seen_ts for a session; \"\" (not an error) if no cursor exists.
getCursor :: Db -> Text -> IO Text
getCursor db sid = do
  rows <- SQL.query db.conn
    "SELECT last_seen_ts FROM session_cursors WHERE session_id = ?" (SQL.Only sid)
  pure $ case rows of
    (SQL.Only ts : _) -> ts
    []                -> ""

-- | The human_seen_ts for a session, falling back to last_seen_ts when NULL.
getHumanCursor :: Db -> Text -> IO Text
getHumanCursor db sid = do
  rows <- SQL.query db.conn
    "SELECT COALESCE(human_seen_ts, last_seen_ts) FROM session_cursors WHERE session_id = ?"
    (SQL.Only sid)
  pure $ case rows of
    (SQL.Only ts : _) -> ts
    []                -> ""

-- | Inserts or updates the agent cursor for the given session.
advanceCursor :: Db -> Text -> Text -> IO ()
advanceCursor db sid ts =
  SQL.execute db.conn
    "INSERT INTO session_cursors (session_id, last_seen_ts) VALUES (?, ?)\
    \ ON CONFLICT(session_id) DO UPDATE SET last_seen_ts = excluded.last_seen_ts"
    (sid, ts)

-- | Advances both the agent and human cursors together (manual /inbox, ack).
advanceBothCursors :: Db -> Text -> Text -> IO ()
advanceBothCursors db sid ts =
  SQL.execute db.conn
    "INSERT INTO session_cursors (session_id, last_seen_ts, human_seen_ts) VALUES (?, ?, ?)\
    \ ON CONFLICT(session_id) DO UPDATE SET last_seen_ts = excluded.last_seen_ts, human_seen_ts = excluded.human_seen_ts"
    (sid, ts, ts)

-- | Sets human_seen_ts to match last_seen_ts (user sent a prompt).
catchUpHumanCursor :: Db -> Text -> IO ()
catchUpHumanCursor db sid =
  SQL.execute db.conn
    "UPDATE session_cursors SET human_seen_ts = last_seen_ts WHERE session_id = ?"
    (SQL.Only sid)

-- | Sets human_seen_ts to NULL (used when leaving auto mode).
clearHumanCursor :: Db -> Text -> IO ()
clearHumanCursor db sid =
  SQL.execute db.conn
    "UPDATE session_cursors SET human_seen_ts = NULL WHERE session_id = ?"
    (SQL.Only sid)

-- | Number of events between the human cursor and agent cursor that match
-- the session's subscription/broadcast rules. 0 if cursors are equal or
-- the human cursor is NULL.
autoDeliveredCount :: Db -> Text -> IO Int
autoDeliveredCount db sid = do
  rows <- SQL.query db.conn
    "SELECT last_seen_ts, human_seen_ts FROM session_cursors WHERE session_id = ?"
    (SQL.Only sid)
  case rows of
    [] -> pure 0
    ((agentCursor, humanCursor) : _) ->
      case humanCursor :: Maybe Text of
        Nothing -> pure 0
        Just hc
          | hc == agentCursor -> pure 0
          | otherwise -> do
              msession <- getSession db sid
              case msession of
                Nothing -> pure 0
                Just session -> do
                  let repoBranch = session.repo <> ":" <> session.branch
                  counts <- SQL.query db.conn
                    "SELECT COUNT(DISTINCT e.id)\
                    \ FROM events e\
                    \ LEFT JOIN event_recipients er ON e.id = er.event_id\
                    \ LEFT JOIN event_resources eres ON e.id = eres.event_id\
                    \ LEFT JOIN subscriptions s ON s.resource_type = eres.resource_type AND s.resource_id = eres.resource_id AND s.session_id = ? AND s.deleted_at IS NULL\
                    \ WHERE e.ts > ? AND e.ts <= ?\
                    \   AND (\
                    \     e.broadcast = 1\
                    \     OR (er.recipient_type = 'session' AND er.recipient_value = ?)\
                    \     OR (er.recipient_type = 'branch' AND (er.recipient_value = ? OR er.recipient_value = ?))\
                    \     OR (er.recipient_type = 'role' AND er.recipient_value = ?)\
                    \     OR s.id IS NOT NULL\
                    \   )"
                    (sid, hc, agentCursor, sid, session.branch, repoBranch, session.role)
                  pure $ case counts of
                    (SQL.Only n : _) -> n
                    []               -> 0

-- | Number of all events between the human cursor and agent cursor,
-- regardless of routing rules (handler sessions see everything).
autoDeliveredCountAll :: Db -> Text -> IO Int
autoDeliveredCountAll db sid = do
  rows <- SQL.query db.conn
    "SELECT last_seen_ts, human_seen_ts FROM session_cursors WHERE session_id = ?"
    (SQL.Only sid)
  case rows of
    [] -> pure 0
    ((agentCursor, humanCursor) : _) ->
      case humanCursor :: Maybe Text of
        Nothing -> pure 0
        Just hc
          | hc == (agentCursor :: Text) -> pure 0
          | otherwise -> do
              counts <- SQL.query db.conn
                "SELECT COUNT(*) FROM events WHERE ts > ? AND ts <= ?"
                (hc, agentCursor)
              pure $ case counts of
                (SQL.Only n : _) -> n
                []               -> 0
