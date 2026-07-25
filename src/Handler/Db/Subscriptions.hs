-- | Port of db/subscriptions.go: soft-deleted resource subscriptions.
module Handler.Db.Subscriptions
  ( Subscription(..)
  , subscriptionToJson
  , subscribe
  , subscribeIfNew
  , unsubscribe
  , reinstate
  , listSubscriptions
  , softDeleteSubscriptionsForBranch
  , softDeleteSubscriptionsForSession
  , restoreSubscriptionsForSession
  ) where

import Control.Monad (when)
import Data.Aeson (Value, object, (.=))
import Data.Maybe (catMaybes)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Database.SQLite.Simple as SQL

import Handler.Db (Db(..))
import Handler.Db.ResourceState (deleteResourceState)

-- | A session's subscription to a resource.
data Subscription = Subscription
  { subId        :: Text
  , sessionId    :: Text
  , resourceType :: Text
  , resourceId   :: Text
  , resourceUrl  :: Maybe Text
  , createdAt    :: Text
  , deletedAt    :: Maybe Text
  } deriving (Show, Eq)

instance SQL.FromRow Subscription where
  fromRow = Subscription
    <$> SQL.field <*> SQL.field <*> SQL.field <*> SQL.field
    <*> SQL.field <*> SQL.field <*> SQL.field

subscriptionToJson :: Subscription -> Value
subscriptionToJson s = object $
  [ "id" .= s.subId
  , "session_id" .= s.sessionId
  , "resource_type" .= s.resourceType
  , "resource_id" .= s.resourceId
  , "created_at" .= s.createdAt
  ] ++ catMaybes
  [ ("resource_url" .=) <$> s.resourceUrl
  , ("deleted_at" .=) <$> s.deletedAt
  ]

-- | Subscribes a session to a resource. Active subscription → no-op (dedup);
-- soft-deleted → reinstated; otherwise inserts new.
subscribe :: Db -> Subscription -> IO ()
subscribe db s = do
  existing <- SQL.query db.conn
    "SELECT id, deleted_at FROM subscriptions\
    \ WHERE session_id = ? AND resource_type = ? AND resource_id = ?"
    (s.sessionId, s.resourceType, s.resourceId)
  case existing :: [(Text, Maybe Text)] of
    ((_, Nothing) : _) -> pure ()
    ((_, Just _) : _)  -> reinstate db s.sessionId s.resourceType s.resourceId
    [] ->
      SQL.execute db.conn
        "INSERT INTO subscriptions (id, session_id, resource_type, resource_id, resource_url, created_at, deleted_at)\
        \ VALUES (?, ?, ?, ?, ?, ?, NULL)"
        (s.subId, s.sessionId, s.resourceType, s.resourceId, s.resourceUrl, s.createdAt)

-- | Creates a subscription only if none exists (active or soft-deleted).
-- Does NOT reinstate soft-deleted rows — used by auto-registration from
-- .worktree-resources to avoid resurrecting watcher-closed subscriptions.
subscribeIfNew :: Db -> Subscription -> IO ()
subscribeIfNew db s = do
  counts <- SQL.query db.conn
    "SELECT COUNT(*) FROM subscriptions\
    \ WHERE session_id = ? AND resource_type = ? AND resource_id = ?"
    (s.sessionId, s.resourceType, s.resourceId)
  let count = case counts of { (SQL.Only n : _) -> n; _ -> 0 :: Int }
  when (count == 0) $
    SQL.execute db.conn
      "INSERT INTO subscriptions (id, session_id, resource_type, resource_id, resource_url, created_at, deleted_at)\
      \ VALUES (?, ?, ?, ?, ?, ?, NULL)"
      (s.subId, s.sessionId, s.resourceType, s.resourceId, s.resourceUrl, s.createdAt)

-- | Soft-deletes a subscription; errors if none is active. If it was the
-- last active subscription for the resource, deletes the resource_state row.
unsubscribe :: Db -> Text -> Text -> Text -> IO ()
unsubscribe db sid rType rId = do
  SQL.execute db.conn
    "UPDATE subscriptions SET deleted_at = datetime('now')\
    \ WHERE session_id = ? AND resource_type = ? AND resource_id = ? AND deleted_at IS NULL"
    (sid, rType, rId)
  n <- SQL.changes db.conn
  when (n == 0) $
    fail ("no active subscription found for session " <> show sid
          <> ", resource " <> T.unpack rType <> "/" <> T.unpack rId)
  remainings <- SQL.query db.conn
    "SELECT COUNT(*) FROM subscriptions\
    \ WHERE resource_type = ? AND resource_id = ? AND deleted_at IS NULL"
    (rType, rId)
  let remaining = case remainings of { (SQL.Only c : _) -> c; _ -> 0 :: Int }
  when (remaining == 0) $ deleteResourceState db rType rId

-- | Clears deleted_at for a soft-deleted subscription; errors if none found.
reinstate :: Db -> Text -> Text -> Text -> IO ()
reinstate db sid rType rId = do
  SQL.execute db.conn
    "UPDATE subscriptions SET deleted_at = NULL\
    \ WHERE session_id = ? AND resource_type = ? AND resource_id = ? AND deleted_at IS NOT NULL"
    (sid, rType, rId)
  n <- SQL.changes db.conn
  when (n == 0) $
    fail ("no soft-deleted subscription found for session " <> show sid
          <> ", resource " <> T.unpack rType <> "/" <> T.unpack rId)

-- | Subscriptions for a session, optionally including soft-deleted ones.
listSubscriptions :: Db -> Text -> Bool -> IO [Subscription]
listSubscriptions db sid includeDeleted =
  SQL.query db.conn
    (SQL.Query ("SELECT id, session_id, resource_type, resource_id, resource_url, created_at, deleted_at\
                \ FROM subscriptions WHERE session_id = ?"
                <> (if includeDeleted then "" else " AND deleted_at IS NULL")
                <> " ORDER BY created_at DESC"))
    (SQL.Only sid)

-- | Soft-deletes all active subscriptions for sessions on a branch.
softDeleteSubscriptionsForBranch :: Db -> Text -> IO Int
softDeleteSubscriptionsForBranch db branch = do
  SQL.execute db.conn
    "UPDATE subscriptions SET deleted_at = datetime('now')\
    \ WHERE session_id IN (SELECT session_id FROM sessions WHERE branch = ?)\
    \   AND deleted_at IS NULL"
    (SQL.Only branch)
  SQL.changes db.conn

-- | Soft-deletes all active subscriptions for a session.
softDeleteSubscriptionsForSession :: Db -> Text -> IO Int
softDeleteSubscriptionsForSession db sid = do
  SQL.execute db.conn
    "UPDATE subscriptions SET deleted_at = datetime('now')\
    \ WHERE session_id = ? AND deleted_at IS NULL"
    (SQL.Only sid)
  SQL.changes db.conn

-- | Un-soft-deletes all subscriptions for a session.
restoreSubscriptionsForSession :: Db -> Text -> IO Int
restoreSubscriptionsForSession db sid = do
  SQL.execute db.conn
    "UPDATE subscriptions SET deleted_at = NULL\
    \ WHERE session_id = ? AND deleted_at IS NOT NULL"
    (SQL.Only sid)
  SQL.changes db.conn
