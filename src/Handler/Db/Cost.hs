-- | Port of db/cost.go: cost snapshots, adjustments, and daily rollups.
module Handler.Db.Cost
  ( CostSnapshot(..)
  , DailyCost(..)
  , DateSummary(..)
  , SessionSummary(..)
  , getCostSnapshot
  , upsertCostSnapshot
  , insertCostAdjustment
  , getTotalAdjustment
  , upsertDailyCost
  , getDailyCostForSession
  , queryDailyCostByDate
  , queryDailyCostBySession
  , queryTotalCost
  ) where

import Data.Maybe (fromMaybe)
import Data.Text (Text)
import qualified Database.SQLite.Simple as SQL

import Handler.Db (Db(..))

data CostSnapshot = CostSnapshot
  { sessionId         :: Text
  , reportedCostUsd   :: Double
  , totalInputTokens  :: Int
  , totalOutputTokens :: Int
  , model             :: Maybe Text
  , updatedAt         :: Text
  } deriving (Show, Eq)

instance SQL.FromRow CostSnapshot where
  fromRow = CostSnapshot
    <$> SQL.field <*> SQL.field <*> SQL.field
    <*> SQL.field <*> SQL.field <*> SQL.field

data DailyCost = DailyCost
  { sessionId    :: Text
  , date         :: Text
  , costUsd      :: Double
  , inputTokens  :: Int
  , outputTokens :: Int
  } deriving (Show, Eq)

instance SQL.FromRow DailyCost where
  fromRow = DailyCost
    <$> SQL.field <*> SQL.field <*> SQL.field <*> SQL.field <*> SQL.field

data DateSummary = DateSummary
  { date         :: Text
  , costUsd      :: Double
  , sessionCount :: Int
  } deriving (Show, Eq)

instance SQL.FromRow DateSummary where
  fromRow = DateSummary <$> SQL.field <*> SQL.field <*> SQL.field

data SessionSummary = SessionSummary
  { sessionId    :: Text
  , sessionName  :: Text
  , costUsd      :: Double
  , inputTokens  :: Int
  , outputTokens :: Int
  } deriving (Show, Eq)

instance SQL.FromRow SessionSummary where
  fromRow = SessionSummary
    <$> SQL.field <*> SQL.field <*> SQL.field <*> SQL.field <*> SQL.field

getCostSnapshot :: Db -> Text -> IO (Maybe CostSnapshot)
getCostSnapshot db sid = do
  rows <- SQL.query db.conn
    "SELECT session_id, reported_cost_usd, total_input_tokens, total_output_tokens, model, updated_at\
    \ FROM cost_snapshots WHERE session_id = ?"
    (SQL.Only sid)
  pure $ case rows of { (r : _) -> Just r; [] -> Nothing }

upsertCostSnapshot :: Db -> CostSnapshot -> IO ()
upsertCostSnapshot db s =
  SQL.execute db.conn
    "INSERT INTO cost_snapshots (session_id, reported_cost_usd, total_input_tokens, total_output_tokens, model, updated_at)\
    \ VALUES (?, ?, ?, ?, ?, ?)\
    \ ON CONFLICT(session_id) DO UPDATE SET\
    \  reported_cost_usd = excluded.reported_cost_usd,\
    \  total_input_tokens = excluded.total_input_tokens,\
    \  total_output_tokens = excluded.total_output_tokens,\
    \  model = excluded.model,\
    \  updated_at = excluded.updated_at"
    ( s.sessionId, s.reportedCostUsd, s.totalInputTokens
    , s.totalOutputTokens, s.model, s.updatedAt )

insertCostAdjustment :: Db -> Text -> Double -> Text -> Text -> IO ()
insertCostAdjustment db sid adjustmentUsd reason createdAt =
  SQL.execute db.conn
    "INSERT INTO cost_adjustments (session_id, adjustment_usd, reason, created_at) VALUES (?, ?, ?, ?)"
    (sid, adjustmentUsd, reason, createdAt)

getTotalAdjustment :: Db -> Text -> IO Double
getTotalAdjustment db sid = do
  rows <- SQL.query db.conn
    "SELECT SUM(adjustment_usd) FROM cost_adjustments WHERE session_id = ?"
    (SQL.Only sid)
  pure $ case rows of
    (SQL.Only m : _) -> fromMaybe 0 m
    []               -> 0

-- | Adds deltas into the (session, date) daily rollup row.
upsertDailyCost :: Db -> Text -> Text -> Double -> Int -> Int -> IO ()
upsertDailyCost db sid date costDelta inputDelta outputDelta =
  SQL.execute db.conn
    "INSERT INTO daily_cost (session_id, date, cost_usd, input_tokens, output_tokens)\
    \ VALUES (?, ?, ?, ?, ?)\
    \ ON CONFLICT(session_id, date) DO UPDATE SET\
    \  cost_usd = daily_cost.cost_usd + excluded.cost_usd,\
    \  input_tokens = daily_cost.input_tokens + excluded.input_tokens,\
    \  output_tokens = daily_cost.output_tokens + excluded.output_tokens"
    (sid, date, costDelta, inputDelta, outputDelta)

getDailyCostForSession :: Db -> Text -> Text -> IO (Maybe DailyCost)
getDailyCostForSession db sid date = do
  rows <- SQL.query db.conn
    "SELECT session_id, date, cost_usd, input_tokens, output_tokens\
    \ FROM daily_cost WHERE session_id = ? AND date = ?"
    (sid, date)
  pure $ case rows of { (r : _) -> Just r; [] -> Nothing }

queryDailyCostByDate :: Db -> Text -> Text -> IO [DateSummary]
queryDailyCostByDate db startDate endDate =
  SQL.query db.conn
    "SELECT date, SUM(cost_usd), COUNT(DISTINCT session_id)\
    \ FROM daily_cost WHERE date >= ? AND date <= ?\
    \ GROUP BY date ORDER BY date DESC"
    (startDate, endDate)

queryDailyCostBySession :: Db -> Text -> Text -> IO [SessionSummary]
queryDailyCostBySession db startDate endDate =
  SQL.query db.conn
    "SELECT dc.session_id, COALESCE(s.session_name, ''), SUM(dc.cost_usd), SUM(dc.input_tokens), SUM(dc.output_tokens)\
    \ FROM daily_cost dc\
    \ LEFT JOIN sessions s ON s.session_id = dc.session_id\
    \ WHERE dc.date >= ? AND dc.date <= ?\
    \ GROUP BY dc.session_id ORDER BY SUM(dc.cost_usd) DESC"
    (startDate, endDate)

-- | Total (cost, input tokens, output tokens) over a date range.
queryTotalCost :: Db -> Text -> Text -> IO (Double, Int, Int)
queryTotalCost db startDate endDate = do
  rows <- SQL.query db.conn
    "SELECT SUM(cost_usd), SUM(input_tokens), SUM(output_tokens)\
    \ FROM daily_cost WHERE date >= ? AND date <= ?"
    (startDate, endDate)
  pure $ case rows of
    ((c, it, ot) : _) -> (fromMaybe 0 c, fromMaybe 0 it, fromMaybe 0 ot)
    []                -> (0, 0, 0)
