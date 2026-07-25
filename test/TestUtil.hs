-- | Shared helpers for the test suite: temp databases and seed data,
-- mirroring the testDB/seedSession helpers in the Go tests.
module TestUtil
  ( withTestDb
  , seedSession
  , mkSession
  ) where

import Control.Exception (bracket)
import Data.Text (Text)
import System.FilePath ((</>))
import System.IO.Temp (withSystemTempDirectory)

import Handler.Db (Db, close, open)
import Handler.Db.Sessions (Session(..), upsertSession)
import Handler.Util (nowIso)

-- | Runs an action against a fresh database in a temp directory.
withTestDb :: (Db -> IO a) -> IO a
withTestDb f =
  withSystemTempDirectory "handler-test" $ \dir ->
    bracket (open (dir </> "test.db")) close f

-- | A minimal session record with the given ID and timestamps.
mkSession :: Text -> Text -> Session
mkSession sid now = Session
  { sessionId = sid
  , harness = "claude"
  , repo = "github.com/example/repo"
  , branch = "main"
  , sessionName = ""
  , pid = 0
  , status = "active"
  , inboxMode = "manual"
  , autoPollInterval = Nothing
  , role = ""
  , terminalType = ""
  , terminalId = ""
  , cmuxWorkspaceId = ""
  , cmuxWorkspaceName = ""
  , cmuxWorkspaceColor = ""
  , lastActive = now
  , lastPrompt = ""
  , cwd = ""
  , registeredAt = now
  , jsonlPath = "/path/to/session.jsonl"
  }

-- | Inserts a minimal session for use in tests.
seedSession :: Db -> Text -> IO ()
seedSession db sid = do
  now <- nowIso
  upsertSession db (mkSession sid now)
