-- | Port of db/cursors_test.go.
module Db.CursorsSpec (spec) where

import Test.Hspec

import Handler.Db.Cursors
  ( advanceBothCursors, advanceCursor, autoDeliveredCount, catchUpHumanCursor
  , clearHumanCursor, getCursor )
import Handler.Db.Events (Event(..), insertEvent)
import TestUtil (seedSession, withTestDb)

broadcastEvent :: Event
broadcastEvent = Event
  { eventId = "evt-auto-1"
  , ts = "2026-06-15T10:05:00Z"
  , externalTs = Nothing
  , source = "test"
  , sessionId = Nothing
  , eventType = "message"
  , title = "test event"
  , body = Nothing
  , author = Nothing
  , authorType = Nothing
  , broadcast = True
  , tags = Nothing
  }

spec :: Spec
spec = do
  it "TestDualCursors: agent/human cursor interplay" $ withTestDb $ \db -> do
    seedSession db "dual-cursor-test"

    advanceBothCursors db "dual-cursor-test" "2026-06-15T10:00:00Z"
    agent <- getCursor db "dual-cursor-test"
    agent `shouldBe` "2026-06-15T10:00:00Z"

    count0 <- autoDeliveredCount db "dual-cursor-test"
    count0 `shouldBe` 0

    insertEvent db broadcastEvent [] []

    -- Advance only agent cursor (simulating auto ack)
    advanceCursor db "dual-cursor-test" "2026-06-15T10:10:00Z"
    count1 <- autoDeliveredCount db "dual-cursor-test"
    count1 `shouldBe` 1

    catchUpHumanCursor db "dual-cursor-test"
    count2 <- autoDeliveredCount db "dual-cursor-test"
    count2 `shouldBe` 0

    clearHumanCursor db "dual-cursor-test"
    count3 <- autoDeliveredCount db "dual-cursor-test"
    count3 `shouldBe` 0

  it "TestGetAndAdvanceCursor: empty then advancing" $ withTestDb $ \db -> do
    seedSession db "session-cursor-test"

    cursor0 <- getCursor db "session-cursor-test"
    cursor0 `shouldBe` ""

    advanceCursor db "session-cursor-test" "2026-06-15T10:00:00Z"
    cursor1 <- getCursor db "session-cursor-test"
    cursor1 `shouldBe` "2026-06-15T10:00:00Z"

    advanceCursor db "session-cursor-test" "2026-06-15T11:00:00Z"
    cursor2 <- getCursor db "session-cursor-test"
    cursor2 `shouldBe` "2026-06-15T11:00:00Z"
