-- | Port of db/events.go: the event ledger and unread/inbox queries.
module Handler.Db.Events
  ( Event(..)
  , EventRecipient(..)
  , EventResource(..)
  , EventFilter(..)
  , emptyFilter
  , eventToJson
  , insertEvent
  , queryEvents
  , unreadForSession
  , unreadCountForSession
  , unreadResourcesForSession
  , globalUnreadForSession
  , globalUnreadCountForSession
  , humanUnreadCountForSession
  , directCountForSession
  ) where

import Control.Monad (forM_)
import Data.Aeson (Value, object, (.=))
import Data.Map.Strict (Map)
import qualified Data.Map.Strict as Map
import Data.Maybe (catMaybes)
import Data.Set (Set)
import qualified Data.Set as Set
import Data.Text (Text)
import qualified Database.SQLite.Simple as SQL
import Database.SQLite.Simple.ToField (toField)

import Handler.Db (Db(..))
import Handler.Db.Cursors (getCursor, getHumanCursor)
import Handler.Db.Sessions (Session(..), getSession)
import Handler.Util (epochIso)

-- | Event types excluded from non-handler inbox queries: bookkeeping events
-- that shouldn't appear as unread in session inboxes.
inboxExcludedTypesSql :: Text
inboxExcludedTypesSql = "AND e.type NOT IN ('watch_started', 'watcher_error')"

data Event = Event
  { eventId    :: Text
  , ts         :: Text
  , externalTs :: Maybe Text
  , source     :: Text
  , sessionId  :: Maybe Text
  , eventType  :: Text
  , title      :: Text
  , body       :: Maybe Text
  , author     :: Maybe Text
  , authorType :: Maybe Text
  , broadcast  :: Bool
  , tags       :: Maybe Text
  } deriving (Show, Eq)

instance SQL.FromRow Event where
  fromRow = do
    eventId    <- SQL.field
    ts         <- SQL.field
    externalTs <- SQL.field
    source     <- SQL.field
    sessionId  <- SQL.field
    eventType  <- SQL.field
    title      <- SQL.field
    body       <- SQL.field
    author     <- SQL.field
    authorType <- SQL.field
    broadcastInt <- SQL.field
    tags       <- SQL.field
    pure Event { broadcast = (broadcastInt :: Int) == 1, .. }

-- | JSON rendering matching the Go struct tags (omitempty on the Maybes).
eventToJson :: Event -> Value
eventToJson e = object $
  [ "id" .= e.eventId
  , "ts" .= e.ts
  , "source" .= e.source
  , "type" .= e.eventType
  , "title" .= e.title
  , "broadcast" .= e.broadcast
  ] ++ catMaybes
  [ ("external_ts" .=) <$> e.externalTs
  , ("session_id" .=) <$> e.sessionId
  , ("body" .=) <$> e.body
  , ("author" .=) <$> e.author
  , ("author_type" .=) <$> e.authorType
  , ("tags" .=) <$> e.tags
  ]

data EventRecipient = EventRecipient
  { recipientType  :: Text
  , recipientValue :: Text
  } deriving (Show, Eq)

data EventResource = EventResource
  { resourceType :: Text
  , resourceId   :: Text
  , resourceUrl  :: Maybe Text
  } deriving (Show, Eq)

-- | Criteria for querying events.
data EventFilter = EventFilter
  { filterSessionId :: Maybe Text
  , filterSource    :: Maybe Text
  , filterType      :: Maybe Text
  , filterSince     :: Maybe Text
  , filterLimit     :: Int
  , filterOffset    :: Int
  } deriving (Show, Eq)

emptyFilter :: EventFilter
emptyFilter = EventFilter Nothing Nothing Nothing Nothing 0 0

eventColumns :: Text
eventColumns = "id, ts, external_ts, source, session_id, type, title, body, author, author_type, broadcast, tags"

eventColumnsE :: Text
eventColumnsE = "e.id, e.ts, e.external_ts, e.source, e.session_id, e.type, e.title, e.body, e.author, e.author_type, e.broadcast, e.tags"

