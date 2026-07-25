-- | Port of config/config_test.go.
module ConfigSpec (spec) where

import Control.Monad (forM_)
import Data.Bits ((.&.))
import qualified Data.Map.Strict as Map
import Data.Text (Text)
import qualified Data.Text.IO as TIO
import System.FilePath ((</>))
import System.IO.Temp (withSystemTempDirectory)
import System.Posix.Files (fileMode, getFileStatus)
import Test.Hspec

import Handler.Config

spec :: Spec
spec = do
  it "TestReadWriteConfig: round-trips services with 0600 permissions" $
    withSystemTempDirectory "config-test" $ \dir -> do
      let path = dir </> "config.yaml"
          cfg = emptyConfig
            { services = Services
                { github = Just GitHubConfig { token = "ghp_test123" }
                , jira = Just JiraConfig
                    { url = "https://jira.example.com"
                    , email = "test@example.com"
                    , token = "jira_token_abc"
                    , botUsernames = ["bot1", "bot2"]
                    , customFields = Map.empty
                    }
                }
            }
      writeConfig path cfg

      st <- getFileStatus path
      (fileMode st .&. 0o777) `shouldBe` 0o600

      readCfg <- readConfig path
      case readCfg.services.github of
        Nothing -> expectationFailure "GitHub config is Nothing"
        Just g -> g.token `shouldBe` "ghp_test123"
      case readCfg.services.jira of
        Nothing -> expectationFailure "Jira config is Nothing"
        Just j -> do
          j.url `shouldBe` "https://jira.example.com"
          j.email `shouldBe` "test@example.com"
          j.token `shouldBe` "jira_token_abc"
          j.botUsernames `shouldBe` ["bot1", "bot2"]

  it "TestReadMissingFile: returns empty config" $ do
    cfg <- readConfig "/nonexistent/path/config.yaml"
    cfg.services.github `shouldBe` Nothing
    cfg.services.jira `shouldBe` Nothing

  describe "TestDefaultResourceURL" $ do
    let cfg = emptyConfig
          { services = Services
              { github = Nothing
              , jira = Just JiraConfig
                  { url = "https://jira.example.com", email = "", token = ""
                  , botUsernames = [], customFields = Map.empty
                  }
              }
          }
        cases :: [(String, Text, Text, Text)]
        cases =
          [ ("PR", "pr", "owner/repo#123", "https://github.com/owner/repo/pull/123")
          , ("PR with org", "pr", "my-org/my-repo#42", "https://github.com/my-org/my-repo/pull/42")
          , ("PR missing hash", "pr", "owner/repo", "")
          , ("PR empty number", "pr", "owner/repo#", "")
          , ("Jira", "jira", "PROJ-456", "https://jira.example.com/browse/PROJ-456")
          , ("Jira trailing slash", "jira", "PROJ-1", "https://jira.example.com/browse/PROJ-1")
          , ("Unknown type", "slack", "chan-123", "")
          ]
    forM_ cases $ \(name, rType, rId, expected) ->
      it name $ defaultResourceUrl cfg rType rId `shouldBe` expected

    it "trailing slash on Jira URL" $ do
      let cfgSlash = emptyConfig
            { services = Services
                { github = Nothing
                , jira = Just JiraConfig
                    { url = "https://jira.example.com/", email = "", token = ""
                    , botUsernames = [], customFields = Map.empty
                    }
                }
            }
      defaultResourceUrl cfgSlash "jira" "PROJ-1"
        `shouldBe` "https://jira.example.com/browse/PROJ-1"

    it "no Jira config" $
      defaultResourceUrl emptyConfig "jira" "PROJ-1" `shouldBe` ""

  describe "TestIsServiceConfigured" $ do
    let withGithub t = emptyConfig { services = Services (Just (GitHubConfig t)) Nothing }
        withJira t = emptyConfig
          { services = Services Nothing
              (Just JiraConfig { url = "", email = "", token = t
                               , botUsernames = [], customFields = Map.empty })
          }
    it "GitHub configured" $ isServiceConfigured (withGithub "test") "github" `shouldBe` True
    it "GitHub not configured - nil" $ isServiceConfigured emptyConfig "github" `shouldBe` False
    it "GitHub not configured - empty token" $ isServiceConfigured (withGithub "") "github" `shouldBe` False
    it "Jira configured" $ isServiceConfigured (withJira "test") "jira" `shouldBe` True
    it "Jira not configured - nil" $ isServiceConfigured emptyConfig "jira" `shouldBe` False
    it "Jira not configured - empty token" $ isServiceConfigured (withJira "") "jira" `shouldBe` False
    it "Unknown service" $ isServiceConfigured emptyConfig "unknown" `shouldBe` False

  it "TestJiraCustomFieldsConfig: parses custom_fields map" $
    withSystemTempDirectory "config-test" $ \dir -> do
      let path = dir </> "config.yaml"
      TIO.writeFile path $ mconcat
        [ "services:\n"
        , "  jira:\n"
        , "    url: https://jira.example.com\n"
        , "    email: test@example.com\n"
        , "    token: test-token\n"
        , "    custom_fields:\n"
        , "      epic_key: \"customfield_10014\"\n"
        , "      blocked: \"customfield_10517\"\n"
        , "      story_points: \"customfield_10028\"\n"
        ]
      cfg <- readConfig path
      case cfg.services.jira of
        Nothing -> expectationFailure "expected Jira config"
        Just j -> do
          Map.size j.customFields `shouldBe` 3
          Map.lookup "epic_key" j.customFields `shouldBe` Just "customfield_10014"

  describe "TestStatuslineShowContext" $ do
    let withSl sc = emptyConfig { statusline = Just (StatuslineConfig sc Nothing) }
    it "nil statusline" $ statuslineShowContext emptyConfig `shouldBe` True
    it "nil show_context" $ statuslineShowContext (withSl Nothing) `shouldBe` True
    it "explicit true" $ statuslineShowContext (withSl (Just True)) `shouldBe` True
    it "explicit false" $ statuslineShowContext (withSl (Just False)) `shouldBe` False

  describe "TestStatuslineShowGit" $ do
    let withSl sg = emptyConfig { statusline = Just (StatuslineConfig Nothing sg) }
    it "nil statusline" $ statuslineShowGit emptyConfig `shouldBe` True
    it "nil show_git" $ statuslineShowGit (withSl Nothing) `shouldBe` True
    it "explicit true" $ statuslineShowGit (withSl (Just True)) `shouldBe` True
    it "explicit false" $ statuslineShowGit (withSl (Just False)) `shouldBe` False

  it "TestJiraNoCustomFields: absent map is empty" $
    withSystemTempDirectory "config-test" $ \dir -> do
      let path = dir </> "config.yaml"
      TIO.writeFile path $ mconcat
        [ "services:\n"
        , "  jira:\n"
        , "    url: https://jira.example.com\n"
        , "    email: test@example.com\n"
        , "    token: test-token\n"
        ]
      cfg <- readConfig path
      fmap (Map.null . (.customFields)) cfg.services.jira `shouldBe` Just True
