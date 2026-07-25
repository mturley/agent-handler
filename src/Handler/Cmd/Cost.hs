-- | Port of cmd/cost.go: API cost breakdown.
module Handler.Cmd.Cost (costCommand) where

import Control.Monad (forM_, unless, when)
import qualified Data.Aeson as A
import Data.Aeson (object, (.=))
import qualified Data.ByteString.Lazy.Char8 as BL
import Data.Text (Text)
import qualified Data.Text as T
import Data.Time.Calendar (fromGregorian, gregorianMonthLength, toGregorian)
import Data.Time.Clock (UTCTime(..), getCurrentTime)
import Data.Time.Format (defaultTimeLocale, formatTime, parseTimeM)
import Options.Applicative
import Text.Printf (printf)

import Handler.Cli.Common
import Handler.Db (Db, close)
import Handler.Db.Cost
import Handler.Db.Sessions (Session(..), getSession)

data CostOpts = CostOpts
  { cMonth   :: Text
  , cToday   :: Bool
  , cSession :: Text
  }

costCommand :: Mod CommandFields NamedCommand
costCommand = mkCommand "cost" "Show API cost breakdown" (runCost <$> opts)
  where
    opts = CostOpts
      <$> strOption (long "month" <> value "" <> help "month to show (YYYY-MM format, default: current month)")
      <*> switch (long "today" <> help "show today's cost only")
      <*> strOption (long "session" <> value "" <> help "show cost for a specific session")

runCost :: CostOpts -> Ctx -> IO ()
runCost o ctx = do
  db <- openReadOnlyDb
  if | o.cSession /= "" -> runCostSession db o.cSession ctx
     | o.cToday -> runCostToday db ctx
     | otherwise -> runCostMonth db o.cMonth ctx
  close db

-- Go marshals these structs without tags, so JSON keys are the field names.
dateSummaryJson :: DateSummary -> A.Value
dateSummaryJson d = object
  [ "Date" .= d.date, "CostUSD" .= d.costUsd, "SessionCount" .= d.sessionCount ]

sessionSummaryJson :: SessionSummary -> A.Value
sessionSummaryJson s = object
  [ "SessionID" .= s.sessionId, "SessionName" .= s.sessionName
  , "CostUSD" .= s.costUsd, "InputTokens" .= s.inputTokens, "OutputTokens" .= s.outputTokens ]

-- | Compact single-line JSON, matching Go's json.NewEncoder(...).Encode.
printJsonCompact :: A.Value -> IO ()
printJsonCompact = BL.putStrLn . A.encode

runCostMonth :: Db -> Text -> Ctx -> IO ()
runCostMonth db monthFlag ctx = do
  now <- getCurrentTime
  (year, month) <-
    if monthFlag == ""
      then let (y, m, _) = toGregorian (utctDay now) in pure (y, m)
      else case parseTimeM True defaultTimeLocale "%Y-%m" (T.unpack monthFlag) :: Maybe UTCTime of
        Just t -> let (y, m, _) = toGregorian (utctDay t) in pure (y, m)
        Nothing -> dieText "invalid --month format, use YYYY-MM"

  let pad2 n = T.pack (printf "%02d" (n :: Int))
      startDate = T.pack (printf "%04d" year) <> "-" <> pad2 month <> "-01"
      endDate = T.pack (printf "%04d" year) <> "-" <> pad2 month <> "-" <> pad2 (gregorianMonthLength year month)

  (totalCost, totalInput, totalOutput) <- queryTotalCost db startDate endDate
  days <- queryDailyCostByDate db startDate endDate
  sessions <- queryDailyCostBySession db startDate endDate

  let today = T.pack (formatTime defaultTimeLocale "%Y-%m-%d" now)
  (todayCost, _, _) <- queryTotalCost db today today

  -- Last month via day arithmetic on the first of this month
  let (ly, lm) = if month == 1 then (year - 1, 12) else (year, month - 1)
      lastMonthStart = T.pack (printf "%04d" ly) <> "-" <> pad2 lm <> "-01"
      lastMonthEnd = T.pack (printf "%04d" ly) <> "-" <> pad2 lm <> "-" <> pad2 (gregorianMonthLength ly lm)
  (lastMonthCost, _, _) <- queryTotalCost db lastMonthStart lastMonthEnd
  (allTimeCost, _, _) <- queryTotalCost db "2000-01-01" "2099-12-31"

  if ctx.jsonOutput
    then printJsonCompact $ object
      [ "period" .= (T.pack (printf "%04d" year) <> "-" <> pad2 month)
      , "total_cost" .= totalCost
      , "input_tokens" .= totalInput
      , "output_tokens" .= totalOutput
      , "today_cost" .= todayCost
      , "last_month_cost" .= lastMonthCost
      , "all_time_cost" .= allTimeCost
      , "by_day" .= map dateSummaryJson days
      , "by_session" .= map sessionSummaryJson sessions
      ]
    else do
      let monthName y m = formatTime defaultTimeLocale "%B" (fromGregorian y m 1)
      putStrLn (printf "Today: $%.2f | This month: $%.2f | %s: $%.2f | All time: $%.2f\n"
                  todayCost totalCost (monthName ly lm) lastMonthCost allTimeCost)
      putStrLn (printf "%s %d: $%.2f" (monthName year month) year totalCost)
      putStrLn (printf "  %d sessions | %s input tokens | %s output tokens\n"
                  (length sessions) (formatTokens totalInput) (formatTokens totalOutput))

      unless (null days) $ do
        putTextLn "  By day:"
        forM_ days $ \day -> do
          let formatted = case parseTimeM True defaultTimeLocale "%Y-%m-%d" (T.unpack day.date) :: Maybe UTCTime of
                Just t -> formatTime defaultTimeLocale "%b %d" t
                Nothing -> T.unpack day.date
          putStrLn (printf "    %s  $%.2f  (%d sessions)" formatted day.costUsd day.sessionCount)
        putTextLn ""

      unless (null sessions) $ do
        putTextLn "  Top sessions:"
        forM_ (take 10 sessions) $ \s ->
          putStrLn (printf "    %-30s $%.2f" (T.unpack (displayName s)) s.costUsd)