-- | Inserts an event with its recipients and resources in one transaction.
insertEvent :: Db -> Event -> [EventRecipient] -> [EventResource] -> IO ()
insertEvent db e recipients resources =
  SQL.withTransaction db.conn $ do
    SQL.execute db.conn
      (SQL.Query ("INSERT INTO events (" <> eventColumns <> ") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"))
      [ toField e.eventId, toField e.ts, toField e.externalTs, toField e.source
      , toField e.sessionId, toField e.eventType, toField e.title, toField e.body
      , toField e.author, toField e.authorType
      , toField (if e.broadcast then 1 else 0 :: Int), toField e.tags
      ]
    forM_ recipients $ \r ->
      SQL.execute db.conn
        "INSERT INTO event_recipients (event_id, recipient_type, recipient_value) VALUES (?, ?, ?)"
        (e.eventId, r.recipientType, r.recipientValue)
    forM_ resources $ \r ->
      SQL.execute db.conn
        "INSERT INTO event_resources (event_id, resource_type, resource_id, resource_url) VALUES (?, ?, ?, ?)"
        (e.eventId, r.resourceType, r.resourceId, r.resourceUrl)

-- | Events matching the filter, ordered by ts DESC.
queryEvents :: Db -> EventFilter -> IO [Event]
queryEvents db f = do
  let clauses = catMaybes
        [ (\v -> (" AND session_id = ?", toField v)) <$> f.filterSessionId
        , (\v -> (" AND source = ?", toField v)) <$> f.filterSource
        , (\v -> (" AND type = ?", toField v)) <$> f.filterType
        , (\v -> (" AND ts > ?", toField v)) <$> f.filterSince
        ]
      limits = concat
        [ [(" LIMIT ?", toField f.filterLimit) | f.filterLimit > 0]
        , [(" OFFSET ?", toField f.filterOffset) | f.filterOffset > 0]
        ]
      sql = "SELECT " <> eventColumns <> " FROM events WHERE 1=1"
            <> mconcat (map fst clauses)
            <> " ORDER BY ts DESC"
            <> mconcat (map fst limits)
  SQL.query db.conn (SQL.Query sql) (map snd (clauses ++ limits))

-- | The shared routing predicate: broadcast, direct recipient (session,
-- branch, repo:branch, role), or subscribed resource.
unreadJoinsSql :: Text
unreadJoinsSql =
  " FROM events e\
  \ LEFT JOIN event_recipients er ON e.id = er.event_id\
  \ LEFT JOIN event_resources eres ON e.id = eres.event_id\
  \ LEFT JOIN subscriptions s ON s.resource_type = eres.resource_type AND s.resource_id = eres.resource_id AND s.session_id = ? AND s.deleted_at IS NULL\
  \ WHERE e.ts > ? "

unreadPredicateSql :: Text
unreadPredicateSql =
  " AND (\
  \   e.broadcast = 1\
  \   OR (er.recipient_type = 'session' AND er.recipient_value = ?)\
  \   OR (er.recipient_type = 'branch' AND (er.recipient_value = ? OR er.recipient_value = ?))\
  \   OR (er.recipient_type = 'role' AND er.recipient_value = ?)\
  \   OR s.id IS NOT NULL\
  \ )"

-- | Looks up cursor + session, then runs an inbox-routed query.
withSessionRouting :: Db -> Text -> (Text -> Session -> [SQL.SQLData] -> IO a) -> IO a
withSessionRouting db sid k = do
  cursor <- getCursor db sid
  msession <- getSession db sid
  case msession of
    Nothing -> fail ("session \"" <> show sid <> "\" not found")
    Just session -> do
      let repoBranch = session.repo <> ":" <> session.branch
      k cursor session
        [ toField sid, toField cursor, toField sid
        , toField session.branch, toField repoBranch, toField session.role
        ]

-- | Unread events for a session, ordered by ts ASC. An event is unread if
-- ts > cursor AND (broadcast OR recipient matches OR subscribed resource).
unreadForSession :: Db -> Text -> IO [Event]
unreadForSession db sid =
  withSessionRouting db sid $ \_ _ args ->
    SQL.query db.conn
      (SQL.Query ("SELECT DISTINCT " <> eventColumnsE <> unreadJoinsSql
                  <> inboxExcludedTypesSql <> unreadPredicateSql
                  <> " ORDER BY e.ts ASC"))
      args

