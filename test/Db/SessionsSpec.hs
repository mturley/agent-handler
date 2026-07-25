-- | Port of db/sessions_test.go.
module Db.SessionsSpec (spec) where

import Test.Hspec

import Handler.Db.Sessions
  ( Session(..), archiveSessions, bumpLastActive, getSession, listSessions
  , upsertSession )
import Handler.Util (nowIso)
import TestUtil (mkSession, withTestDb)

spec :: Spec
spec = do
  it "TestUpsertAndGetSession: round-trips every field" $ withTestDb $ \db -> do
    now <- nowIso
    let s = (mkSession "session-123" now)
          { sessionName = "test-session"
          , pid = 12345
          , inboxMode = "auto"
          , autoPollInterval = Just 300
          }
    upsertSession db s
    mgot <- getSession db "session-123"
    case mgot of
      Nothing -> expectationFailure "expected session, got Nothing"
      Just got -> do
        got.sessionId `shouldBe` "session-123"
        got.harness `shouldBe` "claude"
        got.repo `shouldBe` "github.com/example/repo"
        got.branch `shouldBe` "main"
        got.sessionName `shouldBe` "test-session"
        got.pid `shouldBe` 12345
        got.status `shouldBe` "active"
        got.inboxMode `shouldBe` "auto"
        got.autoPollInterval `shouldBe` Just 300
        got.lastActive `shouldBe` now
        got.registeredAt `shouldBe` now
        got.jsonlPath `shouldBe` "/path/to/session.jsonl"

  it "TestUpsertSessionUpdatesExisting: preserves inbox_mode and auto_poll_interval" $ withTestDb $ \db -> do
    now <- nowIso
    upsertSession db (mkSession "session-456" now)
      { sessionName = "old-name"
      , pid = 12345
      , inboxMode = "auto"
      , autoPollInterval = Just 300
      }
    upsertSession db (mkSession "session-456" now)
      { sessionName = "new-name"
      , pid = 67890
      , inboxMode = ""            -- should preserve existing "auto"
      , autoPollInterval = Nothing -- should preserve existing 300
      }
    mgot <- getSession db "session-456"
    case mgot of
      Nothing -> expectationFailure "expected session"
      Just got -> do
        got.pid `shouldBe` 67890
        got.sessionName `shouldBe` "new-name"
        got.inboxMode `shouldBe` "auto"
        got.autoPollInterval `shouldBe` Just 300

  it "TestListSessionsFiltersArchived" $ withTestDb $ \db -> do
    now <- nowIso
    upsertSession db (mkSession "session-active" now)
    upsertSession db (mkSession "session-archived" now) { status = "archived" }

    active <- listSessions db False 100 0
    map (.sessionId) active `shouldBe` ["session-active"]

    everything <- listSessions db True 100 0
    length everything `shouldBe` 2

  it "TestBumpLastActive: updates and errors on missing session" $ withTestDb $ \db -> do
    now <- nowIso
    upsertSession db (mkSession "session-bump" now)
    bumpLastActive db "session-bump" "2030-01-01T00:00:00Z"
    mgot <- getSession db "session-bump"
    fmap (.lastActive) mgot `shouldBe` Just "2030-01-01T00:00:00Z"
    bumpLastActive db "nonexistent" now `shouldThrow` anyIOException

  it "TestArchiveDeadSessions: archives the given IDs" $ withTestDb $ \db -> do
    now <- nowIso
    upsertSession db (mkSession "session-to-archive-1" now)
    upsertSession db (mkSession "session-to-archive-2" now) { branch = "feature" }
    count <- archiveSessions db ["session-to-archive-1", "session-to-archive-2"]
    count `shouldBe` 2
    m1 <- getSession db "session-to-archive-1"
    fmap (.status) m1 `shouldBe` Just "archived"
    m2 <- getSession db "session-to-archive-2"
    fmap (.status) m2 `shouldBe` Just "archived"

  it "TestSessionTerminalFields: stores and updates terminal metadata" $ withTestDb $ \db -> do
    let now = "2026-07-02T10:00:00Z"
    upsertSession db (mkSession "terminal-test" now)
      { harness = "claude-code", repo = "test/repo", pid = 1234
      , jsonlPath = "/tmp/test.jsonl"
      , terminalType = "cmux", terminalId = "surface-uuid-123"
      }
    m1 <- getSession db "terminal-test"
    fmap (.terminalType) m1 `shouldBe` Just "cmux"
    fmap (.terminalId) m1 `shouldBe` Just "surface-uuid-123"

    upsertSession db (mkSession "terminal-test" now)
      { harness = "claude-code", repo = "test/repo", pid = 5678
      , jsonlPath = "/tmp/test.jsonl"
      , terminalType = "tmux", terminalId = "%42"
      }
    m2 <- getSession db "terminal-test"
    fmap (.terminalType) m2 `shouldBe` Just "tmux"
    fmap (.terminalId) m2 `shouldBe` Just "%42"

  it "TestSessionTerminalFieldsEmpty: defaults to empty strings" $ withTestDb $ \db -> do
    let now = "2026-07-02T10:00:00Z"
    upsertSession db (mkSession "no-terminal-test" now)
      { harness = "claude-code", repo = "test/repo", pid = 1234
      , jsonlPath = "/tmp/test.jsonl"
      }
    mgot <- getSession db "no-terminal-test"
    fmap (.terminalType) mgot `shouldBe` Just ""
    fmap (.terminalId) mgot `shouldBe` Just ""
