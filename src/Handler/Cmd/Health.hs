-- | Port of cmd/health.go: database health and statistics.
module Handler.Cmd.Health (healthCommand) where

import Control.Monad (forM_, when)
import Data.Aeson (object, (.=))
import Data.Map.Strict (Map)
import qualified Data.Map.Strict as Map
import Data.Text (Text)
import qualified Data.Text as T
import qualified Database.SQLite.Simple as SQL
import Options.Applicative
import System.Directory (doesDirectoryExist)
import System.FilePath (takeDirectory, (</>))
import Text.Printf (printf)

import Handler.Cli.Common
import Handler.Db (Db(..), close, defaultPath)
import Handler.Discover (cleanStalePidCaches)

healthCommand :: Mod CommandFields NamedCommand
healthCommand = mkCommand "health" "Show database health and statistics" (pure runHealth)

runHealth :: Ctx -> IO ()
runHealth ctx = do
  db <- openReadOnlyDb

  pageCount <- pragmaInt db "PRAGMA page_count"
  pageSize <- pragmaInt db "PRAGMA page_size"
  let dbSize = pageCount * pageSize

  statusRows <- SQL.query_ db.conn "SELECT status, COUNT(*) FROM sessions GROUP BY status"
  let sessionCounts = Map.fromList (statusRows :: [(Text, Int)])

  totalSubs <- countQuery db "SELECT COUNT(*) FROM subscriptions"
  activeSubs <- countQuery db "SELECT COUNT(*) FROM subscriptions WHERE deleted_at IS NULL"

  dbPath <- defaultPath
  let sessionsDir = takeDirectory dbPath </> "sessions"
  dirExists <- doesDirectoryExist sessionsDir
  staleCleaned <- if dirExists then cleanStalePidCaches sessionsDir else pure 0
  close db

  if ctx.jsonOutput
    then printJson $ object
      [ "db_size_bytes" .= dbSize
      , "db_size_mb" .= (fromIntegral dbSize / (1024 * 1024) :: Double)
      , "session_counts" .= sessionCounts
      , "total_subscriptions" .= totalSubs
      , "active_subscriptions" .= activeSubs
      , "stale_pid_caches_cleaned" .= staleCleaned
      ]
    else do
      putTextLn "Database Health"
      putTextLn "─────────────────────────────"
      putStrLn (printf "DB size: %.2f MB (%d bytes)" (fromIntegral dbSize / (1024 * 1024) :: Double) dbSize)
      putTextLn "\nSessions by status:"
      forM_ (Map.toList sessionCounts) $ \(status, count) ->
        printfT ["  ", status, ": ", T.pack (show count)]
      putTextLn "\nSubscriptions:"
      printfT ["  Active: ", T.pack (show activeSubs)]
      printfT ["  Total (incl. deleted): ", T.pack (show totalSubs)]
      when (staleCleaned > 0) $
        printfT ["\nCleaned ", T.pack (show staleCleaned), " stale PID cache(s)"]
  where
    pragmaInt db q = do
      rows <- SQL.query_ db.conn (SQL.Query q)
      pure $ case rows of { (SQL.Only n : _) -> n; _ -> 0 :: Int }
    countQuery db q = do
      rows <- SQL.query_ db.conn (SQL.Query q)
      pure $ case rows of { (SQL.Only n : _) -> n; _ -> 0 :: Int }
