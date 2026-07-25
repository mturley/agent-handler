-- | Port of db/resource_state.go: cached external-resource state.
module Handler.Db.ResourceState
  ( ResourceState(..)
  , ResourceStateWithSubscription(..)
  , resourceStateToJson
  , resourceStateWithSubToJson
  , upsertResourceState
  , getResourceState
  , deleteResourceState
  , listResourceStatesForSession
  ) where

import Data.Aeson (Value, object, (.=))
import Data.Maybe (catMaybes)
import Data.Text (Text)
import qualified Database.SQLite.Simple as SQL

import Handler.Db (Db(..))

data ResourceState = ResourceState
  { resourceType      :: Text
  , resourceId        :: Text
  , stateJson         :: Text
  , resourceUpdatedAt :: Text
  , watcherUpdatedAt  :: Text
  } deriving (Show, Eq)

instance SQL.FromRow ResourceState where
  fromRow = ResourceState
    <$> SQL.field <*> SQL.field <*> SQL.field <*> SQL.field <*> SQL.field

-- | A resource state paired with subscription metadata.
data ResourceStateWithSubscription = ResourceStateWithSubscription
  { resourceType      :: Text
  , resourceId        :: Text
  , resourceUrl       :: Maybe Text
  , stateJson         :: Text
  , resourceUpdatedAt :: Text
  , watcherUpdatedAt  :: Text
  } deriving (Show, Eq)

instance SQL.FromRow ResourceStateWithSubscription where
  fromRow = ResourceStateWithSubscription
    <$> SQL.field <*> SQL.field <*> SQL.field
    <*> SQL.field <*> SQL.field <*> SQL.field

resourceStateToJson :: ResourceState -> Value
resourceStateToJson r = object
  [ "resource_type" .= r.resourceType
  , "resource_id" .= r.resourceId
  , "state_json" .= r.stateJson
  , "resource_updated_at" .= r.resourceUpdatedAt
  , "watcher_updated_at" .= r.watcherUpdatedAt
  ]

resourceStateWithSubToJson :: ResourceStateWithSubscription -> Value
resourceStateWithSubToJson r = object $
  [ "resource_type" .= r.resourceType
  , "resource_id" .= r.resourceId
  , "state_json" .= r.stateJson
  , "resource_updated_at" .= r.resourceUpdatedAt
  , "watcher_updated_at" .= r.watcherUpdatedAt
  ] ++ catMaybes [ ("resource_url" .=) <$> r.resourceUrl ]

upsertResourceState :: Db -> Text -> Text -> Text -> Text -> Text -> IO ()
upsertResourceState db rType rId stateJson resourceUpdatedAt watcherUpdatedAt =
  SQL.execute db.conn
    "INSERT INTO resource_state (resource_type, resource_id, state_json, resource_updated_at, watcher_updated_at)\
    \ VALUES (?, ?, ?, ?, ?)\
    \ ON CONFLICT(resource_type, resource_id) DO UPDATE SET\
    \  state_json = excluded.state_json,\
    \  resource_updated_at = excluded.resource_updated_at,\
    \  watcher_updated_at = excluded.watcher_updated_at"
    (rType, rId, stateJson, resourceUpdatedAt, watcherUpdatedAt)

getResourceState :: Db -> Text -> Text -> IO (Maybe ResourceState)
getResourceState db rType rId = do
  rows <- SQL.query db.conn
    "SELECT resource_type, resource_id, state_json, resource_updated_at, watcher_updated_at\
    \ FROM resource_state WHERE resource_type = ? AND resource_id = ?"
    (rType, rId)
  pure $ case rows of
    (r : _) -> Just r
    []      -> Nothing

deleteResourceState :: Db -> Text -> Text -> IO ()
deleteResourceState db rType rId =
  SQL.execute db.conn
    "DELETE FROM resource_state WHERE resource_type = ? AND resource_id = ?"
    (rType, rId)

-- | Resource states for all active subscriptions of a session.
listResourceStatesForSession :: Db -> Text -> IO [ResourceStateWithSubscription]
listResourceStatesForSession db sid =
  SQL.query db.conn
    "SELECT s.resource_type, s.resource_id, s.resource_url,\
    \       COALESCE(rs.state_json, '{}'), COALESCE(rs.resource_updated_at, ''), COALESCE(rs.watcher_updated_at, '')\
    \ FROM subscriptions s\
    \ LEFT JOIN resource_state rs ON rs.resource_type = s.resource_type AND rs.resource_id = s.resource_id\
    \ WHERE s.session_id = ? AND s.deleted_at IS NULL\
    \ ORDER BY s.created_at"
    (SQL.Only sid)
