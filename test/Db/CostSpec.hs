-- | Port of db/cost_test.go.
module Db.CostSpec (spec) where

import Data.Maybe (isNothing)
import qualified Database.SQLite.Simple as SQL
import Test.Hspec

import Handler.Db (Db(..))
import Handler.Db.Cost
  ( CostSnapshot(..), DailyCost(..), DateSummary(..), SessionSummary(..)
  , getCostSnapshot, getDailyCostForSession, getTotalAdjustment
  , insertCostAdjustment, queryDailyCostByDate, queryDailyCostBySession
  , queryTotalCost, upsertCostSnapshot, upsertDailyCost )
import TestUtil (seedSession, withTestDb)

spec :: Spec
spec = do
  it "TestGetCostSnapshotNotFound" $ withTestDb $ \db -> do
    msnap <- getCostSnapshot db "nonexistent"
    isNothing msnap `shouldBe` True

  it "TestUpsertAndGetCostSnapshot" $ withTestDb $ \db -> do
    seedSession db "cost-test-1"
    upsertCostSnapshot db CostSnapshot
      { sessionId = "cost-test-1"
      , reportedCostUsd = 12.50
      , totalInputTokens = 100000
      , totalOutputTokens = 5000
      , model = Just "claude-opus-4-6[1m]"
      , updatedAt = "2026-07-16T10:00:00Z"
      }
    mgot <- getCostSnapshot db "cost-test-1"
    case mgot of
      Nothing -> expectationFailure "expected snapshot"
      Just got -> do
        got.reportedCostUsd `shouldBe` 12.50
        got.totalInputTokens `shouldBe` 100000
        got.model `shouldBe` Just "claude-opus-4-6[1m]"

  it "TestUpsertCostSnapshotOverwrites" $ withTestDb $ \db -> do
    seedSession db "cost-test-2"
    upsertCostSnapshot db CostSnapshot
      { sessionId = "cost-test-2", reportedCostUsd = 5.00
      , totalInputTokens = 50000, totalOutputTokens = 2000
      , model = Just "claude-opus-4-6[1m]", updatedAt = "2026-07-16T10:00:00Z"
      }
    upsertCostSnapshot db CostSnapshot
      { sessionId = "cost-test-2", reportedCostUsd = 10.00
      , totalInputTokens = 100000, totalOutputTokens = 4000
      , model = Just "claude-opus-4-6[1m]", updatedAt = "2026-07-16T10:05:00Z"
      }
    mgot <- getCostSnapshot db "cost-test-2"
    fmap (.reportedCostUsd) mgot `shouldBe` Just 10.00

  it "TestInsertCostAdjustmentAndGetTotal" $ withTestDb $ \db -> do
    seedSession db "adj-test"
    insertCostAdjustment db "adj-test" 25.00 "restart_reset" "2026-07-16T10:00:00Z"
    insertCostAdjustment db "adj-test" 15.00 "restart_reset" "2026-07-16T14:00:00Z"
    total <- getTotalAdjustment db "adj-test"
    total `shouldBe` 40.00

  it "TestGetTotalAdjustmentNoRows" $ withTestDb $ \db -> do
    total <- getTotalAdjustment db "nonexistent"
    total `shouldBe` 0

  it "TestUpsertDailyCostAccumulates" $ withTestDb $ \db -> do
    seedSession db "daily-test"
    upsertDailyCost db "daily-test" "2026-07-16" 5.00 50000 2000
    upsertDailyCost db "daily-test" "2026-07-16" 3.00 30000 1000
    mdc <- getDailyCostForSession db "daily-test" "2026-07-16"
    case mdc of
      Nothing -> expectationFailure "expected daily cost"
      Just dc -> do
        dc.costUsd `shouldBe` 8.00
        dc.inputTokens `shouldBe` 80000
        dc.outputTokens `shouldBe` 3000

  it "TestQueryDailyCostByDate" $ withTestDb $ \db -> do
    seedSession db "date-q-1"
    seedSession db "date-q-2"
    upsertDailyCost db "date-q-1" "2026-07-15" 10.00 100000 5000
    upsertDailyCost db "date-q-2" "2026-07-15" 8.00 80000 4000
    upsertDailyCost db "date-q-1" "2026-07-16" 12.00 120000 6000
    results <- queryDailyCostByDate db "2026-07-15" "2026-07-16"
    case results of
      [first', second'] -> do
        -- Results ordered DESC, so Jul 16 first
        first'.date `shouldBe` "2026-07-16"
        first'.costUsd `shouldBe` 12.00
        second'.sessionCount `shouldBe` 2
      _ -> expectationFailure ("expected 2 dates, got " <> show (length results))

  it "TestQueryDailyCostBySession" $ withTestDb $ \db -> do
    seedSession db "sess-q-1"
    seedSession db "sess-q-2"
    SQL.execute_ db.conn
      "UPDATE sessions SET session_name = 'my-session' WHERE session_id = 'sess-q-1'"
    upsertDailyCost db "sess-q-1" "2026-07-15" 10.00 100000 5000
    upsertDailyCost db "sess-q-1" "2026-07-16" 12.00 120000 6000
    upsertDailyCost db "sess-q-2" "2026-07-16" 8.00 80000 4000
    results <- queryDailyCostBySession db "2026-07-15" "2026-07-16"
    case results of
      [top, _] -> do
        -- Ordered by cost DESC, so sess-q-1 (22.00) first
        top.sessionName `shouldBe` "my-session"
        top.costUsd `shouldBe` 22.00
      _ -> expectationFailure ("expected 2 sessions, got " <> show (length results))

  it "TestQueryTotalCost" $ withTestDb $ \db -> do
    seedSession db "total-q-1"
    seedSession db "total-q-2"
    upsertDailyCost db "total-q-1" "2026-07-15" 10.00 100000 5000
    upsertDailyCost db "total-q-2" "2026-07-16" 8.00 80000 4000
    (cost, input, output) <- queryTotalCost db "2026-07-15" "2026-07-16"
    cost `shouldBe` 18.00
    input `shouldBe` 180000
    output `shouldBe` 9000

  it "TestQueryTotalCostEmpty" $ withTestDb $ \db -> do
    (cost, input, output) <- queryTotalCost db "2026-01-01" "2026-01-31"
    (cost, input, output) `shouldBe` (0, 0, 0)
