{-# LANGUAGE TemplateHaskell #-}
-- | SQLite connection management for agent-handler.
-- Port of db/db.go: open with WAL + busy_timeout, apply embedded schema.
module Handler.Db
  ( Db(..)
  , open
  , openReadOnly
  , close
  , withDb
  , handlerHome
  , defaultPath
  ) where

import Control.Exception (bracket)
import Control.Monad (forM_, unless)
import Data.FileEmbed (embedFile)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.Encoding as TE
import qualified Database.SQLite.Simple as SQL
import System.Directory (createDirectoryIfMissing, getHomeDirectory)
import System.Environment (lookupEnv)
import System.FilePath (takeDirectory, (</>))

schemaDDL :: Text
schemaDDL = TE.decodeUtf8 $(embedFile "db/schema.sql")

-- | Wraps a SQLite database connection for agent-handler.
newtype Db = Db { conn :: SQL.Connection }

-- | Creates or opens the SQLite database at the given path.
-- Creates parent directories if needed, applies WAL mode, and runs the schema.
open :: FilePath -> IO Db
open path = do
  createDirectoryIfMissing True (takeDirectory path)
  c <- SQL.open path
  SQL.execute_ c "PRAGMA journal_mode=WAL"
  SQL.execute_ c "PRAGMA busy_timeout=3000"
  applySchema c
  pure (Db c)

-- | Executes each statement of the embedded schema.
-- sqlite-simple runs one statement per execute_, so split on ";".
applySchema :: SQL.Connection -> IO ()
applySchema c =
  forM_ (T.splitOn ";" schemaDDL) $ \stmt ->
    unless (T.null (T.strip stmt)) $
      SQL.execute_ c (SQL.Query stmt)

-- | Opens the database without creating it or applying the schema.
-- The database must already exist. (Go used mode=ro; sqlite-simple has no
-- URI-mode open, so this opens normally but skips setup.)
openReadOnly :: FilePath -> IO Db
openReadOnly path = Db <$> SQL.open path

close :: Db -> IO ()
close (Db c) = SQL.close c

withDb :: FilePath -> (Db -> IO a) -> IO a
withDb path = bracket (open path) close

-- | The agent-handler home directory.
-- Respects HANDLER_HOME env var, defaults to ~/.agent-handler.
handlerHome :: IO FilePath
handlerHome = do
  env <- lookupEnv "HANDLER_HOME"
  case env of
    Just dir | not (null dir) -> pure dir
    _ -> do
      home <- getHomeDirectory
      pure (home </> ".agent-handler")

-- | The default database path: $HANDLER_HOME/data/handler.db
defaultPath :: IO FilePath
defaultPath = do
  home <- handlerHome
  pure (home </> "data" </> "handler.db")
