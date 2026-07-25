-- | Port of db/db_test.go.
module Db.DbSpec (spec) where

import Control.Monad (forM_)
import Data.Text (Text)
import qualified Database.SQLite.Simple as SQL
import System.FilePath ((</>))
import System.IO.Temp (withSystemTempDirectory)
import Test.Hspec

import Handler.Db (Db(..), close, open)

expectedTables :: [Text]
expectedTables =
  [ "events"
  , "event_recipients"
  , "event_resources"
  , "sessions"
  , "session_cursors"
  , "subscriptions"
  , "resource_relationships"
  , "resource_state"
  , "cost_snapshots"
  , "cost_adjustments"
  , "daily_cost"
  ]

spec :: Spec
spec = do
  it "TestOpen: sets WAL mode and creates all tables" $
    withSystemTempDirectory "handler-test" $ \dir -> do
      db <- open (dir </> "test.db")
      modes <- SQL.query_ db.conn "PRAGMA journal_mode" :: IO [SQL.Only Text]
      case modes of
        (SQL.Only mode : _) -> mode `shouldBe` "wal"
        [] -> expectationFailure "no journal_mode row"
      forM_ expectedTables $ \table -> do
        counts <- SQL.query db.conn
          "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?"
          (SQL.Only table) :: IO [SQL.Only Int]
        (table, counts) `shouldBe` (table, [SQL.Only 1])
      close db

  it "TestOpenIdempotent: schema survives a second open" $
    withSystemTempDirectory "handler-test" $ \dir -> do
      db1 <- open (dir </> "test.db")
      close db1
      db2 <- open (dir </> "test.db")
      counts <- SQL.query_ db2.conn
        "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='events'"
        :: IO [SQL.Only Int]
      counts `shouldBe` [SQL.Only 1]
      close db2
