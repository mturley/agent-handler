-- | Port of db/resources_test.go.
module Db.ResourcesSpec (spec) where

import Data.Text (Text)
import Test.Hspec

import Handler.Db.Events (Event(..), EventResource(..), insertEvent)
import Handler.Db.Resources (ResourceRelationship(..), findRelatedSessions, linkResources, resourceHistory)
import Handler.Db.Sessions (Session(..))
import Handler.Db.Subscriptions (Subscription(..), subscribe)
import Handler.Util (nowIso)
import TestUtil (seedSession, withTestDb)

epicChild :: Text -> Text -> Text -> Text -> ResourceRelationship
epicChild relId childId childUrl now = ResourceRelationship
  { relId = relId
  , childType = "jira"
  , childId = childId
  , childUrl = Just childUrl
  , parentType = "jira"
  , parentId = "RHOAIENG-50"
  , parentUrl = Just "https://redhat.atlassian.net/browse/RHOAIENG-50"
  , relationship = "epic_child"
  , source = "jira"
  , createdAt = now
  }

spec :: Spec
spec = do
  it "TestLinkAndFindRelated: sessions related via shared epic parent" $ withTestDb $ \db -> do
    now <- nowIso
    seedSession db "s1"
    seedSession db "s2"

    subscribe db Subscription
      { subId = "sub-1", sessionId = "s1", resourceType = "jira"
      , resourceId = "RHOAIENG-100"
      , resourceUrl = Just "https://redhat.atlassian.net/browse/RHOAIENG-100"
      , createdAt = now, deletedAt = Nothing
      }
    subscribe db Subscription
      { subId = "sub-2", sessionId = "s2", resourceType = "jira"
      , resourceId = "RHOAIENG-101"
      , resourceUrl = Just "https://redhat.atlassian.net/browse/RHOAIENG-101"
      , createdAt = now, deletedAt = Nothing
      }

    -- Before linking, no relations
    before <- findRelatedSessions db "s1"
    before `shouldBe` []

    linkResources db (epicChild "rel-1" "RHOAIENG-100" "https://redhat.atlassian.net/browse/RHOAIENG-100" now)
    linkResources db (epicChild "rel-2" "RHOAIENG-101" "https://redhat.atlassian.net/browse/RHOAIENG-101" now)

    related1 <- findRelatedSessions db "s1"
    map (.sessionId) related1 `shouldBe` ["s2"]

    related2 <- findRelatedSessions db "s2"
    map (.sessionId) related2 `shouldBe` ["s1"]

  it "TestResourceHistory: events referencing a resource" $ withTestDb $ \db -> do
    now <- nowIso
    seedSession db "s1"
    subscribe db Subscription
      { subId = "sub-pr", sessionId = "s1", resourceType = "github_pr"
      , resourceId = "owner/repo#123"
      , resourceUrl = Just "https://github.com/owner/repo/pull/123"
      , createdAt = now, deletedAt = Nothing
      }
    insertEvent db
      Event
        { eventId = "event-1", ts = now, externalTs = Nothing
        , source = "github", sessionId = Nothing, eventType = "pr_comment"
        , title = "New comment on PR #123", body = Nothing
        , author = Nothing, authorType = Nothing, broadcast = False, tags = Nothing
        }
      []
      [ EventResource "github_pr" "owner/repo#123"
          (Just "https://github.com/owner/repo/pull/123") ]

    history <- resourceHistory db "github_pr" "owner/repo#123" 10
    map (.eventId) history `shouldBe` ["event-1"]
    map (.title) history `shouldBe` ["New comment on PR #123"]

    -- limit=0 means no limit
    historyAll <- resourceHistory db "github_pr" "owner/repo#123" 0
    length historyAll `shouldBe` 1
