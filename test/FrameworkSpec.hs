-- | Port of watcher/framework_test.go.
module FrameworkSpec (spec) where

import Test.Hspec

import Handler.Db.Events (Event(..), EventResource(..), insertEvent)
import Handler.Db.Sessions (Session(..), upsertSession)
import Handler.Db.Subscriptions (Subscription(..), subscribe)
import Handler.Util (newUuid, nowIso)
import Handler.Watcher.Framework (WatchedResource(..), activeResources, eventCursor, isDuplicate)
import TestUtil (mkSession, seedSession, withTestDb)

spec :: Spec
spec = do
  it "TestActiveResources: finds subscriptions of active sessions" $ withTestDb $ \db -> do
    now <- nowIso
    seedSession db "session-123"
    subId <- newUuid
    subscribe db Subscription
      { subId = subId, sessionId = "session-123", resourceType = "github_pr"
      , resourceId = "owner/repo#123"
      , resourceUrl = Just "https://github.com/owner/repo/pull/123"
      , createdAt = now, deletedAt = Nothing
      }
    resources <- activeResources db "github_pr"
    resources `shouldBe`
      [ WatchedResource "github_pr" "owner/repo#123" "https://github.com/owner/repo/pull/123" ]

  it "TestActiveResourcesSkipsArchived" $ withTestDb $ \db -> do
    now <- nowIso
    upsertSession db (mkSession "session-archived" now) { status = "archived" }
    subId <- newUuid
    subscribe db Subscription
      { subId = subId, sessionId = "session-archived", resourceType = "github_pr"
      , resourceId = "owner/repo#456"
      , resourceUrl = Just "https://github.com/owner/repo/pull/456"
      , createdAt = now, deletedAt = Nothing
      }
    resources <- activeResources db "github_pr"
    resources `shouldBe` []

  it "TestEventCursorEmpty" $ withTestDb $ \db -> do
    cursor <- eventCursor db "github_pr_watcher" "github_pr" "owner/repo#123"
    cursor `shouldBe` ""

  it "TestEventCursorAfterEvent" $ withTestDb $ \db -> do
    let externalTs = "2024-01-15T12:00:00Z"
    eid <- newUuid
    now <- nowIso
    insertEvent db
      Event
        { eventId = eid, ts = now, externalTs = Just externalTs
        , source = "github_pr_watcher", sessionId = Nothing
        , eventType = "pr_comment", title = "New comment"
        , body = Nothing, author = Nothing, authorType = Nothing
        , broadcast = False, tags = Nothing
        }
      []
      [ EventResource "github_pr" "owner/repo#123"
          (Just "https://github.com/owner/repo/pull/123") ]
    cursor <- eventCursor db "github_pr_watcher" "github_pr" "owner/repo#123"
    cursor `shouldBe` externalTs

  it "TestIsDuplicate: matches on source+resource+type+external_ts" $ withTestDb $ \db -> do
    let externalTs = "2024-01-15T12:00:00Z"
    eid <- newUuid
    now <- nowIso
    insertEvent db
      Event
        { eventId = eid, ts = now, externalTs = Just externalTs
        , source = "github_pr_watcher", sessionId = Nothing
        , eventType = "pr_comment", title = "New comment"
        , body = Nothing, author = Nothing, authorType = Nothing
        , broadcast = False, tags = Nothing
        }
      []
      [ EventResource "github_pr" "owner/repo#123"
          (Just "https://github.com/owner/repo/pull/123") ]

    dup <- isDuplicate db "github_pr_watcher" "github_pr" "owner/repo#123" "pr_comment" externalTs
    dup `shouldBe` True
    dupTs <- isDuplicate db "github_pr_watcher" "github_pr" "owner/repo#123" "pr_comment" "2024-01-15T13:00:00Z"
    dupTs `shouldBe` False
    dupType <- isDuplicate db "github_pr_watcher" "github_pr" "owner/repo#123" "pr_review" externalTs
    dupType `shouldBe` False
    dupRes <- isDuplicate db "github_pr_watcher" "github_pr" "owner/repo#456" "pr_comment" externalTs
    dupRes `shouldBe` False
