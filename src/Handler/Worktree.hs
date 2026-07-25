-- | Port of worktree/resources.go: the .worktree-resources file.
module Handler.Worktree
  ( Resource(..)
  , parseResourceId
  , readResources
  , appendResource
  , removeResource
  ) where

import Control.Monad (unless, when)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.IO as TIO
import System.Directory (doesFileExist, removeFile)

data Resource = Resource
  { resId   :: Text
  , resUrl  :: Text
  , primary :: Bool
  } deriving (Show, Eq)

-- | Splits \"type:id\" into (type, id); type is \"\" when no colon present.
parseResourceId :: Text -> (Text, Text)
parseResourceId resourceId =
  case T.breakOn ":" resourceId of
    (t, rest) | not (T.null rest) -> (t, T.drop 1 rest)
    _ -> ("", resourceId)

-- | Reads the .worktree-resources file; [] if it doesn't exist.
-- Lines are \"id url\" with an optional \"~ \" prefix marking non-primary.
readResources :: FilePath -> IO [Resource]
readResources path = do
  exists <- doesFileExist path
  if not exists
    then pure []
    else do
      contents <- TIO.readFile path
      pure (concatMap parseLine (T.lines contents))
  where
    parseLine raw =
      let line0 = T.strip raw
      in if T.null line0
           then []
           else
             let (primary, line) = case T.stripPrefix "~ " line0 of
                   Just rest -> (False, rest)
                   Nothing   -> (True, line0)
             in case T.words line of
                  (rid : url : _) -> [Resource rid url primary]
                  _               -> []

-- | Appends a resource line unless the ID is already present.
appendResource :: FilePath -> Text -> Text -> Bool -> IO ()
appendResource path resourceId url primary = do
  existing <- readResources path
  unless (any (\r -> r.resId == resourceId) existing) $ do
    let line = resourceId <> " " <> url <> "\n"
    TIO.appendFile path (if primary then line else "~ " <> line)

-- | Removes a resource line; deletes the file when it becomes empty.
removeResource :: FilePath -> Text -> IO ()
removeResource path resourceId = do
  resources <- readResources path
  when (not (null resources)) $ do
    let filtered = filter (\r -> r.resId /= resourceId) resources
    if null filtered
      then removeFile path
      else TIO.writeFile path $ T.concat
        [ (if r.primary then "" else "~ ") <> r.resId <> " " <> r.resUrl <> "\n"
        | r <- filtered
        ]