-- | Total unread count and a breakdown by type.
unreadCountForSession :: Db -> Text -> IO (Int, Map Text Int)
unreadCountForSession db sid =
  withSessionRouting db sid $ \_ _ args -> do
    rows <- SQL.query db.conn
      (SQL.Query ("SELECT e.type, COUNT(DISTINCT e.id) as count" <> unreadJoinsSql
                  <> inboxExcludedTypesSql <> unreadPredicateSql
                  <> " GROUP BY e.type"))
      args
    let breakdown = Map.fromList rows
    pure (sum (Map.elems breakdown), breakdown)

-- | The set of \"type:id\" strings for resources that have unread events.
unreadResourcesForSession :: Db -> Text -> IO (Set Text)
unreadResourcesForSession db sid =
  withSessionRouting db sid $ \_ _ args -> do
    rows <- SQL.query db.conn
      (SQL.Query ("SELECT DISTINCT eres.resource_type, eres.resource_id\
                  \ FROM events e\
                  \ LEFT JOIN event_recipients er ON e.id = er.event_id\
                  \ JOIN event_resources eres ON e.id = eres.event_id\
                  \ LEFT JOIN subscriptions s ON s.resource_type = eres.resource_type AND s.resource_id = eres.resource_id AND s.session_id = ? AND s.deleted_at IS NULL\
                  \ WHERE e.ts > ? "
                  <> inboxExcludedTypesSql <> unreadPredicateSql))
      args
    pure $ Set.fromList [rt <> ":" <> rid | (rt, rid) <- rows]

-- | ALL events since the session's cursor, regardless of targeting.
globalUnreadForSession :: Db -> Text -> IO [Event]
globalUnreadForSession db sid = do
  cursor <- getCursor db sid
  SQL.query db.conn
    (SQL.Query ("SELECT DISTINCT " <> eventColumnsE
                <> " FROM events e WHERE e.ts > ? ORDER BY e.ts ASC"))
    (SQL.Only cursor)

-- | Total count and breakdown by type of ALL events since the cursor.
globalUnreadCountForSession :: Db -> Text -> IO (Int, Map Text Int)
globalUnreadCountForSession db sid = do
  cursor <- getCursor db sid
  rows <- SQL.query db.conn
    "SELECT e.type, COUNT(*) as count FROM events e WHERE e.ts > ? GROUP BY e.type"
    (SQL.Only cursor)
  let breakdown = Map.fromList rows
  pure (sum (Map.elems breakdown), breakdown)

-- | Count of events unread by the human (human_seen_ts cursor): counts
-- auto-delivered events as unread until the user has actually seen them.
humanUnreadCountForSession :: Db -> Text -> IO Int
humanUnreadCountForSession db sid = do
  cursor <- getHumanCursor db sid
  msession <- getSession db sid
  case msession of
    Nothing -> pure 0
    Just session -> do
      let repoBranch = session.repo <> ":" <> session.branch
      counts <- SQL.query db.conn
        (SQL.Query ("SELECT COUNT(DISTINCT e.id)" <> unreadJoinsSql
                    <> inboxExcludedTypesSql <> unreadPredicateSql))
        [ toField sid, toField cursor, toField sid
        , toField session.branch, toField repoBranch, toField session.role
        ]
      pure $ case counts of
        (SQL.Only n : _) -> n
        []               -> 0

-- | Count of unread events directly addressed to a session
-- (via event_recipients, not subscription routing).
directCountForSession :: Db -> Text -> IO Int
directCountForSession db sid = do
  cursor0 <- getCursor db sid
  let cursor = if cursor0 == "" then epochIso else cursor0
  msession <- getSession db sid
  case msession of
    Nothing -> pure 0
    Just session -> do
      let repoBranch = session.repo <> ":" <> session.branch
      counts <- SQL.query db.conn
        "SELECT COUNT(DISTINCT e.id) FROM events e\
        \ JOIN event_recipients er ON er.event_id = e.id\
        \ WHERE e.ts > ?\
        \   AND (\
        \     (er.recipient_type = 'session' AND er.recipient_value = ?)\
        \     OR (er.recipient_type = 'branch' AND (er.recipient_value = ? OR er.recipient_value = ?))\
        \     OR (er.recipient_type = 'role' AND er.recipient_value = ?)\
        \   )"
        (cursor, sid, session.branch, repoBranch, session.role)
      pure $ case counts of
        (SQL.Only n : _) -> n
        []               -> 0
