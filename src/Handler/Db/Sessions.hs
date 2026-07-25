-- | Port of db/sessions.go: the session registry.
module Handler.Db.Sessions
  ( Session(..)
  , upsertSession
  , getSession
  , listSessions
  , listArchivedSessions
  , listSessionsByName
  , bumpLastActive
  , bumpLastPrompt
  , archiveSessions
  , configureSession
  , sessionToJson
  ) where

import Control.Monad (when)
import Data.Aeson (Value, object, (.=))
import Data.Text (Text)
import qualified Data.Text as T
import qualified Database.SQLite.Simple as SQL
import Database.SQLite.Simple (NamedParam(..))
import Database.SQLite.Simple.ToField (toField)

import Handler.Db (Db(..))

-- | A Claude Code session registered with the handler.
-- Nullable text columns are represented as Text coalesced to \"\" (as in Go);
-- auto_poll_interval keeps its Maybe.
data Session = Session
  { sessionId          :: Text
  , harness            :: Text
  , repo               :: Text
  , branch             :: Text
  , sessionName        :: Text
  , pid                :: Int
  , status             :: Text
  , inboxMode          :: Text
  , autoPollInterval   :: Maybe Int
  , role               :: Text
  , terminalType       :: Text
  , terminalId         :: Text
  , cmuxWorkspaceId    :: Text
  , cmuxWorkspaceName  :: Text
  , cmuxWorkspaceColor :: Text
  , lastActive         :: Text
  , lastPrompt         :: Text
  , cwd                :: Text
  , registeredAt       :: Text
  , jsonlPath          :: Text
  } deriving (Show, Eq)

instance SQL.FromRow Session where
  fromRow =
    Session
      <$> SQL.field <*> SQL.field <*> SQL.field <*> SQL.field
      <*> SQL.field <*> SQL.field <*> SQL.field <*> SQL.field
      <*> SQL.field <*> SQL.field <*> SQL.field <*> SQL.field
      <*> SQL.field <*> SQL.field <*> SQL.field <*> SQL.field
      <*> SQL.field <*> SQL.field <*> SQL.field <*> SQL.field

-- | The SELECT column list used by every session query, with the same
-- COALESCE defaults as the Go code.
sessionColumns :: Text
sessionColumns = T.unlines
  [ "session_id, harness, repo, branch,"
  , "COALESCE(session_name, '') as session_name,"
  , "COALESCE(pid, 0) as pid,"
  , "status,"
  , "inbox_mode,"
  , "auto_poll_interval,"
  , "COALESCE(role, '') as role,"
  , "COALESCE(terminal_type, '') as terminal_type,"
  , "COALESCE(terminal_id, '') as terminal_id,"
  , "COALESCE(cmux_workspace_id, '') as cmux_workspace_id,"
  , "COALESCE(cmux_workspace_name, '') as cmux_workspace_name,"
  , "COALESCE(cmux_workspace_color, '') as cmux_workspace_color,"
  , "last_active,"
  , "COALESCE(last_prompt, '') as last_prompt,"
  , "COALESCE(cwd, '') as cwd,"
  , "registered_at, jsonl_path"
  ]

-- | JSON rendering shared by CLI --json output and the API server.
sessionToJson :: Session -> Value
sessionToJson s = object
  [ "session_id" .= s.sessionId
  , "harness" .= s.harness
  , "repo" .= s.repo
  , "branch" .= s.branch
  , "session_name" .= s.sessionName
  , "pid" .= s.pid
  , "status" .= s.status
  , "inbox_mode" .= s.inboxMode
  , "auto_poll_interval" .= s.autoPollInterval
  , "role" .= s.role
  , "terminal_type" .= s.terminalType
  , "terminal_id" .= s.terminalId
  , "cmux_workspace_id" .= s.cmuxWorkspaceId
  , "cmux_workspace_name" .= s.cmuxWorkspaceName
  , "cmux_workspace_color" .= s.cmuxWorkspaceColor
  , "last_active" .= s.lastActive
  , "last_prompt" .= s.lastPrompt
  , "cwd" .= s.cwd
  , "registered_at" .= s.registeredAt
  , "jsonl_path" .= s.jsonlPath
  ]

-- | Inserts or updates a session. On conflict, inbox_mode and role are
-- preserved and auto_poll_interval/last_prompt are COALESCEd, matching Go.
upsertSession :: Db -> Session -> IO ()
upsertSession db s =
  SQL.execute db.conn
    "INSERT INTO sessions (\
    \  session_id, harness, repo, branch, session_name, pid, status,\
    \  inbox_mode, auto_poll_interval, role, terminal_type, terminal_id,\
    \  cmux_workspace_id, cmux_workspace_name, cmux_workspace_color, last_active, last_prompt, cwd, registered_at, jsonl_path\
    \) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)\
    \ ON CONFLICT(session_id) DO UPDATE SET\
    \  harness = excluded.harness,\
    \  repo = excluded.repo,\
    \  branch = excluded.branch,\
    \  session_name = excluded.session_name,\
    \  pid = excluded.pid,\
    \  status = excluded.status,\
    \  inbox_mode = sessions.inbox_mode,\
    \  auto_poll_interval = COALESCE(excluded.auto_poll_interval, sessions.auto_poll_interval),\
    \  role = sessions.role,\
    \  terminal_type = excluded.terminal_type,\
    \  terminal_id = excluded.terminal_id,\
    \  cmux_workspace_id = excluded.cmux_workspace_id,\
    \  cmux_workspace_name = excluded.cmux_workspace_name,\
    \  cmux_workspace_color = excluded.cmux_workspace_color,\
    \  last_active = excluded.last_active,\
    \  last_prompt = COALESCE(sessions.last_prompt, excluded.last_prompt),\
    \  cwd = excluded.cwd,\
    \  registered_at = excluded.registered_at,\
    \  jsonl_path = excluded.jsonl_path"
    [ toField s.sessionId, toField s.harness, toField s.repo, toField s.branch
    , toField s.sessionName, toField s.pid, toField s.status
    , toField s.inboxMode, toField s.autoPollInterval, toField s.role
    , toField s.terminalType, toField s.terminalId
    , toField s.cmuxWorkspaceId, toField s.cmuxWorkspaceName, toField s.cmuxWorkspaceColor
    , toField s.lastActive, toField s.lastPrompt, toField s.cwd
    , toField s.registeredAt, toField s.jsonlPath
    ]

