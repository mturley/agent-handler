-- | Port of db/subscriptions_test.go.
module Db.SubscriptionsSpec (spec) where

import Data.Maybe (isJust, isNothing)
import Data.Text (Text)
import Test.Hspec

import Handler.Db.Subscriptions
  ( Subscription(..), listSubscriptions, reinstate, subscribe, unsubscribe )
import Handler.Util (nowIso)
import TestUtil (seedSession, withTestDb)

mkSub :: Text -> Text -> Text -> Text -> Subscription
mkSub sid sess rType rId = Subscription
  { subId = sid, sessionId = sess, resourceType = rType, resourceId = rId
  , resourceUrl = Nothing, createdAt = "", deletedAt = Nothing
  }

spec :: Spec
spec = do
  it "TestSubscribeAndList: round-trips fields" $ withTestDb $ \db -> do
    seedSession db "session-sub-test"
    now <- nowIso
    subscribe db (mkSub "sub-1" "session-sub-test" "pr" "owner/repo#123")
      { resourceUrl = Just "https://github.com/owner/repo/pull/123"
      , createdAt = now
      }
    subs <- listSubscriptions db "session-sub-test" False
    case subs of
      [s] -> do
        s.subId `shouldBe` "sub-1"
        s.sessionId `shouldBe` "session-sub-test"
        s.resourceType `shouldBe` "pr"
        s.resourceId `shouldBe` "owner/repo#123"
        s.resourceUrl `shouldBe` Just "https://github.com/owner/repo/pull/123"
        s.deletedAt `shouldBe` Nothing
      _ -> expectationFailure ("expected 1 subscription, got " <> show (length subs))

  it "TestUnsubscribeSoftDeletes" $ withTestDb $ \db -> do
    seedSession db "session-unsub-test"
    now <- nowIso
    subscribe db (mkSub "sub-2" "session-unsub-test" "pr" "owner/repo#456") { createdAt = now }
    unsubscribe db "session-unsub-test" "pr" "owner/repo#456"

    activeSubs <- listSubscriptions db "session-unsub-test" False
    activeSubs `shouldBe` []

    allSubs <- listSubscriptions db "session-unsub-test" True
    length allSubs `shouldBe` 1
    isJust (head allSubs).deletedAt `shouldBe` True

  it "TestReinstateSubscription" $ withTestDb $ \db -> do
    seedSession db "session-reinstate-test"
    now <- nowIso
    subscribe db (mkSub "sub-3" "session-reinstate-test" "pr" "owner/repo#789") { createdAt = now }
    unsubscribe db "session-reinstate-test" "pr" "owner/repo#789"
    reinstate db "session-reinstate-test" "pr" "owner/repo#789"

    subs <- listSubscriptions db "session-reinstate-test" False
    length subs `shouldBe` 1
    isNothing (head subs).deletedAt `shouldBe` True

  it "TestSubscribeDeduplicate: keeps the first subscription" $ withTestDb $ \db -> do
    seedSession db "session-dedup-test"
    now <- nowIso
    subscribe db (mkSub "sub-4" "session-dedup-test" "pr" "owner/repo#100") { createdAt = now }
    subscribe db (mkSub "sub-5" "session-dedup-test" "pr" "owner/repo#100") { createdAt = now }

    subs <- listSubscriptions db "session-dedup-test" False
    map (.subId) subs `shouldBe` ["sub-4"]
