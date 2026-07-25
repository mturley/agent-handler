-- | Port of config/validate_test.go, with a warp mock standing in for
-- Go's httptest server.
module ValidateSpec (spec) where

import qualified Data.Aeson as A
import qualified Data.Aeson.KeyMap as KM
import qualified Data.ByteString as BS
import qualified Data.ByteString.Lazy as BL
import Data.IORef (IORef, newIORef, readIORef, writeIORef)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.Encoding as TE
import Network.HTTP.Types (mkStatus)
import Network.Wai (Application, rawPathInfo, requestHeaders, requestMethod, responseLBS, strictRequestBody)
import Network.Wai.Handler.Warp (testWithApplication)
import Test.Hspec

import Handler.Config (validateGitHubToken, validateJiraToken)

data RecordedReq = RecordedReq
  { reqMethod :: BS.ByteString
  , reqPath   :: BS.ByteString
  , reqAuth   :: Maybe BS.ByteString
  , reqBody   :: BL.ByteString
  }

-- | An app that records the request and replies with a canned status/body.
mockApp :: Int -> BL.ByteString -> IORef (Maybe RecordedReq) -> Application
mockApp status body ref req respond = do
  b <- strictRequestBody req
  writeIORef ref $ Just RecordedReq
    { reqMethod = requestMethod req
    , reqPath = rawPathInfo req
    , reqAuth = lookup "Authorization" (requestHeaders req)
    , reqBody = b
    }
  respond (responseLBS (mkStatus status "") [("Content-Type", "application/json")] body)

withMock :: Int -> BL.ByteString -> (Text -> IORef (Maybe RecordedReq) -> IO a) -> IO a
withMock status body f = do
  ref <- newIORef Nothing
  testWithApplication (pure (mockApp status body ref)) $ \port ->
    f ("http://127.0.0.1:" <> T.pack (show port)) ref

shouldBeLeftContaining :: Either Text Text -> Text -> Expectation
shouldBeLeftContaining result needle = case result of
  Left err -> err `shouldSatisfy` (needle `T.isInfixOf`)
  Right v -> expectationFailure ("expected error, got Right " <> T.unpack v)

spec :: Spec
spec = do
  describe "TestValidateGitHubToken" $ do
    it "valid token" $
      withMock 200 "{\"data\":{\"viewer\":{\"login\":\"testuser\"}}}" $ \url ref -> do
        result <- validateGitHubToken "ghp_valid" (Just url)
        result `shouldBe` Right "testuser"
        mreq <- readIORef ref
        case mreq of
          Nothing -> expectationFailure "no request recorded"
          Just req -> do
            req.reqMethod `shouldBe` "POST"
            req.reqAuth `shouldBe` Just "Bearer ghp_valid"

    it "invalid token - 401" $
      withMock 401 "{\"message\": \"Bad credentials\"}" $ \url _ -> do
        result <- validateGitHubToken "ghp_invalid" (Just url)
        result `shouldBeLeftContaining` "invalid GitHub token"

    it "GraphQL error" $
      withMock 200 "{\"errors\":[{\"message\":\"Something went wrong\"}]}" $ \url _ -> do
        result <- validateGitHubToken "ghp_error" (Just url)
        result `shouldBeLeftContaining` "GraphQL error"

    it "empty login" $
      withMock 200 "{\"data\":{\"viewer\":{\"login\":\"\"}}}" $ \url _ -> do
        result <- validateGitHubToken "ghp_empty" (Just url)
        result `shouldBeLeftContaining` "empty login"

  describe "TestValidateJiraToken" $ do
    it "valid credentials" $
      withMock 200 "{\"displayName\": \"Test User\"}" $ \url ref -> do
        result <- validateJiraToken url "test@example.com" "jira_token_123"
        result `shouldBe` Right "Test User"
        mreq <- readIORef ref
        case mreq of
          Nothing -> expectationFailure "no request recorded"
          Just req -> do
            req.reqMethod `shouldBe` "GET"
            req.reqPath `shouldSatisfy` BS.isSuffixOf "/rest/api/3/myself"
            case req.reqAuth of
              Just auth -> auth `shouldSatisfy` BS.isPrefixOf "Basic "
              Nothing -> expectationFailure "no Authorization header"

    it "invalid credentials - 401" $
      withMock 401 "{\"errorMessages\": [\"Invalid credentials\"]}" $ \url _ -> do
        result <- validateJiraToken url "test@example.com" "bad_token"
        result `shouldBeLeftContaining` "invalid Jira credentials"

    it "server error - 500" $
      withMock 500 "{\"errorMessages\": [\"Internal server error\"]}" $ \url _ -> do
        result <- validateJiraToken url "test@example.com" "token"
        result `shouldBeLeftContaining` "Jira API error: status 500"

    it "empty displayName" $
      withMock 200 "{\"displayName\": \"\"}" $ \url _ -> do
        result <- validateJiraToken url "test@example.com" "token"
        result `shouldBeLeftContaining` "empty displayName"

  it "TestValidateGitHubTokenRequestBody: sends the viewer/login query" $
    withMock 200 "{\"data\":{\"viewer\":{\"login\":\"test\"}}}" $ \url ref -> do
      result <- validateGitHubToken "test_token" (Just url)
      result `shouldBe` Right "test"
      mreq <- readIORef ref
      case mreq of
        Nothing -> expectationFailure "no request recorded"
        Just req ->
          case A.decode (req.reqBody) of
            Just (A.Object o)
              | Just (A.String q) <- KM.lookup "query" o -> do
                  q `shouldSatisfy` ("viewer" `T.isInfixOf`)
                  q `shouldSatisfy` ("login" `T.isInfixOf`)
            _ -> expectationFailure
                   ("bad request body: " <> T.unpack (TE.decodeUtf8Lenient (BL.toStrict (req.reqBody))))