-- | Retrieves a session by ID; Nothing if it does not exist.
getSession :: Db -> Text -> IO (Maybe Session)
getSession db sid = do
  rows <- SQL.query db.conn
    (SQL.Query ("SELECT " <> sessionColumns <> " FROM sessions WHERE session_id = ?"))
    (SQL.Only sid)
  pure $ case rows of
    (s : _) -> Just s
    []      -> Nothing

-- | Sessions ordered by last_active DESC, optionally filtering out archived.
listSessions :: Db -> Bool -> Int -> Int -> IO [Session]
listSessions db includeArchived limit offset =
  SQL.query db.conn
    (SQL.Query ("SELECT " <> sessionColumns <> " FROM sessions " <> whereSql
                <> " ORDER BY last_active DESC LIMIT ? OFFSET ?"))
    (limit, offset)
  where
    whereSql = if includeArchived then "" else "WHERE status != 'archived'"

-- | Archived sessions with optional search; returns (sessions, total count).
listArchivedSessions :: Db -> Text -> Text -> Int -> Int -> IO ([Session], Int)
listArchivedSessions db search sort limit offset = do
  let (whereSql, searchArgs)
        | T.null search = ("WHERE status = 'archived'", [])
        | otherwise =
            ( "WHERE status = 'archived' AND (COALESCE(session_name, '') LIKE ? OR repo LIKE ?)"
            , let term = "%" <> search <> "%" in [toField term, toField term] )
  totals <- SQL.query db.conn
    (SQL.Query ("SELECT COUNT(*) FROM sessions " <> whereSql)) searchArgs
  let total = case totals of { (SQL.Only n : _) -> n; _ -> 0 }
  sessions <- SQL.query db.conn
    (SQL.Query ("SELECT " <> sessionColumns <> " FROM sessions " <> whereSql
                <> " ORDER BY " <> archivedSortClause sort <> " LIMIT ? OFFSET ?"))
    (searchArgs ++ [toField limit, toField offset])
  pure (sessions, total)

archivedSortClause :: Text -> Text
archivedSortClause = \case
  "name"         -> "COALESCE(session_name, session_id) ASC"
  "-name"        -> "COALESCE(session_name, session_id) DESC"
  "last_prompt"  -> "COALESCE(last_prompt, '') DESC"
  "-last_prompt" -> "COALESCE(last_prompt, '') ASC"
  _              -> "COALESCE(last_prompt, '') DESC"

-- | All active sessions with the given name, most recently active first.
listSessionsByName :: Db -> Text -> IO [Session]
listSessionsByName db name =
  SQL.query db.conn
    (SQL.Query ("SELECT " <> sessionColumns
                <> " FROM sessions WHERE session_name = ? AND status = 'active'"
                <> " ORDER BY last_active DESC"))
    (SQL.Only name)

-- | Updates last_active; errors if the session is not found.
bumpLastActive :: Db -> Text -> Text -> IO ()
bumpLastActive db sid ts = do
  SQL.execute db.conn
    "UPDATE sessions SET last_active = ? WHERE session_id = ?" (ts, sid)
  n <- SQL.changes db.conn
  when (n == 0) $ fail ("session not found: " <> T.unpack sid)

bumpLastPrompt :: Db -> Text -> Text -> IO ()
bumpLastPrompt db sid ts =
  SQL.execute db.conn
    "UPDATE sessions SET last_prompt = ? WHERE session_id = ?" (ts, sid)

-- | Sets status='archived' for the given IDs; returns how many were archived.
archiveSessions :: Db -> [Text] -> IO Int
archiveSessions _ [] = pure 0
archiveSessions db sids = do
  let placeholders = T.intercalate ", " (replicate (length sids) "?")
  SQL.execute db.conn
    (SQL.Query ("UPDATE sessions SET status = 'archived' WHERE session_id IN (" <> placeholders <> ")"))
    sids
  SQL.changes db.conn

-- | Updates inbox_mode, auto_poll_interval, and role for a session.
configureSession :: Db -> Text -> Text -> Maybe Int -> Maybe Text -> IO ()
configureSession db sid inboxMode autoPollInterval role = do
  SQL.executeNamed db.conn
    "UPDATE sessions SET inbox_mode = :mode, auto_poll_interval = :interval, role = COALESCE(:role, role) WHERE session_id = :sid"
    [ ":mode" := inboxMode, ":interval" := autoPollInterval
    , ":role" := role, ":sid" := sid ]
  n <- SQL.changes db.conn
  when (n == 0) $ fail ("session not found: " <> T.unpack sid)
