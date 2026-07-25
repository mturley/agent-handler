-- | Port of worktree/resources_test.go.
module WorktreeSpec (spec) where

import Control.Monad (forM_)
import Data.Text (Text)
import qualified Data.Text.IO as TIO
import System.FilePath ((</>))
import System.IO.Temp (withSystemTempDirectory)
import Test.Hspec

import Handler.Worktree (Resource(..), appendResource, parseResourceId, readResources, removeResource)

withResourceFile :: Text -> (FilePath -> IO a) -> IO a
withResourceFile contents f =
  withSystemTempDirectory "worktree-test" $ \dir -> do
    let path = dir </> ".worktree-resources"
    TIO.writeFile path contents
    f path

spec :: Spec
spec = do
  it "TestReadResources" $
    withResourceFile
      "pr:owner/repo#42 https://github.com/owner/repo/pull/42\njira:RHOAIENG-100 https://redhat.atlassian.net/browse/RHOAIENG-100\n"
      $ \path -> do
        resources <- readResources path
        length resources `shouldBe` 2
        (head resources).resId `shouldBe` "pr:owner/repo#42"
        (head resources).primary `shouldBe` True

  it "TestReadResourcesWithRelated: ~ prefix marks non-primary" $
    withResourceFile
      "pr:owner/repo#42 https://github.com/owner/repo/pull/42\n~ pr:owner/repo#40 https://github.com/owner/repo/pull/40\n~ jira:RHOAIENG-99 https://redhat.atlassian.net/browse/RHOAIENG-99\n"
      $ \path -> do
        resources <- readResources path
        map (.primary) resources `shouldBe` [True, False, False]
        (resources !! 1).resId `shouldBe` "pr:owner/repo#40"

  it "TestReadResourcesSkipsMalformed" $
    withResourceFile "pr:owner/repo#42 https://url\n\nbadline\njira:X https://y\n" $ \path -> do
      resources <- readResources path
      length resources `shouldBe` 2

  it "TestReadResourcesFileNotExist" $ do
    resources <- readResources "/nonexistent/.worktree-resources"
    resources `shouldBe` []

  it "TestAppendResourcePrimary" $
    withSystemTempDirectory "worktree-test" $ \dir -> do
      let path = dir </> ".worktree-resources"
      appendResource path "pr:owner/repo#42" "https://github.com/owner/repo/pull/42" True
      resources <- readResources path
      map (.primary) resources `shouldBe` [True]

  it "TestAppendResourceRelated" $
    withSystemTempDirectory "worktree-test" $ \dir -> do
      let path = dir </> ".worktree-resources"
      appendResource path "pr:owner/repo#42" "https://url" True
      appendResource path "pr:owner/repo#40" "https://url2" False
      resources <- readResources path
      map (.primary) resources `shouldBe` [True, False]

  it "TestAppendResourceDeduplicates" $
    withSystemTempDirectory "worktree-test" $ \dir -> do
      let path = dir </> ".worktree-resources"
      appendResource path "pr:owner/repo#42" "https://url" True
      appendResource path "pr:owner/repo#42" "https://url" True
      resources <- readResources path
      length resources `shouldBe` 1

  it "TestRemoveResource" $
    withResourceFile "pr:owner/repo#42 https://url1\n~ jira:X https://url2\n" $ \path -> do
      removeResource path "pr:owner/repo#42"
      resources <- readResources path
      map (.resId) resources `shouldBe` ["jira:X"]
      map (.primary) resources `shouldBe` [False]

  it "TestRemoveResourcePreservesMarkers" $
    withResourceFile "pr:owner/repo#42 https://url1\n~ pr:owner/repo#40 https://url2\njira:X https://url3\n" $ \path -> do
      removeResource path "pr:owner/repo#42"
      resources <- readResources path
      map (.primary) resources `shouldBe` [False, True]

  describe "TestParseResourceID" $ do
    let cases :: [(Text, Text, Text)]
        cases =
          [ ("pr:123", "pr", "123")
          , ("issue:456", "issue", "456")
          , ("jira:PROJ-789", "jira", "PROJ-789")
          , ("no-colon", "", "no-colon")
          , ("multiple:colons:here", "multiple", "colons:here")
          ]
    forM_ cases $ \(input, expType, expId) ->
      it (show input) $ parseResourceId input `shouldBe` (expType, expId)
