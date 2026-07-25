-- | Port of cmd/query.go and cmd/schema_cmd.go: read-only SQL access.
module Handler.Cmd.Query (queryCommand, schemaCommand) where

import Control.Monad (forM, forM_)
import Data.Aeson (Value(..), toJSON)
import qualified Data.Aeson.Key as Key
import qualified Data.Aeson.KeyMap as KM
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.Encoding as TE
import qualified Database.SQLite.Simple as SQL
import Database.SQLite.Simple (SQLData(..))
import Options.Applicative

import Handler.Cli.Common
import Handler.Db (Db(..), close)

queryCommand :: Mod CommandFields NamedCommand
queryCommand = mkCommand "query"
  "Execute a read-only SQL query against the agent-handler database"
  (runQuery <$> strArgument (metavar "SQL"))

runQuery :: Text -> Ctx -> IO ()
runQuery sqlQuery ctx = do
  -- Safety check: reject write operations
  let normalized = T.toUpper (T.strip sqlQuery)
      writeOps = ["INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "CREATE", "ATTACH"]
  if any (`T.isPrefixOf` normalized) writeOps
    then dieText "write operations are not allowed in query command"
    else do
      db <- openReadOnlyDb
      SQL.withStatement db.conn (SQL.Query sqlQuery) $ \stmt -> do
        colCount <- SQL.columnCount stmt
        columns <- forM [0 .. colCount - 1] (SQL.columnName stmt)
        rows <- collectRows stmt []
        if ctx.jsonOutput
          then printJson (toJSON (map (rowToObject columns) rows))
          else do
            putTextLn (T.intercalate "\t" columns)
            forM_ rows $ \row ->
              putTextLn (T.intercalate "\t" (map renderCell row))
      close db
  where
    collectRows stmt acc = do
      mrow <- SQL.nextRow stmt :: IO (Maybe [SQLData])
      case mrow of
        Nothing -> pure (reverse acc)
        Just row -> collectRows stmt (row : acc)

    renderCell = \case
      SQLText t    -> t
      SQLInteger i -> T.pack (show i)
      SQLFloat f   -> T.pack (show f)
      SQLBlob b    -> TE.decodeUtf8Lenient b
      SQLNull      -> "NULL"

    rowToObject columns row = Object $ KM.fromList
      [ (Key.fromText col, cellToValue cell) | (col, cell) <- zip columns row ]

    cellToValue = \case
      SQLText t    -> String t
      SQLInteger i -> toJSON i
      SQLFloat f   -> toJSON f
      SQLBlob b    -> String (TE.decodeUtf8Lenient b)
      SQLNull      -> Null

schemaCommand :: Mod CommandFields NamedCommand
schemaCommand = mkCommand "schema"
  "Print the current database schema (DDL statements)"
  (pure runSchema)

runSchema :: Ctx -> IO ()
runSchema _ = do
  db <- openReadOnlyDb
  tables <- SQL.query_ db.conn
    "SELECT sql FROM sqlite_master WHERE type='table' AND sql IS NOT NULL ORDER BY name"
  forM_ (tables :: [SQL.Only Text]) $ \(SQL.Only ddl) -> do
    putTextLn (ddl <> ";")
    putTextLn ""
  indexes <- SQL.query_ db.conn
    "SELECT sql FROM sqlite_master WHERE type='index' AND sql IS NOT NULL ORDER BY name"
  forM_ (indexes :: [SQL.Only Text]) $ \(SQL.Only ddl) ->
    putTextLn (ddl <> ";")
  close db