runCostToday :: Db -> Ctx -> IO ()
runCostToday db ctx = do
  now <- getCurrentTime
  let today = T.pack (formatTime defaultTimeLocale "%Y-%m-%d" now)
  (totalCost, totalInput, totalOutput) <- queryTotalCost db today today
  sessions <- queryDailyCostBySession db today today

  if ctx.jsonOutput
    then printJsonCompact $ object
      [ "date" .= today
      , "total_cost" .= totalCost
      , "input_tokens" .= totalInput
      , "output_tokens" .= totalOutput
      , "by_session" .= map sessionSummaryJson sessions
      ]
    else do
      putStrLn (printf "Today (%s): $%.2f" (formatTime defaultTimeLocale "%b %d" now) totalCost)
      putStrLn (printf "  %d sessions | %s input tokens | %s output tokens\n"
                  (length sessions) (formatTokens totalInput) (formatTokens totalOutput))
      unless (null sessions) $ do
        putTextLn "  By session:"
        forM_ sessions $ \s ->
          putStrLn (printf "    %-30s $%.2f" (T.unpack (displayName s)) s.costUsd)

runCostSession :: Db -> Text -> Ctx -> IO ()
runCostSession db sessionId ctx = do
  msession <- getSession db sessionId
  case msession of
    Nothing -> dieText ("session not found: " <> sessionId)
    Just session -> do
      msnap <- getCostSnapshot db sessionId
      adjustment <- getTotalAdjustment db sessionId
      now <- getCurrentTime
      let today = T.pack (formatTime defaultTimeLocale "%Y-%m-%d" now)
      mtodayCost <- getDailyCostForSession db sessionId today

      if ctx.jsonOutput
        then printJsonCompact $ object $
          [ "session_id" .= sessionId
          , "session_name" .= session.sessionName
          , "adjustment" .= adjustment
          ]
          ++ (case msnap of
                Just snap ->
                  [ "reported_cost" .= snap.reportedCostUsd
                  , "true_cost" .= (snap.reportedCostUsd + adjustment)
                  , "model" .= snap.model
                  ]
                Nothing -> [])
          ++ (case mtodayCost of
                Just tc -> [ "today_cost" .= tc.costUsd ]
                Nothing -> [])
        else do
          let name = if session.sessionName == "" then T.take 8 sessionId else session.sessionName
          printfT ["Session: ", name]
          case msnap of
            Just snap -> do
              putStrLn (printf "  True cost:     $%.2f" (snap.reportedCostUsd + adjustment))
              putStrLn (printf "  Reported cost: $%.2f" snap.reportedCostUsd)
              when (adjustment > 0) $
                putStrLn (printf "  Adjustments:   $%.2f (restart resets)" adjustment)
              printfT ["  Model:         ", maybe "" id snap.model]
            Nothing -> putTextLn "  No cost data recorded yet"
          forM_ mtodayCost $ \tc ->
            putStrLn (printf "  Today:         $%.2f" tc.costUsd)

displayName :: SessionSummary -> Text
displayName s = if s.sessionName == "" then T.take 8 s.sessionId else s.sessionName

formatTokens :: Int -> String
formatTokens n
  | n >= 1_000_000 = printf "%.1fM" (fromIntegral n / 1_000_000 :: Double)
  | n >= 1_000 = printf "%.0fK" (fromIntegral n / 1_000 :: Double)
  | otherwise = show n
