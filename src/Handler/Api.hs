{-# LANGUAGE ScopedTypeVariables #-}
-- | Port of cmd/api/: the web UI's HTTP API server (scotty on warp),
-- including the SSE stream and static/SPA serving of the embedded UI.
module Handler.Api
  ( ApiConfig(..)
  , serveApi
  ) where

import Control.Concurrent (threadDelay)
import Control.Concurrent.Async (mapConcurrently)
import Control.Exception (SomeException, try)
import Control.Monad (unless)
import Control.Monad.IO.Class (liftIO)
import Data.Aeson (Value(..), object, (.=))
import qualified Data.Aeson as A
import qualified Data.Aeson.Key as Key
import qualified Data.Aeson.KeyMap as KM
import qualified Data.ByteString.Builder as B
import qualified Data.ByteString.Lazy as BL
import Data.Char (isHexDigit)
import Data.List (foldl', isInfixOf)
import Data.Map.Strict (Map)
import qualified Data.Map.Strict as Map
import Data.Maybe (catMaybes, fromMaybe)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.Encoding as TE
import qualified Data.Text.Lazy as TL
import Data.Time.Clock (UTCTime, diffUTCTime, getCurrentTime)
import Data.Time.Format (defaultTimeLocale, formatTime, parseTimeM)
import Data.Time.LocalTime (getZonedTime)
import qualified Database.SQLite.Simple as SQL
import Database.SQLite.Simple.ToField (toField)
import Network.HTTP.Types.Status (Status, status400, status404, status500)
import Network.Wai (rawPathInfo)
import Network.Wai.Handler.Warp (defaultSettings, setPort)
import System.Directory (doesFileExist, getHomeDirectory)
import System.Exit (ExitCode(..))
import System.FilePath ((</>))
import qualified System.Info
import System.IO (hPutStrLn, stderr)
import System.Process (readProcessWithExitCode)
import Web.Scotty

import Handler.Config (Config, emptyConfig, isServiceConfigured, readConfig, configDefaultPath)
import Handler.Db (Db(..))
import qualified Handler.Db as Db
import Handler.Db.Cursors (advanceBothCursors)
import Handler.Db.Events (eventToJson, unreadForSession, unreadCountForSession)
import Handler.Db.Peek (PeekState(..), getPeekState, peekStateToJson)
import Handler.Db.ResourceState (ResourceState(..), getResourceState)
import Handler.Db.Sessions (Session(..), archiveSessions, getSession, listArchivedSessions, listSessions)
import Handler.Db.Subscriptions (Subscription(..), listSubscriptions)
import Handler.Db.WatcherStatus (WatcherStatus(..), getWatcherStatus, hasWatcherError)
import Handler.Discover (isSessionProcess)
import Handler.Util (nowIso)
import Handler.WebEmbed (contentTypeFor, lookupWebFile, webIndexHtml)

-- | Mirror of the Go api.Server fields (the web FS is the compiled-in embed).
data ApiConfig = ApiConfig
  { db            :: Db
  , cmuxAvailable :: Bool
  , devMode       :: Bool
  , port          :: Int
  }

-- | Timestamped stderr log line, like Go's log.New(os.Stderr, "[handler-ui] ", LstdFlags).
logLine :: Text -> IO ()
logLine msg = do
  now <- getZonedTime
  hPutStrLn stderr $
    "[handler-ui] " <> formatTime defaultTimeLocale "%Y/%m/%d %H:%M:%S" now
    <> " " <> T.unpack msg

-- | Starts the API server; blocks forever.
serveApi :: ApiConfig -> IO ()
serveApi cfg = do
  logLine ("Listening on http://localhost:" <> T.pack (show cfg.port))
  scottyOpts (Options { verbose = 0, settings = setPort cfg.port defaultSettings }) $ do
    get "/api/capabilities" $ json (object ["cmux" .= cfg.cmuxAvailable])
    get "/api/sessions" (handleSessions cfg)
    get "/api/sessions/archived" (handleArchivedSessions cfg)
    get "/api/sessions/:id" (handleSession cfg)
    get "/api/sessions/:id/peek" (handleSessionPeek cfg)
    get "/api/sessions/:id/inbox" (handleSessionInbox cfg)
    get "/api/resources" (handleResources cfg)
    get "/api/events" (handleEvents cfg)
    get "/api/stream" (handleStream cfg)
    post "/api/actions/switch" handleSwitch
    post "/api/actions/peek" handleForcePeek
    post "/api/actions/dismiss-inbox" handleDismissInbox
    post "/api/actions/archive-sessions" handleArchiveSessions
    -- Static files with SPA fallback (skip in dev mode — Vite serves them)
    unless cfg.devMode $ do
      get "/" serveSpaIndex
      notFound serveStatic

writeErr :: Status -> Text -> ActionM ()
writeErr s msg = do
  status s
  json (object ["error" .= msg])

-- | Parses the JSON request body into an Object, or replies 400.
withJsonBody :: (A.Object -> ActionM ()) -> ActionM ()
withJsonBody k = do
  raw <- body
  case A.decode raw of
    Just (A.Object o) -> k o
    _ -> writeErr status400 "Invalid JSON"

strField :: A.Object -> Text -> Maybe Text
strField o k = case KM.lookup (Key.fromText k) o of
  Just (A.String s) -> Just s
  _ -> Nothing

------------------------------------------------------------------------
-- Sessions

-- | GET /api/sessions — all non-archived sessions, enriched, with cmux order.
handleSessions :: ApiConfig -> ActionM ()
handleSessions cfg = do
  result <- liftIO $ try $ do
    sessions <- listSessions cfg.db False 1000 0
    cmuxOrder <- buildCmuxOrderMap
    mapConcurrently
      (\s -> enrichSession cfg.db (Map.findWithDefault 999999 s.terminalId cmuxOrder) s)
      sessions
  case result :: Either SomeException [Value] of
    Left e -> do
      liftIO $ logLine ("Error listing sessions: " <> T.pack (show e))
      writeErr status500 "Failed to list sessions"
    Right enriched -> json enriched

-- | GET /api/sessions/archived?limit=&offset=&search=&sort=
handleArchivedSessions :: ApiConfig -> ActionM ()
handleArchivedSessions cfg = do
  limitQ <- queryParamMaybe "limit" :: ActionM (Maybe Int)
  offsetQ <- queryParamMaybe "offset" :: ActionM (Maybe Int)
  search <- fromMaybe "" <$> (queryParamMaybe "search" :: ActionM (Maybe Text))
  sort <- fromMaybe "" <$> (queryParamMaybe "sort" :: ActionM (Maybe Text))
  let limit = case limitQ of
        Just n | n > 0 && n <= 200 -> n
        _ -> 50
      offset = case offsetQ of
        Just n | n >= (0 :: Int) -> n
        _ -> 0
  result <- liftIO $ try $ do
    (sessions, total) <- listArchivedSessions cfg.db search sort limit offset
    enriched <- mapM (enrichSession cfg.db 0) sessions
    pure (enriched, total, offset + length sessions < total)
  case result :: Either SomeException ([Value], Int, Bool) of
    Left e -> do
      liftIO $ logLine ("Error listing archived sessions: " <> T.pack (show e))
      writeErr status500 "Failed to list archived sessions"
    Right (enriched, total, hasMore) ->
      json (object ["sessions" .= enriched, "total" .= total, "has_more" .= hasMore])

-- | GET /api/sessions/:id
handleSession :: ApiConfig -> ActionM ()
handleSession cfg = do
  sessionId <- captureParam "id"
  if T.null sessionId
    then writeErr status400 "session_id is required"
    else do
      msession <- liftIO $ getSession cfg.db sessionId
      case msession of
        Nothing -> writeErr status404 "Session not found"
        Just session -> do
          enriched <- liftIO $ enrichSession cfg.db 0 session
          json enriched

-- | GET /api/sessions/:id/peek
handleSessionPeek :: ApiConfig -> ActionM ()
handleSessionPeek cfg = do
  sessionId <- captureParam "id"
  if T.null sessionId
    then writeErr status400 "session_id is required"
    else do
      result <- liftIO (try (getPeekState cfg.db sessionId))
      case result :: Either SomeException (Maybe PeekState) of
        Left e -> do
          liftIO $ logLine ("Error getting peek state for " <> sessionId <> ": " <> T.pack (show e))
          writeErr status500 "Failed to get peek state"
        Right Nothing -> json $ object
          [ "content" .= ("" :: Text)
          , "needs_input" .= False
          , "reason" .= ("" :: Text)
          , "updated_at" .= ("" :: Text)
          ]
        Right (Just ps) -> json (peekStateToJson ps)

-- | GET /api/sessions/:id/inbox
handleSessionInbox :: ApiConfig -> ActionM ()
handleSessionInbox cfg = do
  sessionId <- captureParam "id"
  if T.null sessionId
    then writeErr status400 "session_id is required"
    else do
      result <- liftIO (try (unreadForSession cfg.db sessionId))
      case result of
        Left (e :: SomeException) -> do
          liftIO $ logLine ("Error getting inbox for " <> sessionId <> ": " <> T.pack (show e))
          writeErr status500 "Failed to get inbox"
        Right events -> json (map eventToJson events)

-- | Whether an RFC3339 timestamp is within the last 24 hours.
withinDay :: Text -> IO Bool
withinDay ts
  | T.null ts = pure False
  | otherwise = case parseRfc3339 ts of
      Nothing -> pure False
      Just t -> do
        now <- getCurrentTime
        pure (diffUTCTime now t < 24 * 3600)

parseRfc3339 :: Text -> Maybe UTCTime
parseRfc3339 ts =
  let s = T.unpack ts
  in parseTimeM True defaultTimeLocale "%Y-%m-%dT%H:%M:%S%QZ" s
     <|> parseTimeM True defaultTimeLocale "%Y-%m-%dT%H:%M:%S%Q%Ez" s
  where (<|>) a b = maybe b Just a

-- | Computes the derived fields for a session (port of enrichSession).
-- cmux_order is caller-supplied: the list endpoint passes the cmux ordinal
-- (or 999999); other endpoints pass the Go zero value 0.
enrichSession :: Db -> Int -> Session -> IO Value
enrichSession db cmuxOrder session = do
  displayState <-
    if session.status == "archived"
      then pure ("archived" :: Text)
      else do
        alive <- isSessionProcess session.pid session.sessionId
        if not alive
          then pure "dead"
          else do
            recent <- withinDay session.lastPrompt
            pure (if recent then "active" else "idle")

  (unreadCount, breakdown) <-
    if displayState == "active" || displayState == "idle"
      then either (const (0, Map.empty)) id
             <$> (try (unreadCountForSession db session.sessionId)
                    :: IO (Either SomeException (Int, Map Text Int)))
      else pure (0, Map.empty)

  needsInput <- maybe False (.needsInput)
    . either (const Nothing) id
    <$> (try (getPeekState db session.sessionId)
           :: IO (Either SomeException (Maybe PeekState)))

  subs <- either (const []) id
    <$> (try (listSubscriptions db session.sessionId False)
           :: IO (Either SomeException [Subscription]))
  let subCount = length subs
      subBreakdown = Map.fromListWith (+) [(s.resourceType, 1 :: Int) | s <- subs]

  pure $ object $
    [ "session_id" .= session.sessionId
    , "session_name" .= session.sessionName
    , "branch" .= session.branch
    , "repo" .= session.repo
    , "display_state" .= displayState
    , "inbox_mode" .= session.inboxMode
    , "peekable" .= (session.terminalType /= "")
    , "unread_count" .= unreadCount
    , "last_active" .= session.lastActive
    , "needs_input" .= needsInput
    , "pid" .= session.pid
    , "status" .= session.status
    , "subscriptions_count" .= subCount
    , "cmux_order" .= cmuxOrder
    ] ++ catMaybes
    [ nonEmpty "terminal_type" session.terminalType
    , if Map.null breakdown then Nothing else Just ("unread_breakdown" .= breakdown)
    , nonEmpty "last_prompt" session.lastPrompt
    , nonEmpty "cmux_workspace" session.cmuxWorkspaceName
    , nonEmpty "cmux_workspace_color" session.cmuxWorkspaceColor
    , if Map.null subBreakdown then Nothing else Just ("subscriptions_breakdown" .= subBreakdown)
    , nonEmpty "cwd" session.cwd
    ]
  where
    nonEmpty k v = if T.null v then Nothing else Just (Key.fromText k .= v)

-- | Queries cmux for workspace/surface ordering: surface UUID → ordinal.
buildCmuxOrderMap :: IO (Map Text Int)
buildCmuxOrderMap = do
  wsResult <- try (readProcessWithExitCode "cmux" ["workspace", "list", "--json"] "")
  case wsResult :: Either SomeException (ExitCode, String, String) of
    Right (ExitSuccess, wsOut, _) ->
      case A.decode (BL.fromStrict (TE.encodeUtf8 (T.pack wsOut))) of
        Just (A.Object o)
          | Just (A.Array wss) <- KM.lookup "workspaces" o -> do
              let refs = [r | A.Object w <- foldr (:) [] wss
                            , Just (A.String r) <- [KM.lookup "ref" w]]
              entries <- mapM surfacesFor (zip [0 ..] refs)
              pure (Map.fromList (concat entries))
        _ -> pure Map.empty
    _ -> pure Map.empty
  where
    surfacesFor (wsIdx, ref) = do
      surfResult <- try (readProcessWithExitCode "cmux"
        ["list-pane-surfaces", "--workspace", T.unpack ref, "--id-format", "uuids"] "")
      case surfResult :: Either SomeException (ExitCode, String, String) of
        Right (ExitSuccess, out, _) ->
          pure [ (uuid, wsIdx * 1000 + surfIdx)
               | (surfIdx, line) <- zip [0 ..] (filter (not . T.null) (T.lines (T.pack out)))
               , let uuid = extractSurfaceUuid line
               , not (T.null uuid)
               ]
        _ -> pure []

-- | Finds a 36-char 8-4-4-4-12 UUID substring in a line; \"\" if none.
extractSurfaceUuid :: Text -> Text
extractSurfaceUuid line = go (T.unpack line)
  where
    go s
      | length s < 36 = ""
      | isUuid (take 36 s) = T.pack (take 36 s)
      | otherwise = go (drop 1 s)
    isUuid s =
      length s == 36
      && and [ if i `elem` [8, 13, 18, 23 :: Int] then c == '-' else isHexDigit c
             | (i, c) <- zip [0 ..] s ]

------------------------------------------------------------------------
-- Resources

-- | GET /api/resources — active subscriptions grouped by resource, plus
-- watcher health for github and jira.
handleResources :: ApiConfig -> ActionM ()
handleResources cfg = do
  result <- liftIO $ try $ do
    rows <- SQL.query_ cfg.db.conn
      "SELECT s.resource_type, s.resource_id, s.resource_url, s.session_id, sess.session_name, sess.status, sess.pid, sess.last_prompt\
      \ FROM subscriptions s\
      \ INNER JOIN sessions sess ON sess.session_id = s.session_id\
      \ WHERE s.deleted_at IS NULL AND sess.status != 'archived'\
      \ ORDER BY s.resource_type, s.resource_id, s.created_at"
      :: IO [(Text, Text, Maybe Text, Text, Maybe Text, Text, Maybe Int, Maybe Text)]

    -- Group by resource, keeping first-seen order (Go used a map with
    -- random iteration order; insertion order is a deterministic stand-in).
    let keyOf (rt, rid, _, _, _, _, _, _) = rt <> "::" <> rid
        keys = dedup [keyOf r | r <- rows]
        dedup = foldl' (\acc k -> if k `elem` acc then acc else acc ++ [k]) []
        grouped = [ (k, [r | r <- rows, keyOf r == k]) | k <- keys ]

    resources <- mapM (buildResourceEntry cfg.db) grouped

    cfg' <- either (\(_ :: SomeException) -> emptyConfig)
              id <$> try (readConfig =<< configDefaultPath)
    ghStatus <- buildWatcherStatus cfg.db cfg' "github"
    jiraStatus <- buildWatcherStatus cfg.db cfg' "jira"
    pure (resources, ghStatus, jiraStatus)
  case result :: Either SomeException ([Value], Value, Value) of
    Left e -> do
      liftIO $ logLine ("Error querying subscriptions: " <> T.pack (show e))
      writeErr status500 "Failed to query subscriptions"
    Right (resources, ghStatus, jiraStatus) ->
      json $ object
        [ "resources" .= resources
        , "watchers" .= object ["github" .= ghStatus, "jira" .= jiraStatus]
        ]

buildResourceEntry
  :: Db -> (Text, [(Text, Text, Maybe Text, Text, Maybe Text, Text, Maybe Int, Maybe Text)])
  -> IO Value
buildResourceEntry db (_, rows@((rType, rId, rUrl, _, _, _, _, _) : _)) = do
  mstate <- either (\(_ :: SomeException) -> Nothing) id <$> try (getResourceState db rType rId)
  sessions <- mapM sessionEntry rows
  let stateFields = case mstate of
        Nothing -> []
        Just st ->
          (case A.decode (BL.fromStrict (TE.encodeUtf8 st.stateJson)) of
             Just (A.Object o) | not (KM.null o) -> ["state" .= A.Object o]
             _ -> [])
          ++ [ "resource_updated_at" .= st.resourceUpdatedAt | st.resourceUpdatedAt /= "" ]
          ++ [ "watcher_updated_at" .= st.watcherUpdatedAt | st.watcherUpdatedAt /= "" ]
  pure $ object $
    [ "resource_type" .= rType
    , "resource_id" .= rId
    , "sessions" .= sessions
    ]
    ++ [ "resource_url" .= u | Just u <- [rUrl], u /= "" ]
    ++ stateFields
  where
    sessionEntry (_, _, _, sessionId, sessionName, sessStatus, pid, _) = do
      displayState <-
        if sessStatus == "archived"
          then pure ("archived" :: Text)
          else do
            alive <- isSessionProcess (fromMaybe 0 pid) sessionId
            pure (if alive then "idle" else "dead")
      pure $ object
        [ "session_id" .= sessionId
        , "session_name" .= fromMaybe "" sessionName
        , "display_state" .= displayState
        ]
buildResourceEntry _ (_, []) = pure A.Null

-- | Watcher health info for a service (configured/installed/errors).
buildWatcherStatus :: Db -> Config -> Text -> IO Value
buildWatcherStatus db cfg service = do
  installed <- isWatcherInstalled service
  mws <- either (\(_ :: SomeException) -> Nothing) id <$> try (getWatcherStatus db service)
  (lastSuccess, lastError, hasError) <- case mws of
    Nothing -> pure (Nothing, Nothing, False)
    Just ws -> do
      hasErr <- if ws.lastError /= "" then hasWatcherError db service else pure False
      pure ( if ws.lastSuccess /= "" then Just ws.lastSuccess else Nothing
           , if ws.lastError /= "" then Just ws.lastError else Nothing
           , hasErr )
  pure $ object $
    [ "configured" .= isServiceConfigured cfg service
    , "installed" .= installed
    , "has_error" .= hasError
    ]
    ++ [ "last_success" .= s | Just s <- [lastSuccess] ]
    ++ [ "last_error" .= e | Just e <- [lastError] ]

-- | Local port of watcher.IsInstalled (scheduler.go): launchd plist on
-- macOS, crontab marker elsewhere.
isWatcherInstalled :: Text -> IO Bool
isWatcherInstalled name
  | System.Info.os == "darwin" = do
      home <- getHomeDirectory
      doesFileExist ( home </> "Library" </> "LaunchAgents"
                      </> ("com.agent-handler.watcher-" <> T.unpack name <> ".plist") )
  | otherwise = do
      result <- try (readProcessWithExitCode "crontab" ["-l"] "")
      pure $ case result :: Either SomeException (ExitCode, String, String) of
        Right (ExitSuccess, out, errOut) ->
          ("# agent-handler-" <> T.unpack name) `isInfixOf` (out <> errOut)
        _ -> False

------------------------------------------------------------------------
-- Events timeline

-- | Row shape for the timeline query (12 columns, beyond tuple FromRow).
data TimelineRow = TimelineRow
  { trId          :: Text
  , trTs          :: Text
  , trSource      :: Text
  , trSessionId   :: Maybe Text
  , trSessionName :: Text
  , trType        :: Text
  , trTitle       :: Text
  , trBody        :: Maybe Text
  , trAuthor      :: Maybe Text
  , trAuthorType  :: Maybe Text
  , trBroadcast   :: Int
  , trTags        :: Maybe Text
  }

instance SQL.FromRow TimelineRow where
  fromRow = TimelineRow
    <$> SQL.field <*> SQL.field <*> SQL.field <*> SQL.field
    <*> SQL.field <*> SQL.field <*> SQL.field <*> SQL.field
    <*> SQL.field <*> SQL.field <*> SQL.field <*> SQL.field

-- | GET /api/events?before=&limit=&session=&type=&source=&search=
handleEvents :: ApiConfig -> ActionM ()
handleEvents cfg = do
  before <- fromMaybe "" <$> (queryParamMaybe "before" :: ActionM (Maybe Text))
  limitQ <- queryParamMaybe "limit" :: ActionM (Maybe Int)
  sessionFilter <- fromMaybe "" <$> (queryParamMaybe "session" :: ActionM (Maybe Text))
  typeFilter <- fromMaybe "" <$> (queryParamMaybe "type" :: ActionM (Maybe Text))
  sourceFilter <- fromMaybe "" <$> (queryParamMaybe "source" :: ActionM (Maybe Text))
  searchFilter <- fromMaybe "" <$> (queryParamMaybe "search" :: ActionM (Maybe Text))
  let limit = case limitQ of
        Just n | n > 0 && n <= 200 -> n
        _ -> 50

  let clauses = concat
        [ [ (" AND e.ts < ?", [toField (before :: Text)]) | before /= "" ]
        , [ ( " AND (e.session_id = ? OR e.id IN (\
              \ SELECT er.event_id FROM event_resources er\
              \ JOIN subscriptions sub ON er.resource_type = sub.resource_type AND er.resource_id = sub.resource_id\
              \ WHERE sub.session_id = ?))"
            , [toField sessionFilter, toField sessionFilter] )
          | sessionFilter /= "" ]
        , [ let types = map T.strip (T.splitOn "," typeFilter)
                placeholders = T.intercalate "," (map (const "?") types)
            in (" AND e.type IN (" <> placeholders <> ")", map toField types)
          | typeFilter /= "" ]
        , [ (" AND e.source = ?", [toField sourceFilter]) | sourceFilter /= "" ]
        , [ let term = "%" <> searchFilter <> "%"
            in (" AND (e.title LIKE ? OR e.body LIKE ?)", [toField term, toField term])
          | searchFilter /= "" ]
        ]
      sql = "SELECT e.id, e.ts, e.source, e.session_id, COALESCE(s.session_name, ''),\
            \       e.type, e.title, e.body, e.author, e.author_type, e.broadcast, e.tags\
            \ FROM events e\
            \ LEFT JOIN sessions s ON e.session_id = s.session_id\
            \ WHERE 1=1"
            <> mconcat (map fst clauses)
            <> " ORDER BY e.ts DESC LIMIT ?"
      args = concatMap snd clauses ++ [toField (limit + 1)]

  result <- liftIO $ try $ do
    rows <- SQL.query cfg.db.conn (SQL.Query sql) args
    let hasMore = length rows > limit
        page = take limit rows
    events <- mapM (timelineEventJson cfg.db) page
    pure (events, hasMore, if null page then "" else (last page).trTs)
  case result :: Either SomeException ([Value], Bool, Text) of
    Left e -> do
      liftIO $ logLine ("Error querying events: " <> T.pack (show e))
      writeErr status500 "Failed to query events"
    Right (events, hasMore, nextCursor) ->
      json $ object
        [ "events" .= events
        , "has_more" .= hasMore
        , "next_cursor" .= nextCursor
        ]

-- | A timeline event with its resources; null for absent optional fields
-- (the Go struct has no omitempty here).
timelineEventJson :: Db -> TimelineRow -> IO Value
timelineEventJson db row = do
  resources <- either (\(_ :: SomeException) -> []) id
    <$> try (fetchEventResources db row.trId)
  pure $ object
    [ "id" .= row.trId
    , "ts" .= row.trTs
    , "source" .= row.trSource
    , "session_id" .= row.trSessionId
    , "session_name" .= row.trSessionName
    , "type" .= row.trType
    , "title" .= row.trTitle
    , "body" .= row.trBody
    , "author" .= row.trAuthor
    , "author_type" .= row.trAuthorType
    , "broadcast" .= (row.trBroadcast == 1)
    , "tags" .= row.trTags
    , "resources" .= resources
    ]

-- | All resources for an event, with metadata extracted from cached state.
fetchEventResources :: Db -> Text -> IO [Value]
fetchEventResources db eventId = do
  rows <- SQL.query db.conn
    "SELECT er.resource_type, er.resource_id, er.resource_url, rs.state_json\
    \ FROM event_resources er\
    \ LEFT JOIN resource_state rs ON er.resource_type = rs.resource_type AND er.resource_id = rs.resource_id\
    \ WHERE er.event_id = ?"
    (SQL.Only eventId)
  pure
    [ object $
        [ "resource_type" .= rType
        , "resource_id" .= rId
        , "resource_url" .= (rUrl :: Maybe Text)
        ]
        ++ [ "metadata" .= meta
           | Just sj <- [stateJson]
           , Just meta <- [extractResourceMetadata rType sj]
           ]
    | (rType, rId, rUrl, stateJson) <- rows :: [(Text, Text, Maybe Text, Maybe Text)]
    ]

-- | Pulls display metadata out of a resource's cached state JSON.
extractResourceMetadata :: Text -> Text -> Maybe (Map Text Text)
extractResourceMetadata resourceType stateJson = do
  o <- case A.decode (BL.fromStrict (TE.encodeUtf8 stateJson)) of
    Just (A.Object o) -> Just o
    _ -> Nothing
  let str k = case KM.lookup k o of
        Just (A.String s) -> s
        _ -> ""
      keep pairs = let m = Map.fromList [(k, v) | (k, v) <- pairs, v /= ""]
                   in if Map.null m then Nothing else Just m
  case resourceType of
    "pr" -> keep [("title", str "title"), ("author", str "author"), ("state", str "state")]
    "jira" -> keep
      [ ("title", str "summary"), ("assignee", str "assignee")
      , ("priority", str "priority"), ("status", str "status") ]
    _ -> Nothing

------------------------------------------------------------------------
-- SSE stream

-- | GET /api/stream — heartbeat every 5s, events_new when MAX(ts) moves.
handleStream :: ApiConfig -> ActionM ()
handleStream cfg = do
  setHeader "Content-Type" "text/event-stream"
  setHeader "Cache-Control" "no-cache"
  setHeader "Connection" "keep-alive"
  setHeader "Access-Control-Allow-Origin" "*"
  stream $ \send flush -> do
    flush
    initial <- maxEventTs
    loop send flush initial
  where
    maxEventTs :: IO Text
    maxEventTs = do
      result <- try (SQL.query_ cfg.db.conn "SELECT MAX(ts) FROM events")
      pure $ case result :: Either SomeException [SQL.Only (Maybe Text)] of
        Right (SQL.Only (Just ts) : _) -> ts
        _ -> ""
    sse send name payload =
      send (B.byteString (TE.encodeUtf8 ("event: " <> name <> "\ndata: " <> payload <> "\n\n")))
    loop send flush lastTs = do
      threadDelay (5 * 1000000)
      current <- maxEventTs
      let changed = current /= "" && current /= lastTs
      result <- try $ do
        when' changed $ sse send "events_new" "{\"type\":\"events_new\"}"
        sse send "heartbeat" "{\"type\":\"heartbeat\"}"
        flush
      case result :: Either SomeException () of
        Left _ -> pure ()  -- client went away
        Right () -> loop send flush (if changed then current else lastTs)
    when' c a = if c then a else pure ()

------------------------------------------------------------------------
-- Actions

-- | POST /api/actions/switch {"session_id": ...} — shells out to handler switch.
handleSwitch :: ActionM ()
handleSwitch = withJsonBody $ \o ->
  case strField o "session_id" of
    Just sid | sid /= "" -> do
      result <- liftIO $ try (readProcessWithExitCode "handler" ["switch", "--session", T.unpack sid] "")
      case result :: Either SomeException (ExitCode, String, String) of
        Right (ExitSuccess, out, errOut) ->
          json (object ["success" .= True, "output" .= (out <> errOut)])
        Right (_, out, errOut) -> do
          liftIO $ logLine ("Error switching to session " <> sid <> ": " <> T.pack (out <> errOut))
          writeErr status500 ("Failed to switch session: " <> T.pack (out <> errOut))
        Left e -> do
          liftIO $ logLine ("Error switching to session " <> sid <> ": " <> T.pack (show e))
          writeErr status500 "Failed to switch session: "
    _ -> writeErr status400 "session_id is required"

-- | POST /api/actions/peek {"session_id": ...} — shells out to handler peek --json.
handleForcePeek :: ActionM ()
handleForcePeek = withJsonBody $ \o ->
  case strField o "session_id" of
    Just sid | sid /= "" -> do
      result <- liftIO $ try (readProcessWithExitCode "handler" ["peek", "--session", T.unpack sid, "--json"] "")
      case result :: Either SomeException (ExitCode, String, String) of
        Right (ExitSuccess, out, errOut) ->
          case A.decode (BL.fromStrict (TE.encodeUtf8 (T.pack (out <> errOut)))) :: Maybe Value of
            Just v -> json v
            Nothing -> do
              liftIO $ logLine ("Error parsing peek output for " <> sid)
              writeErr status500 "Failed to parse peek output"
        Right (_, out, errOut) -> do
          liftIO $ logLine ("Error peeking session " <> sid <> ": " <> T.pack (out <> errOut))
          writeErr status500 ("Failed to peek session: " <> T.pack (out <> errOut))
        Left e -> do
          liftIO $ logLine ("Error peeking session " <> sid <> ": " <> T.pack (show e))
          writeErr status500 "Failed to peek session: "
    _ -> writeErr status400 "session_id is required"

-- | POST /api/actions/dismiss-inbox {"session_id": ...} — advances both
-- cursors via a fresh writable connection (the server's is read-only).
handleDismissInbox :: ActionM ()
handleDismissInbox = withJsonBody $ \o ->
  case strField o "session_id" of
    Just sid | sid /= "" -> do
      result <- liftIO $ try $ do
        path <- Db.defaultPath
        wdb <- Db.open path
        now <- nowIso
        advanceBothCursors wdb sid now
        Db.close wdb
      case result :: Either SomeException () of
        Left e -> do
          liftIO $ logLine ("Error advancing cursors for " <> sid <> ": " <> T.pack (show e))
          writeErr status500 "Failed to dismiss inbox"
        Right () -> json (object ["success" .= True])
    _ -> writeErr status400 "session_id is required"

-- | POST /api/actions/archive-sessions {"session_ids": [...]}.
handleArchiveSessions :: ActionM ()
handleArchiveSessions = withJsonBody $ \o ->
  case KM.lookup "session_ids" o of
    Just (A.Array arr)
      | ids <- [s | A.String s <- foldr (:) [] arr]
      , not (null ids) -> do
          result <- liftIO $ try $ do
            path <- Db.defaultPath
            wdb <- Db.open path
            count <- archiveSessions wdb ids
            Db.close wdb
            pure count
          case result :: Either SomeException Int of
            Left e -> do
              liftIO $ logLine ("Error archiving sessions: " <> T.pack (show e))
              writeErr status500 "Failed to archive sessions"
            Right count -> json (object ["success" .= True, "archived" .= count])
    _ -> writeErr status400 "session_ids is required"

------------------------------------------------------------------------
-- Static files + SPA fallback

serveSpaIndex :: ActionM ()
serveSpaIndex = case webIndexHtml of
  Just index -> do
    setHeader "Content-Type" "text/html"
    raw (BL.fromStrict index)
  Nothing -> do
    status status404
    text "404 page not found"

-- | Serves the exact embedded file when it exists, else the SPA index.
serveStatic :: ActionM ()
serveStatic = do
  req <- request
  let path = T.drop 1 (TE.decodeUtf8 (rawPathInfo req))
  case lookupWebFile path of
    Just contents -> do
      setHeader "Content-Type" (TL.fromStrict (contentTypeFor path))
      raw (BL.fromStrict contents)
    Nothing -> serveSpaIndex
