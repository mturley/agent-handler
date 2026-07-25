-- | Port of config/config.go and config/validate.go: the YAML config file
-- and service-token validation.
module Handler.Config
  ( Config(..)
  , Services(..)
  , GitHubConfig(..)
  , JiraConfig(..)
  , StatuslineConfig(..)
  , ExperimentalConfig(..)
  , emptyConfig
  , experimentalCostDisplay
  , statuslineShowContext
  , statuslineShowGit
  , configDefaultPath
  , readConfig
  , writeConfig
  , isServiceConfigured
  , resourceTypeToService
  , defaultResourceUrl
  , validateGitHubToken
  , validateJiraToken
  ) where

import Control.Exception (throwIO)
import Data.Aeson ((.:?), (.!=))
import qualified Data.Aeson as A
import qualified Data.Aeson.Key as Key
import qualified Data.Aeson.KeyMap as KM
import qualified Data.ByteString.Base64 as B64
import Data.Map.Strict (Map)
import qualified Data.Map.Strict as Map
import Data.Maybe (catMaybes, fromMaybe)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.Encoding as TE
import qualified Data.Yaml as Yaml
import Network.HTTP.Simple
import System.Directory (createDirectoryIfMissing, doesFileExist, getHomeDirectory)
import System.Environment (lookupEnv)
import System.FilePath (takeDirectory, (</>))
import System.Posix.Files (setFileMode)

data Config = Config
  { services     :: Services
  , statusline   :: Maybe StatuslineConfig
  , experimental :: Maybe ExperimentalConfig
  , debug        :: Bool
  } deriving (Show, Eq)

data ExperimentalConfig = ExperimentalConfig
  { costDisplay :: Maybe Bool
  } deriving (Show, Eq)

data Services = Services
  { github :: Maybe GitHubConfig
  , jira   :: Maybe JiraConfig
  } deriving (Show, Eq)

data GitHubConfig = GitHubConfig
  { token :: Text
  } deriving (Show, Eq)

data JiraConfig = JiraConfig
  { url          :: Text
  , email        :: Text
  , token        :: Text
  , botUsernames :: [Text]
  , customFields :: Map Text Text
  } deriving (Show, Eq)

data StatuslineConfig = StatuslineConfig
  { showContext :: Maybe Bool
  , showGit     :: Maybe Bool
  } deriving (Show, Eq)

emptyConfig :: Config
emptyConfig = Config (Services Nothing Nothing) Nothing Nothing False

instance A.FromJSON Config where
  parseJSON = A.withObject "Config" $ \o -> Config
    <$> o .:? "services" .!= Services Nothing Nothing
    <*> o .:? "statusline"
    <*> o .:? "experimental"
    <*> o .:? "debug" .!= False

instance A.FromJSON ExperimentalConfig where
  parseJSON = A.withObject "ExperimentalConfig" $ \o ->
    ExperimentalConfig <$> o .:? "cost_display"

instance A.FromJSON Services where
  parseJSON = A.withObject "Services" $ \o -> Services
    <$> o .:? "github"
    <*> o .:? "jira"

instance A.FromJSON GitHubConfig where
  parseJSON = A.withObject "GitHubConfig" $ \o -> GitHubConfig
    <$> o .:? "token" .!= ""

instance A.FromJSON JiraConfig where
  parseJSON = A.withObject "JiraConfig" $ \o -> JiraConfig
    <$> o .:? "url" .!= ""
    <*> o .:? "email" .!= ""
    <*> o .:? "token" .!= ""
    <*> o .:? "bot_usernames" .!= []
    <*> o .:? "custom_fields" .!= Map.empty

instance A.FromJSON StatuslineConfig where
  parseJSON = A.withObject "StatuslineConfig" $ \o -> StatuslineConfig
    <$> o .:? "show_context"
    <*> o .:? "show_git"

-- Serialization mirrors the Go yaml tags (omitempty on optional sections).
instance A.ToJSON Config where
  toJSON c = A.object $
    [ "services" A..= c.services ]
    ++ catMaybes
       [ ("statusline" A..=) <$> c.statusline
       , ("experimental" A..=) <$> c.experimental
       ]
    ++ [ "debug" A..= c.debug | c.debug ]

instance A.ToJSON ExperimentalConfig where
  toJSON e = A.object $ catMaybes [ ("cost_display" A..=) <$> e.costDisplay ]

instance A.ToJSON Services where
  toJSON s = A.object $ catMaybes
    [ ("github" A..=) <$> s.github
    , ("jira" A..=) <$> s.jira
    ]

instance A.ToJSON GitHubConfig where
  toJSON g = A.object [ "token" A..= g.token ]

instance A.ToJSON JiraConfig where
  toJSON j = A.object $
    [ "url" A..= j.url
    , "email" A..= j.email
    , "token" A..= j.token
    ]
    ++ [ "bot_usernames" A..= j.botUsernames | not (null j.botUsernames) ]
    ++ [ "custom_fields" A..= j.customFields | not (Map.null j.customFields) ]

instance A.ToJSON StatuslineConfig where
  toJSON s = A.object $ catMaybes
    [ ("show_context" A..=) <$> s.showContext
    , ("show_git" A..=) <$> s.showGit
    ]

-- | Whether enhanced cost display is enabled (default False).
experimentalCostDisplay :: Config -> Bool
experimentalCostDisplay c =
  fromMaybe False (c.experimental >>= (.costDisplay))

-- | Whether the model/context/cost line is shown (default True).
statuslineShowContext :: Config -> Bool
statuslineShowContext c =
  fromMaybe True (c.statusline >>= (.showContext))

