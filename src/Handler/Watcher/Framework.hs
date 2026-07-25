-- | Port of watcher/framework.go and watcher/event_types.go: the shared
-- watcher plumbing (active resources, cursors, dedup, logging).
module Handler.Watcher.Framework
  ( WatchedResource(..)
  , eventTypeDisplayName
  , activeResources
  , eventCursor
  , isDuplicate
  , emitWatcherEvent
  , emitWatcherError
  , watcherLog
  ) where

import Control.Exception (IOException, try)
import Control.Monad (unless)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.IO as TIO
import qualified Database.SQLite.Simple as SQL
import System.Directory (createDirectoryIfMissing)
import System.FilePath ((</>))
import System.IO (hPutStrLn, stderr)

import Handler.Db (Db(..), handlerHome)
import Handler.Db.Events (Event(..), EventResource(..), insertEvent)
import Handler.Db.WatcherStatus (WatcherStatus(..), getWatcherStatus, hasWatcherError)
import Handler.Util (newUuid, nowIso)

-- | A resource that watchers monitor.
data WatchedResource = WatchedResource
  { resourceType :: Text
  , resourceId   :: Text
  , resourceUrl  :: Text
  } deriving (Show, Eq)

-- | Human-readable labels for watcher event types, falling back to the raw
-- type string. Port of EventType.DisplayName().
eventTypeDisplayName :: Text -> Text
eventTypeDisplayName = \case
  "pr_comment"               -> "PR comments"
  "pr_review_comment"        -> "review comments"
  "pr_review_requested"      -> "review requests"
  "pr_approved"              -> "approvals"
  "pr_closed"                -> "PR closed"
  "pr_merged"                -> "PR merged"
  "pr_reopened"              -> "PR reopened"
  "pr_new_commits"           -> "new commits"
  "ci_check_passed"          -> "CI passed"
  "ci_check_failed"          -> "CI failed"
  "jira_comment"             -> "Jira comments"
  "jira_status_change"       -> "status changes"
  "jira_assigned"            -> "assignments"
  "jira_description_changed" -> "description changes"
  "jira_labels_changed"      -> "label changes"
  "watch_started"            -> "watch started"
  "watcher_error"            -> "watcher errors"
  other                      -> other

-- | Resources of the given type with at least one non-deleted subscription
-- from a session with status='active'.
activeResources :: Db -> Text -> IO [WatchedResource]
activeResources db rType = do
  rows <- SQL.query db.conn
    "SELECT DISTINCT sub.resource_type, sub.resource_id, COALESCE(sub.resource_url, '')\
    \ FROM subscriptions sub\
    \ JOIN sessions s ON s.session_id = sub.session_id\
    \ WHERE sub.deleted_at IS NULL AND sub.resource_type = ? AND s.status = 'active'"
    (SQL.Only rType)
  pure [WatchedResource t i u | (t, i, u) <- rows]

-- | The maximum external_ts from events for (source, resource type, id);
-- \"\" if no events exist.
eventCursor :: Db -> Text -> Text -> Text -> IO Text
eventCursor db source rType rId = do
  rows <- SQL.query db.conn
    "SELECT MAX(e.external_ts)\
    \ FROM events e\
    \ JOIN event_resources er ON er.event_id = e.id\
    \ WHERE e.source = ? AND er.resource_type = ? AND er.resource_id = ?"
    (source, rType, rId)
  pure $ case rows of
    (SQL.Only (Just c) : _) -> c
    _                       -> ""

-- | Whether an event with this (source, resource, type, external_ts)
-- already exists.
isDuplicate :: Db -> Text -> Text -> Text -> Text -> Text -> IO Bool
isDuplicate db source rType rId eventType externalTs = do
  rows <- SQL.query db.conn
    "SELECT 1 FROM events e\
    \ JOIN event_resources er ON er.event_id = e.id\
    \ WHERE e.source = ? AND er.resource_type = ? AND er.resource_id = ? AND e.type = ? AND e.external_ts = ?\
    \ LIMIT 1"
    (source, rType, rId, eventType, externalTs)
  pure $ not (null (rows :: [SQL.Only Int]))

strMaybe :: Text -> Maybe Text
strMaybe s = if T.null s then Nothing else Just s

-- | Inserts a watcher event with event_resources.
emitWatcherEvent :: Db -> Text -> Text -> Text -> Maybe Text -> Text -> Maybe Text -> Maybe Text -> WatchedResource -> IO ()
emitWatcherEvent db source eventType title body externalTs author authorType resource = do
  eid <- newUuid
  ts <- nowIso
  insertEvent db
    Event
      { eventId = eid, ts = ts, externalTs = Just externalTs
      , source = source, sessionId = Nothing, eventType = eventType
      , title = title, body = body, author = author, authorType = authorType
      , broadcast = False, tags = Nothing
      }
    []
    [ EventResource
        { resourceType = resource.resourceType
        , resourceId = resource.resourceId
        , resourceUrl = strMaybe resource.resourceUrl
        }
    ]

-- | Inserts a watcher_error event, unless the watcher is already in error
-- state with the same message.
emitWatcherError :: Db -> Text -> Text -> Maybe Text -> WatchedResource -> IO ()
emitWatcherError db source title body resource = do
  alreadyReported <- case body of
    Nothing -> pure False
    Just b -> do
      mws <- getWatcherStatus db source
      case mws of
        Just ws | ws.lastErrorMessage == b -> hasWatcherError db source
        _ -> pure False
  unless alreadyReported $ do
    eid <- newUuid
    ts <- nowIso
    insertEvent db
      Event
        { eventId = eid, ts = ts, externalTs = Nothing
        , source = source, sessionId = Nothing, eventType = "watcher_error"
        , title = title, body = body, author = Nothing, authorType = Nothing
        , broadcast = False, tags = Nothing
        }
      []
      [ EventResource
          { resourceType = resource.resourceType
          , resourceId = resource.resourceId
          , resourceUrl = strMaybe resource.resourceUrl
          }
      ]

-- | Appends a timestamped line to ~/.agent-handler/data/logs/watcher-<name>.log,
-- falling back to stderr. Port of OpenLog + log.Logger usage.
watcherLog :: Text -> Text -> IO ()
watcherLog watcherName message = do
  home <- handlerHome
  ts <- nowIso
  let logDir = home </> "data" </> "logs"
      line = ts <> " " <> message
  result <- try $ do
    createDirectoryIfMissing True logDir
    TIO.appendFile (logDir </> ("watcher-" <> T.unpack watcherName <> ".log")) (line <> "\n")
  case result :: Either IOException () of
    Right () -> pure ()
    Left _ -> hPutStrLn stderr ("[" <> T.unpack watcherName <> "] " <> T.unpack line)
