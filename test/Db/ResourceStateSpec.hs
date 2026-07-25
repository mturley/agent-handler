-- | Port of db/resource_state_test.go.
module Db.ResourceStateSpec (spec) where

import Data.Maybe (isJust, isNothing)
import Test.Hspec

import Handler.Db.ResourceState
  ( ResourceState(..), deleteResourceState, getResourceState
  , listResourceStatesForSession, upsertResourceState )
import Handler.Db.Subscriptions (Subscription(..), subscribe, unsubscribe)
import TestUtil (seedSession, withTestDb)

spec :: Spec
spec = do
  it "TestUpsertAndGetResourceState" $ withTestDb $ \db -> do
    upsertResourceState db "pr" "owner/repo#1" "{\"state\":\"open\"}"
      "2026-07-06T10:00:00Z" "2026-07-06T10:01:00Z"
    mrs <- getResourceState db "pr" "owner/repo#1"
    case mrs of
      Nothing -> expectationFailure "expected resource state"
      Just rs -> do
        rs.stateJson `shouldBe` "{\"state\":\"open\"}"
        rs.resourceUpdatedAt `shouldBe` "2026-07-06T10:00:00Z"
        rs.watcherUpdatedAt `shouldBe` "2026-07-06T10:01:00Z"

  it "TestUpsertResourceStateOverwrites" $ withTestDb $ \db -> do
    upsertResourceState db "pr" "owner/repo#1" "{\"state\":\"open\"}"
      "2026-07-06T10:00:00Z" "2026-07-06T10:01:00Z"
    upsertResourceState db "pr" "owner/repo#1" "{\"state\":\"merged\"}"
      "2026-07-06T11:00:00Z" "2026-07-06T11:01:00Z"
    mrs <- getResourceState db "pr" "owner/repo#1"
    fmap (.stateJson) mrs `shouldBe` Just "{\"state\":\"merged\"}"

  it "TestGetResourceStateNotFound" $ withTestDb $ \db -> do
    mrs <- getResourceState db "pr" "nonexistent"
    isNothing mrs `shouldBe` True

  it "TestDeleteResourceState" $ withTestDb $ \db -> do
    upsertResourceState db "jira" "PROJ-1" "{\"status\":\"open\"}"
      "2026-07-06T10:00:00Z" "2026-07-06T10:01:00Z"
    deleteResourceState db "jira" "PROJ-1"
    mrs <- getResourceState db "jira" "PROJ-1"
    isNothing mrs `shouldBe` True

  it "TestListResourceStatesForSession" $ withTestDb $ \db -> do
    seedSession db "rs-session-test"
    let now = "2026-07-06T10:00:00Z"
    subscribe db Subscription
      { subId = "sub-1", sessionId = "rs-session-test", resourceType = "pr"
      , resourceId = "owner/repo#1", resourceUrl = Nothing
      , createdAt = now, deletedAt = Nothing
      }
    subscribe db Subscription
      { subId = "sub-2", sessionId = "rs-session-test", resourceType = "jira"
      , resourceId = "PROJ-100", resourceUrl = Nothing
      , createdAt = now, deletedAt = Nothing
      }
    upsertResourceState db "pr" "owner/repo#1" "{\"state\":\"open\"}" now now
    upsertResourceState db "jira" "PROJ-100" "{\"status\":\"In Progress\"}" now now

    results <- listResourceStatesForSession db "rs-session-test"
    length results `shouldBe` 2

  it "TestResourceStateCleanupOnLastUnsubscribe" $ withTestDb $ \db -> do
    seedSession db "cleanup-sess-1"
    seedSession db "cleanup-sess-2"
    let now = "2026-07-06T10:00:00Z"
    subscribe db Subscription
      { subId = "sub-a", sessionId = "cleanup-sess-1", resourceType = "pr"
      , resourceId = "owner/repo#1", resourceUrl = Nothing
      , createdAt = now, deletedAt = Nothing
      }
    subscribe db Subscription
      { subId = "sub-b", sessionId = "cleanup-sess-2", resourceType = "pr"
      , resourceId = "owner/repo#1", resourceUrl = Nothing
      , createdAt = now, deletedAt = Nothing
      }
    upsertResourceState db "pr" "owner/repo#1" "{\"state\":\"open\"}" now now

    -- First unsubscribe: state remains (other session still subscribed)
    unsubscribe db "cleanup-sess-1" "pr" "owner/repo#1"
    mrs1 <- getResourceState db "pr" "owner/repo#1"
    isJust mrs1 `shouldBe` True

    -- Last unsubscribe: state deleted
    unsubscribe db "cleanup-sess-2" "pr" "owner/repo#1"
    mrs2 <- getResourceState db "pr" "owner/repo#1"
    isNothing mrs2 `shouldBe` True