-- | Whether the git status line is shown (default True).
statuslineShowGit :: Config -> Bool
statuslineShowGit c =
  fromMaybe True (c.statusline >>= (.showGit))

-- | Default configuration file path; respects HANDLER_HOME.
configDefaultPath :: IO FilePath
configDefaultPath = do
  env <- lookupEnv "HANDLER_HOME"
  case env of
    Just dir | not (null dir) -> pure (dir </> "config.yaml")
    _ -> do
      home <- getHomeDirectory
      pure (home </> ".agent-handler" </> "config.yaml")

-- | Reads configuration; an empty Config if the file doesn't exist.
readConfig :: FilePath -> IO Config
readConfig path = do
  exists <- doesFileExist path
  if not exists
    then pure emptyConfig
    else do
      result <- Yaml.decodeFileEither path
      case result of
        Left err -> throwIO err
        Right cfg -> pure cfg

-- | Writes configuration with 0600 permissions, creating parent dirs.
writeConfig :: FilePath -> Config -> IO ()
writeConfig path cfg = do
  createDirectoryIfMissing True (takeDirectory path)
  Yaml.encodeFile path cfg
  setFileMode path 0o600

-- | Whether a service has a non-empty token.
isServiceConfigured :: Config -> Text -> Bool
isServiceConfigured c = \case
  "github" -> maybe False (\g -> g.token /= "") c.services.github
  "jira"   -> maybe False (\j -> j.token /= "") c.services.jira
  _        -> False

-- | Maps resource types to service names.
resourceTypeToService :: Text -> Text
resourceTypeToService = \case
  "pr"   -> "github"
  "jira" -> "jira"
  _      -> ""

-- | Constructs a URL for a resource from its type and ID.
-- \"pr\" + \"owner\/repo#123\" → github PR URL; \"jira\" + \"PROJ-123\" → browse URL.
defaultResourceUrl :: Config -> Text -> Text -> Text
defaultResourceUrl c resourceType resourceId = case resourceType of
  "pr" -> prResourceUrl resourceId
  "jira" -> case c.services.jira of
    Just j | j.url /= "" -> T.dropWhileEnd (== '/') j.url <> "/browse/" <> resourceId
    _ -> ""
  _ -> ""

prResourceUrl :: Text -> Text
prResourceUrl resourceId =
  case T.breakOnEnd "#" resourceId of
    (repoHash, num)
      | T.null repoHash -> ""
      | otherwise ->
          let repo = T.dropEnd 1 repoHash
          in if T.null repo || T.null num
               then ""
               else "https://github.com/" <> repo <> "/pull/" <> num

-- | Validates a GitHub token via the GraphQL API; returns the viewer login.
validateGitHubToken :: Text -> Maybe Text -> IO (Either Text Text)
validateGitHubToken token apiUrl = do
  let endpoint = fromMaybe "https://api.github.com/graphql" apiUrl
  req0 <- parseRequest ("POST " <> T.unpack endpoint)
  let req = setRequestBodyJSON (A.object ["query" A..= ("{ viewer { login } }" :: Text)])
          $ setRequestHeader "Authorization" ["Bearer " <> TE.encodeUtf8 token]
          $ setRequestHeader "Content-Type" ["application/json"]
          $ setRequestHeader "User-Agent" ["agent-handler"]
            req0
  resp <- httpLBS req
  let code = getResponseStatusCode resp
  if | code == 401 -> pure (Left "invalid GitHub token: authentication failed")
     | code /= 200 -> pure (Left ("GitHub API error: status " <> T.pack (show code)))
     | otherwise ->
         case A.decode (getResponseBody resp) :: Maybe A.Value of
           Nothing -> pure (Left "failed to parse response")
           Just val -> pure (extractLogin val)
  where
    extractLogin val = case val of
      A.Object o ->
        case KM.lookup "errors" o of
          Just (A.Array errs) | not (null errs) ->
            Left ("GraphQL error: " <> firstErrMessage (foldr (:) [] errs))
          _ ->
            case dig o ["data", "viewer", "login"] of
              Just (A.String login) | login /= "" -> Right login
              _ -> Left "empty login in response"
      _ -> Left "failed to parse response"
    firstErrMessage (A.Object e : _) =
      case KM.lookup "message" e of
        Just (A.String m) -> m
        _ -> "unknown"
    firstErrMessage _ = "unknown"
    dig o (k:ks) = case KM.lookup (Key.fromText k) o of
      Just (A.Object o') | not (null ks) -> dig o' ks
      Just v | null ks -> Just v
      _ -> Nothing
    dig _ [] = Nothing

-- | Validates Jira credentials via /rest/api/3/myself; returns displayName.
validateJiraToken :: Text -> Text -> Text -> IO (Either Text Text)
validateJiraToken baseUrl email token = do
  req0 <- parseRequest ("GET " <> T.unpack baseUrl <> "/rest/api/3/myself")
  let auth = B64.encode (TE.encodeUtf8 (email <> ":" <> token))
      req = setRequestHeader "Authorization" ["Basic " <> auth]
          $ setRequestHeader "Accept" ["application/json"]
            req0
  resp <- httpLBS req
  let code = getResponseStatusCode resp
  if | code == 401 -> pure (Left "invalid Jira credentials: authentication failed")
     | code /= 200 -> pure (Left ("Jira API error: status " <> T.pack (show code)))
     | otherwise ->
         case A.decode (getResponseBody resp) :: Maybe A.Value of
           Just (A.Object o)
             | Just (A.String dn) <- KM.lookup "displayName" o, dn /= ""
             -> pure (Right dn)
           Just _ -> pure (Left "empty displayName in response")
           Nothing -> pure (Left "failed to parse response")
