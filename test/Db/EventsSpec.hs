-- | Port of db/events_test.go.
module Db.EventsSpec (spec) where

import Data.Text (Text)
import qualified Data.Text as T
import Data.Time.Clock (addUTCTime, getCurrentTime)
import Data.Time.Format (defaultTimeLocale, formatTime)
import Test.Hspec

import Handler.Db.Cursors (advanceCursor)
import Handler.Db.Events
  ( Event(..), EventFilter(..), EventRecipient(..), EventResource(..)
  , emptyFilter, insertEvent, queryEvents, unreadForSession )
import Handler.Db.Subscriptions (Subscription(..), subscribe)
import Handler.Util (nowIso)
import TestUtil (seedSession, withTestDb)

-- | RFC3339 timestamp offset from now by the given seconds.
isoOffset :: Double -> IO Text
isoOffset secs = do
  now <- getCurrentTime
  pure $ T.pack $ formatTime defaultTimeLocale "%Y-%m-%dT%H:%M:%SZ"
    (addUTCTime (realToFrac secs) now)

baseEvent :: Text -> Text -> Event
baseEvent eid ts = Event
  { eventId = eid, ts = ts, externalTs = Nothing, source = "system"
  , sessionId = Nothing, eventType = "announcement", title = "event"
  , body = Nothing, author = Nothing, authorType = Nothing
  , broadcast = False, tags = Nothing
  }

spec :: Spec
spec = do
  it "TestInsertAndQueryEvents: round-trips every field" $ withTestDb $ \db -> do
    seedSession db "session-event-test"
    now <- nowIso
    let e = Event
          { eventId = "event-1"
          , ts = now
          , externalTs = Just "2026-06-15T10:00:00Z"
          , source = "github"
          , sessionId = Just "session-event-test"
          , eventType = "pr_opened"
          , title = "New PR opened"
          , body = Just "PR body text"
          , author = Just "alice"
          , authorType = Just "user"
          , broadcast = False
          , tags = Just "tag1,tag2"
          }
        recipients = [EventRecipient "session" "session-event-test"]
        resources =
          [ EventResource "pr" "owner/repo#123"
              (Just "https://github.com/owner/repo/pull/123") ]
    insertEvent db e recipients resources

    events <- queryEvents db emptyFilter { filterSessionId = Just "session-event-test" }
    case events of
      [got] -> got `shouldBe` e
      _ -> expectationFailure ("expected 1 event, got " <> show (length events))

  it "TestUnreadViaResourceSubscription" $ withTestDb $ \db -> do
    seedSession db "session-unread-sub"
    now <- nowIso
    subscribe db Subscription
      { subId = "sub-unread-1", sessionId = "session-unread-sub"
      , resourceType = "pr", resourceId = "owner/repo#200"
      , resourceUrl = Nothing, createdAt = now, deletedAt = Nothing
      }
    cursorTs <- isoOffset (-3600)
    advanceCursor db "session-unread-sub" cursorTs
    eventTs <- nowIso
    insertEvent db (baseEvent "event-unread-1" eventTs)
      { source = "github", eventType = "pr_comment", title = "New comment on PR" }
      []
      [EventResource "pr" "owner/repo#200" Nothing]

    unread <- unreadForSession db "session-unread-sub"
    map (.eventId) unread `shouldBe` ["event-unread-1"]

  it "TestUnreadViaDirectRecipient" $ withTestDb $ \db -> do
    seedSession db "session-unread-direct"
    cursorTs <- isoOffset (-3600)
    advanceCursor db "session-unread-direct" cursorTs
    eventTs <- nowIso
    insertEvent db (baseEvent "event-unread-2" eventTs)
      { eventType = "notification", title = "Direct notification" }
      [EventRecipient "session" "session-unread-direct"]
      []

    unread <- unreadForSession db "session-unread-direct"
    map (.eventId) unread `shouldBe` ["event-unread-2"]

  it "TestUnreadViaBroadcast" $ withTestDb $ \db -> do
    seedSession db "session-unread-broadcast"
    cursorTs <- isoOffset (-3600)
    advanceCursor db "session-unread-broadcast" cursorTs
    eventTs <- nowIso
    insertEvent db (baseEvent "event-unread-3" eventTs)
      { title = "System announcement", broadcast = True }
      [] []

    unread <- unreadForSession db "session-unread-broadcast"
    map (.eventId) unread `shouldBe` ["event-unread-3"]

  it "TestUnreadExcludesOldEvents" $ withTestDb $ \db -> do
    seedSession db "session-unread-old"
    oldEventTs <- isoOffset (-7200)
    insertEvent db (baseEvent "event-old" oldEventTs)
      { title = "Old announcement", broadcast = True }
      [] []
    -- Cursor is AFTER the event timestamp
    cursorTs <- isoOffset (-3600)
    advanceCursor db "session-unread-old" cursorTs

    unread <- unreadForSession db "session-unread-old"
    unread `shouldBe` []
