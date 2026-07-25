-- | Port of db/peek_test.go.
module Db.PeekSpec (spec) where

import Data.Maybe (isJust, isNothing)
import Test.Hspec

import Handler.Db.Peek
  ( PeekState(..), deletePeekStatesForSessions, getPeekState, listPeekStates
  , peekStatesAgeSeconds, upsertPeekState )
import Handler.Util (nowIso)
import TestUtil (seedSession, withTestDb)

spec :: Spec
spec = do
  it "TestUpsertAndGetPeekState" $ withTestDb $ \db -> do
    seedSession db "peek-test"
    now <- nowIso
    upsertPeekState db "peek-test" "terminal content here" True "awaiting approval" now
    mps <- getPeekState db "peek-test"
    case mps of
      Nothing -> expectationFailure "expected non-Nothing PeekState"
      Just ps -> do
        ps.content `shouldBe` "terminal content here"
        ps.needsInput `shouldBe` True
        ps.reason `shouldBe` "awaiting approval"

  it "TestUpsertPeekStateOverwrites" $ withTestDb $ \db -> do
    seedSession db "peek-overwrite"
    now <- nowIso
    upsertPeekState db "peek-overwrite" "old content" True "awaiting approval" now
    upsertPeekState db "peek-overwrite" "new content" False "" now
    mps <- getPeekState db "peek-overwrite"
    fmap (.content) mps `shouldBe` Just "new content"
    fmap (.needsInput) mps `shouldBe` Just False

  it "TestGetPeekStateNotFound" $ withTestDb $ \db -> do
    mps <- getPeekState db "nonexistent"
    isNothing mps `shouldBe` True

  it "TestListPeekStates" $ withTestDb $ \db -> do
    seedSession db "peek-list-1"
    seedSession db "peek-list-2"
    now <- nowIso
    upsertPeekState db "peek-list-1" "content 1" True "awaiting approval" now
    upsertPeekState db "peek-list-2" "content 2" False "" now
    states <- listPeekStates db
    length states `shouldBe` 2

  it "TestPeekStatesAge" $ withTestDb $ \db -> do
    seedSession db "peek-age"
    -- With no rows, age should be very large
    age0 <- peekStatesAgeSeconds db
    age0 `shouldSatisfy` (>= 3600)
    -- Fresh row → small age
    now <- nowIso
    upsertPeekState db "peek-age" "content" False "" now
    age1 <- peekStatesAgeSeconds db
    age1 `shouldSatisfy` (<= 2)

  it "TestDeletePeekStatesForSessions" $ withTestDb $ \db -> do
    seedSession db "peek-del-1"
    seedSession db "peek-del-2"
    now <- nowIso
    upsertPeekState db "peek-del-1" "c1" False "" now
    upsertPeekState db "peek-del-2" "c2" False "" now
    deletePeekStatesForSessions db ["peek-del-1"]
    mps1 <- getPeekState db "peek-del-1"
    isNothing mps1 `shouldBe` True
    mps2 <- getPeekState db "peek-del-2"
    isJust mps2 `shouldBe` True
