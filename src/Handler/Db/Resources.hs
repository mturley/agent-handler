-- | Port of db/resources.go: resource relationships and history.
module Handler.Db.Resources
  ( ResourceRelationship(..)
  , linkResources
  , findRelatedSessions
  , resourceHistory
  , sessionsForResource
  ) where

import Data.Text (Text)
import qualified Database.SQLite.Simple as SQL
import Database.SQLite.Simple.ToField (toField)

import Handler.Db (Db(..))
import Handler.Db.Events (Event(..))
import Handler.Db.Sessions (Session(..))
import Handler.Db.Subscriptions (Subscription(..))

-- | A hierarchical relationship between resources.
data ResourceRelationship = ResourceRelationship
  { relId        :: Text
  , childType    :: Text
  , childId      :: Text
  , childUrl     :: Maybe Text
  , parentType   :: Text
  , parentId     :: Text
  , parentUrl    :: Maybe Text
  , relationship :: Text
  , source       :: Text
  , createdAt    :: Text
  } deriving (Show, Eq)

linkResources :: Db -> ResourceRelationship -> IO ()
linkResources db r =
  SQL.execute db.conn
    "INSERT INTO resource_relationships (id, child_type, child_id, child_url, parent_type, parent_id, parent_url, relationship, source, created_at)\
    \ VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
    ( r.relId, r.childType, r.childId, r.childUrl, r.parentType
    , r.parentId, r.parentUrl, r.relationship, r.source, r.createdAt )

-- | Sessions sharing direct resource subscriptions OR subscribed to
-- resources with the same parent. Excludes the given session and archived
-- sessions; ordered by last_active DESC. (The Go version scans a reduced
-- column set; missing fields default to empty.)
-- | Wrapper for the reduced column set FindRelatedSessions selects
-- (sqlite-simple tuples stop at 10 fields).
newtype RelatedSessionRow = RelatedSessionRow Session

instance SQL.FromRow RelatedSessionRow where
  fromRow = do
    sessionId <- SQL.field
    harness <- SQL.field
    repo <- SQL.field
    branch <- SQL.field
    sessionName <- SQL.field
    pid <- SQL.field
    status <- SQL.field
    inboxMode <- SQL.field
    autoPollInterval <- SQL.field
    lastActive <- SQL.field
    registeredAt <- SQL.field
    jsonlPath <- SQL.field
    pure $ RelatedSessionRow Session
      { role = "", terminalType = "", terminalId = ""
      , cmuxWorkspaceId = "", cmuxWorkspaceName = "", cmuxWorkspaceColor = ""
      , lastPrompt = "", cwd = ""
      , ..
      }

findRelatedSessions :: Db -> Text -> IO [Session]
findRelatedSessions db sid = do
  rows <- SQL.query db.conn
    "SELECT DISTINCT\
    \  s.session_id, s.harness, s.repo, s.branch,\
    \  COALESCE(s.session_name, '') as session_name,\
    \  COALESCE(s.pid, 0) as pid,\
    \  s.status,\
    \  s.inbox_mode,\
    \  s.auto_poll_interval,\
    \  s.last_active, s.registered_at, s.jsonl_path\
    \ FROM sessions s\
    \ JOIN subscriptions sub ON s.session_id = sub.session_id AND sub.deleted_at IS NULL\
    \ WHERE s.session_id != ? AND s.status != 'archived'\
    \   AND (\
    \     (sub.resource_type, sub.resource_id) IN (\
    \       SELECT resource_type, resource_id\
    \       FROM subscriptions\
    \       WHERE session_id = ? AND deleted_at IS NULL\
    \     )\
    \     OR\
    \     EXISTS (\
    \       SELECT 1\
    \       FROM resource_relationships rr_other\
    \       JOIN resource_relationships rr_mine ON rr_mine.parent_type = rr_other.parent_type AND rr_mine.parent_id = rr_other.parent_id\
    \       JOIN subscriptions sub_mine ON sub_mine.resource_type = rr_mine.child_type AND sub_mine.resource_id = rr_mine.child_id\
    \       WHERE sub_mine.session_id = ? AND sub_mine.deleted_at IS NULL\
    \         AND rr_other.child_type = sub.resource_type AND rr_other.child_id = sub.resource_id\
    \         AND (rr_other.child_type != rr_mine.child_type OR rr_other.child_id != rr_mine.child_id)\
    \     )\
    \   )\
    \ ORDER BY s.last_active DESC"
    (sid, sid, sid)
  pure [ s | RelatedSessionRow s <- rows ]

-- | All events referencing a resource, ordered by ts DESC.
resourceHistory :: Db -> Text -> Text -> Int -> IO [Event]
resourceHistory db rType rId limit =
  SQL.query db.conn
    (SQL.Query ("SELECT DISTINCT e.id, e.ts, e.external_ts, e.source, e.session_id, e.type, e.title, e.body, e.author, e.author_type, e.broadcast, e.tags\
                \ FROM events e\
                \ JOIN event_resources er ON e.id = er.event_id\
                \ WHERE er.resource_type = ? AND er.resource_id = ?\
                \ ORDER BY e.ts DESC"
                <> (if limit > 0 then " LIMIT ?" else "")))
    ([toField rType, toField rId] ++ [toField limit | limit > 0])

-- | All subscriptions (including deleted) for a resource.
sessionsForResource :: Db -> Text -> Text -> IO [Subscription]
sessionsForResource db rType rId =
  SQL.query db.conn
    "SELECT id, session_id, resource_type, resource_id, resource_url, created_at, deleted_at\
    \ FROM subscriptions WHERE resource_type = ? AND resource_id = ?\
    \ ORDER BY created_at DESC"
    (rType, rId)
