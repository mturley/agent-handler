-- | Port of watcher/github/: PR polling via the GitHub GraphQL API.
module Handler.Watcher.GitHub
  ( PRRef(..)
  , PRData(..)
  , Review(..)
  , Comment(..)
  , ReviewComment(..)
  , CommitInfo(..)
  , CheckRun(..)
  , RateLimit(..)
  , parsePrResourceId
  , fetchPRs
  , buildPrStateJson
  , poll
  ) where

import Control.Exception (SomeException, try)
import Control.Monad (forM, forM_)
import qualified Data.Aeson as A
import qualified Data.Aeson.Key as Key
import qualified Data.Aeson.KeyMap as KM
import Data.Char (isDigit)
import qualified Data.Map.Strict as Map
import Data.Maybe (catMaybes, fromMaybe)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.Lazy as TL
import qualified Data.Text.Lazy.Encoding as TLE
import qualified Data.Text.Encoding as TE
import qualified Data.Vector as V
import Network.HTTP.Simple

import Handler.Config (Config(..), GitHubConfig(..), Services(..))
import Handler.Db (Db)
import Handler.Db.ResourceState (ResourceState(..), getResourceState, upsertResourceState)
import Handler.Db.WatcherStatus (recordWatcherError, recordWatcherSuccess)
import Handler.Util (nowIso)
import Handler.Watcher.Framework
  ( WatchedResource(..)
  , emitWatcherError
  , emitWatcherEvent
  , eventCursor
  , isDuplicate
  , watcherLog
  )

-- | Identifies a GitHub pull request.
data PRRef = PRRef
  { owner  :: Text
  , repo   :: Text
  , number :: Int
  } deriving (Show, Eq)

-- | Pull request data from the GitHub GraphQL API.
data PRData = PRData
  { number         :: Int
  , owner          :: Text
  , repo           :: Text
  , state          :: Text
  , title          :: Text
  , updatedAt      :: Text
  , reviews        :: [Review]
  , comments       :: [Comment]
  , reviewComments :: [ReviewComment]
  , commits        :: CommitInfo
  , checkRuns      :: [CheckRun]
  } deriving (Show, Eq)

-- | A PR review.
data Review = Review
  { author      :: Text
  , authorType  :: Text
  , state       :: Text
  , submittedAt :: Text
  , body        :: Text
  } deriving (Show, Eq)

-- | A PR issue comment.
data Comment = Comment
  { author     :: Text
  , authorType :: Text
  , createdAt  :: Text
  , body       :: Text
  } deriving (Show, Eq)

-- | A PR review comment (inline comment on code).
data ReviewComment = ReviewComment
  { author     :: Text
  , authorType :: Text
  , createdAt  :: Text
  , path       :: Text
  , body       :: Text
  } deriving (Show, Eq)

-- | Commit count and latest SHA.
data CommitInfo = CommitInfo
  { totalCount :: Int
  , latestSha  :: Text
  , latestDate :: Text
  } deriving (Show, Eq)

-- | A check run status.
data CheckRun = CheckRun
  { name        :: Text
  , conclusion  :: Text
  , completedAt :: Text
  } deriving (Show, Eq)

-- | GitHub API rate limit info.
data RateLimit = RateLimit
  { remaining :: Int
  , limit     :: Int
  } deriving (Show, Eq)

-- | Parses a resource ID like \"owner\/repo#123\" into a PRRef.
parsePrResourceId :: Text -> Either Text PRRef
parsePrResourceId resourceId =
  case T.breakOn "/" resourceId of
    (owner, slashRest)
      | not (T.null owner)
      , Just rest <- T.stripPrefix "/" slashRest
      , (repo, hashRest) <- T.breakOn "#" rest
      , not (T.null repo)
      , Just numText <- T.stripPrefix "#" hashRest
      , not (T.null numText)
      , T.all isDigit numText
      -> Right PRRef { owner = owner, repo = repo, number = read (T.unpack numText) }
    _ -> Left ("invalid PR resource ID format: " <> T.pack (show resourceId) <> " (expected owner/repo#number)")

-- | Fetches PR data for multiple PRs in a single batched GraphQL query.
-- The optional endpoint overrides the GitHub API URL (for testing).
fetchPRs :: Text -> [PRRef] -> Maybe Text -> IO (Either Text ([PRData], RateLimit))
fetchPRs _ [] _ = pure (Left "no PRs to fetch")
fetchPRs token prs apiUrl = do
  let endpoint = fromMaybe "https://api.github.com/graphql" apiUrl
  result <- try $ do
    req0 <- parseRequest ("POST " <> T.unpack endpoint)
    let req = setRequestBodyJSON (A.object ["query" A..= buildBatchedPrQuery prs])
            $ setRequestHeader "Authorization" ["Bearer " <> TE.encodeUtf8 token]
            $ setRequestHeader "Content-Type" ["application/json"]
            $ setRequestHeader "User-Agent" ["agent-handler"]
              req0
    httpLBS req
  case result of
    Left (e :: SomeException) -> pure (Left ("failed to execute request: " <> T.pack (show e)))
    Right resp -> do
      let code = getResponseStatusCode resp
          bodyText = TL.toStrict (TLE.decodeUtf8 (getResponseBody resp))
      if code /= 200
        then pure (Left ("GitHub API returned status " <> T.pack (show code) <> ": " <> bodyText))
        else case A.decode (getResponseBody resp) of
          Nothing -> pure (Left "failed to unmarshal response")
          Just val -> pure (parseGraphQlResponse val prs)

-- | Constructs a GraphQL query for multiple PRs.
buildBatchedPrQuery :: [PRRef] -> Text
buildBatchedPrQuery prs =
  "query {\n    "
  <> T.intercalate "\n    " (zipWith alias [0 :: Int ..] prs)
  <> "\n    rateLimit {\n      remaining\n      limit\n    }\n  }"
  where
    alias i pr =
      "pr" <> T.pack (show i) <> ": repository(owner: \"" <> pr.owner
      <> "\", name: \"" <> pr.repo <> "\") {\n\
         \      pullRequest(number: " <> T.pack (show pr.number) <> ") {\n\
         \        number\n\
         \        state\n\
         \        title\n\
         \        updatedAt\n\
         \        reviews(last: 20) {\n\
         \          nodes {\n\
         \            author {\n\
         \              __typename\n\
         \              login\n\
         \            }\n\
         \            state\n\
         \            submittedAt\n\
         \            body\n\
         \          }\n\
         \        }\n\
         \        comments(last: 20) {\n\
         \          nodes {\n\
         \            author {\n\
         \              __typename\n\
         \              login\n\
         \            }\n\
         \            createdAt\n\
         \            body\n\
         \          }\n\
         \        }\n\
         \        reviewThreads(last: 20) {\n\
         \          nodes {\n\
         \            comments(last: 20) {\n\
         \              nodes {\n\
         \                author {\n\
         \                  __typename\n\
         \                  login\n\
         \                }\n\
         \                createdAt\n\
         \                path\n\
         \                body\n\
         \              }\n\
         \            }\n\
         \          }\n\
         \        }\n\
         \        commits(last: 1) {\n\
         \          totalCount\n\
         \          nodes {\n\
         \            commit {\n\
         \              oid\n\
         \              committedDate\n\
         \              checkSuites(last: 10) {\n\
         \                nodes {\n\
         \                  checkRuns(last: 20) {\n\
         \                    nodes {\n\
         \                      name\n\
         \                      conclusion\n\
         \                      completedAt\n\
         \                    }\n\
         \                  }\n\
         \                }\n\
         \              }\n\
         \            }\n\
         \          }\n\
         \        }\n\
         \      }\n\
         \    }"

-- JSON plucking helpers over aeson Values.

(.?) :: A.Object -> Text -> Maybe A.Value
o .? k = KM.lookup (Key.fromText k) o

asObj :: A.Value -> Maybe A.Object
asObj = \case { A.Object o -> Just o; _ -> Nothing }

objAt :: A.Object -> Text -> Maybe A.Object
objAt o k = o .? k >>= asObj

textAt :: A.Object -> Text -> Text
textAt o k = case o .? k of
  Just (A.String s) -> s
  _ -> ""

intAt :: A.Object -> Text -> Int
intAt o k = case o .? k of
  Just (A.Number n) -> truncate n
  _ -> 0

nodesAt :: A.Object -> Text -> [A.Object]
nodesAt o k = fromMaybe [] $ do
  conn <- objAt o k
  A.Array ns <- conn .? "nodes"
  pure (catMaybes (map asObj (V.toList ns)))

-- | Parses the GraphQL response into PRData values and the rate limit.
parseGraphQlResponse :: A.Value -> [PRRef] -> Either Text ([PRData], RateLimit)
parseGraphQlResponse val prs = do
  top <- maybe (Left "failed to unmarshal response") Right (asObj val)
  case top .? "errors" of
    Just (A.Array errs) | not (V.null errs) ->
      Left ("GraphQL errors: " <> T.intercalate "; "
             [ textAt e "message" | Just e <- map asObj (V.toList errs) ])
    _ -> do
      dataObj <- maybe (Left "failed to unmarshal raw data") Right (objAt top "data")
      let rl = case objAt dataObj "rateLimit" of
            Just rlo -> RateLimit { remaining = intAt rlo "remaining", limit = intAt rlo "limit" }
            Nothing  -> RateLimit 0 0
      prDataList <- forM (zip [0 :: Int ..] prs) $ \(i, pr) -> do
        let aliasKey = "pr" <> T.pack (show i)
        repoObj <- maybe (Left ("missing PR data for alias " <> aliasKey)) Right (objAt dataObj aliasKey)
        case objAt repoObj "pullRequest" of
          Nothing -> Left ("PR " <> pr.owner <> "/" <> pr.repo <> "#" <> T.pack (show pr.number) <> " not found")
          Just node -> Right (parsePrNode node pr.owner pr.repo)
      pure (prDataList, rl)

-- | Converts a pullRequest node into a PRData.
parsePrNode :: A.Object -> Text -> Text -> PRData
parsePrNode node owner repo = PRData
  { number = intAt node "number"
  , owner = owner
  , repo = repo
  , state = textAt node "state"
  , title = textAt node "title"
  , updatedAt = textAt node "updatedAt"
  , reviews =
      [ Review
          { author = authorLogin r
          , authorType = authorTypeOf r
          , state = textAt r "state"
          , submittedAt = textAt r "submittedAt"
          , body = textAt r "body"
          }
      | r <- nodesAt node "reviews"
      ]
  , comments =
      [ Comment
          { author = authorLogin c
          , authorType = authorTypeOf c
          , createdAt = textAt c "createdAt"
          , body = textAt c "body"
          }
      | c <- nodesAt node "comments"
      ]
  , reviewComments =
      [ ReviewComment
          { author = authorLogin rc
          , authorType = authorTypeOf rc
          , createdAt = textAt rc "createdAt"
          , path = textAt rc "path"
          , body = textAt rc "body"
          }
      | thread <- nodesAt node "reviewThreads"
      , rc <- nodesAt thread "comments"
      ]
  , commits = CommitInfo
      { totalCount = maybe 0 (`intAt` "totalCount") (objAt node "commits")
      , latestSha = maybe "" (`textAt` "oid") latestCommit
      , latestDate = maybe "" (`textAt` "committedDate") latestCommit
      }
  , checkRuns =
      [ CheckRun
          { name = textAt run "name"
          , conclusion = textAt run "conclusion"
          , completedAt = completedAtOf run
          }
      | commit <- maybe [] pure latestCommit
      , suite <- nodesAt commit "checkSuites"
      , run <- nodesAt suite "checkRuns"
      , completedAtOf run /= ""
      ]
  }
  where
    latestCommit = case [ c | n <- nodesAt node "commits", Just c <- [objAt n "commit"] ] of
      (c : _) -> Just c
      []      -> Nothing
    authorLogin o = maybe "" (`textAt` "login") (objAt o "author")
    authorTypeOf o =
      if maybe "" (`textAt` "__typename") (objAt o "author") == "Bot"
        then "bot" else "user"
    completedAtOf run = textAt run "completedAt"

-- | Polls GitHub for PR updates and emits events.
poll :: Db -> Config -> [WatchedResource] -> IO (Either Text ())
poll db cfg resources =
  case cfg.services.github of
    Just gh | gh.token /= "" -> pollWith db gh.token resources
    _ -> pure (Left "GitHub token not configured")

pollWith :: Db -> Text -> [WatchedResource] -> IO (Either Text ())
pollWith db token resources = do
  let logger = watcherLog "github"
  parsed <- forM resources $ \r ->
    case parsePrResourceId r.resourceId of
      Left err -> do
        logger ("ERROR: failed to parse resource ID " <> T.pack (show r.resourceId) <> ": " <> err)
        emitWatcherError db "github" "Invalid PR resource ID"
          (Just ("Failed to parse resource ID: " <> err)) r
        pure Nothing
      Right ref -> pure (Just (ref, r))
  let refs = catMaybes parsed
      resourceMap = Map.fromList [(r.resourceId, r) | (_, r) <- refs]
  if null refs
    then do
      logger "No valid PRs to poll"
      pure (Right ())
    else do
      logger ("Fetching data for " <> T.pack (show (length refs)) <> " PRs...")
      result <- fetchPRs token (map fst refs) Nothing
      case result of
        Left err -> do
          logger ("ERROR: failed to fetch PRs: " <> err)
          let errBody = "Failed to fetch PR data: " <> err
          forM_ resources $ \r ->
            emitWatcherError db "github" "GitHub API error" (Just errBody) r
          recordWatcherError db "github" errBody
          pure (Left err)
        Right (prDataList, rl) -> do
          logger ("Rate limit: " <> T.pack (show rl.remaining) <> "/" <> T.pack (show rl.limit) <> " remaining")
          counts <- forM prDataList $ \prData -> do
            let resourceId = prData.owner <> "/" <> prData.repo <> "#" <> T.pack (show prData.number)
            case Map.lookup resourceId resourceMap of
              Nothing -> do
                logger ("WARNING: received data for unknown resource " <> T.pack (show resourceId))
                pure 0
              Just resource -> do
                processed <- try (processPR db prData resource)
                case processed :: Either SomeException Int of
                  Left err -> do
                    logger ("ERROR: failed to process PR " <> resourceId <> ": " <> T.pack (show err))
                    emitWatcherError db "github" "PR processing error"
                      (Just ("Failed to process PR: " <> T.pack (show err))) resource
                    pure 0
                  Right n -> pure n
          logger ("Emitted " <> T.pack (show (sum counts)) <> " events")
          recordWatcherSuccess db "github"
          pure (Right ())

-- | Processes a single PR and emits events; returns the count emitted.
processPR :: Db -> PRData -> WatchedResource -> IO Int
processPR db prData resource = do
  let logger = watcherLog "github"
  cursor <- eventCursor db "github" resource.resourceType resource.resourceId

  if cursor == ""
    then do
      let title = "Started watching PR: " <> prData.title
          body = "PR #" <> T.pack (show prData.number) <> " in " <> prData.owner <> "/" <> prData.repo
                 <> "\nState: " <> prData.state
      emitWatcherEvent db "github" "watch_started" title (Just body) prData.updatedAt Nothing Nothing resource
      logger ("Emitted watch_started for " <> resource.resourceId)
      pure 1
    else do
      reviewCount <- fmap sum $ forM prData.reviews $ \review ->
        if review.submittedAt <= cursor
          then pure 0
          else do
            dup <- isDuplicate db "github" resource.resourceType resource.resourceId
                     (reviewEventType review.state) review.submittedAt
            if dup then pure 0 else case review.state of
              "APPROVED" -> do
                emitWatcherEvent db "github" "pr_approved"
                  ("PR approved by " <> review.author) (Just review.body)
                  review.submittedAt (Just review.author) (Just review.authorType) resource
                logger ("Emitted pr_approved for " <> resource.resourceId <> " by " <> review.author)
                pure 1
              "CHANGES_REQUESTED" -> do
                emitWatcherEvent db "github" "pr_review_comment"
                  ("Changes requested by " <> review.author) (Just review.body)
                  review.submittedAt (Just review.author) (Just review.authorType) resource
                logger ("Emitted pr_review_comment for " <> resource.resourceId <> " by " <> review.author)
                pure 1
              _ -> pure 0

      commentCount <- fmap sum $ forM prData.comments $ \comment ->
        if comment.createdAt <= cursor
          then pure 0
          else do
            dup <- isDuplicate db "github" resource.resourceType resource.resourceId
                     "pr_comment" comment.createdAt
            if dup then pure 0 else do
              emitWatcherEvent db "github" "pr_comment"
                ("Comment by " <> comment.author) (Just comment.body)
                comment.createdAt (Just comment.author) (Just comment.authorType) resource
              logger ("Emitted pr_comment for " <> resource.resourceId <> " by " <> comment.author)
              pure 1

      reviewCommentCount <- fmap sum $ forM prData.reviewComments $ \rc ->
        if rc.createdAt <= cursor
          then pure 0
          else do
            dup <- isDuplicate db "github" resource.resourceType resource.resourceId
                     "pr_review_comment" rc.createdAt
            if dup then pure 0 else do
              emitWatcherEvent db "github" "pr_review_comment"
                ("Review comment by " <> rc.author <> " on " <> rc.path) (Just rc.body)
                rc.createdAt (Just rc.author) (Just rc.authorType) resource
              logger ("Emitted pr_review_comment for " <> resource.resourceId <> " by " <> rc.author)
              pure 1

      checkCount <- fmap sum $ forM prData.checkRuns $ \checkRun ->
        if checkRun.completedAt <= cursor
          then pure 0
          else case checkRunEventType checkRun.conclusion of
            Nothing -> pure 0
            Just eventType -> do
              dup <- isDuplicate db "github" resource.resourceType resource.resourceId
                       eventType checkRun.completedAt
              if dup then pure 0 else do
                emitWatcherEvent db "github" eventType
                  ("Check " <> checkRun.name <> ": " <> checkRun.conclusion) Nothing
                  checkRun.completedAt Nothing Nothing resource
                logger ("Emitted " <> eventType <> " for " <> resource.resourceId <> ": " <> checkRun.name)
                pure 1

      stateCount <-
        if prData.state == "MERGED" || prData.state == "CLOSED"
          then do
            let eventType = if prData.state == "CLOSED" then "pr_closed" else "pr_merged"
            dup <- isDuplicate db "github" resource.resourceType resource.resourceId
                     eventType prData.updatedAt
            if dup then pure 0 else do
              emitWatcherEvent db "github" eventType
                ("PR " <> prData.state)
                (Just ("PR #" <> T.pack (show prData.number) <> ": " <> prData.title))
                prData.updatedAt Nothing Nothing resource
              logger ("Emitted " <> eventType <> " for " <> resource.resourceId)
              pure 1
          else pure 0

      -- Detect new commits by comparing latest SHA against previous state
      newCommitCount <-
        if prData.commits.latestSha == ""
          then pure 0
          else do
            prevState <- getResourceState db "pr" resource.resourceId
            let prevSha = fromMaybe "" $ do
                  ps <- prevState
                  A.Object prev <- A.decode (TLE.encodeUtf8 (TL.fromStrict ps.stateJson))
                  A.String sha <- prev .? "latest_commit_sha"
                  pure sha
            if prevSha /= "" && prevSha /= prData.commits.latestSha
              then do
                dup <- isDuplicate db "github" resource.resourceType resource.resourceId
                         "pr_new_commits" prData.commits.latestDate
                if dup then pure 0 else do
                  emitted <- try $ emitWatcherEvent db "github" "pr_new_commits"
                    ("New commits pushed to PR #" <> T.pack (show prData.number))
                    (Just ("Latest commit: " <> T.take 7 prData.commits.latestSha))
                    prData.commits.latestDate Nothing Nothing resource
                  case emitted :: Either SomeException () of
                    Left err -> do
                      logger ("WARNING: failed to emit new commits event: " <> T.pack (show err))
                      pure 0
                    Right () -> do
                      logger ("Emitted pr_new_commits for " <> resource.resourceId)
                      pure 1
              else pure 0

      now <- nowIso
      written <- try $ upsertResourceState db "pr" resource.resourceId
                   (buildPrStateJson prData) prData.updatedAt now
      case written :: Either SomeException () of
        Left err -> logger ("WARNING: failed to upsert resource state for " <> resource.resourceId
                            <> ": " <> T.pack (show err))
        Right () -> pure ()

      pure (reviewCount + commentCount + reviewCommentCount + checkCount + stateCount + newCommitCount)

reviewEventType :: Text -> Text
reviewEventType state = if state == "APPROVED" then "pr_approved" else "pr_review_comment"

checkRunEventType :: Text -> Maybe Text
checkRunEventType = \case
  "SUCCESS" -> Just "ci_check_passed"
  "NEUTRAL" -> Just "ci_check_passed"
  "SKIPPED" -> Just "ci_check_passed"
  "FAILURE" -> Just "ci_check_failed"
  "TIMED_OUT" -> Just "ci_check_failed"
  "ACTION_REQUIRED" -> Just "ci_check_failed"
  "CANCELLED" -> Just "ci_check_failed"
  "STALE" -> Just "ci_check_failed"
  _ -> Nothing

-- | Computes the overall review decision based on the latest review per author.
derivePrReviewDecision :: [Review] -> Text
derivePrReviewDecision reviews =
  let latestByAuthor = Map.elems $ Map.fromListWith
        (\a b -> if a.submittedAt > b.submittedAt then a else b)
        [ (r.author, r) | r <- reviews, r.state /= "DISMISSED" ]
  in if | null latestByAuthor -> "NONE"
        | any (\r -> r.state == "CHANGES_REQUESTED") latestByAuthor -> "CHANGES_REQUESTED"
        | all (\r -> r.state == "APPROVED") latestByAuthor -> "APPROVED"
        | otherwise -> "REVIEW_REQUIRED"

-- | Computes the overall CI status based on check runs.
deriveCiStatus :: [CheckRun] -> Text
deriveCiStatus checkRuns
  | null checkRuns = "NONE"
  | any (\cr -> cr.conclusion `elem` ["FAILURE", "TIMED_OUT", "ACTION_REQUIRED", "CANCELLED"]) checkRuns = "FAILURE"
  | any (\cr -> cr.conclusion == "") checkRuns = "PENDING"
  | otherwise = "SUCCESS"

-- | Whether there are commits after the latest review.
hasNewCommitsSinceReview :: PRData -> Bool
hasNewCommitsSinceReview prData
  | prData.commits.latestDate == "" = False
  | otherwise =
      let latestReviewDate = maximum ("" : [r.submittedAt | r <- prData.reviews])
      in latestReviewDate /= "" && prData.commits.latestDate > latestReviewDate

-- | Constructs the state JSON for a PR.
buildPrStateJson :: PRData -> Text
buildPrStateJson prData =
  TL.toStrict $ TLE.decodeUtf8 $ A.encode $ A.object
    [ "title" A..= prData.title
    , "state" A..= prData.state
    , "review_decision" A..= derivePrReviewDecision prData.reviews
    , "has_new_commits_since_review" A..= hasNewCommitsSinceReview prData
    , "ci_status" A..= deriveCiStatus prData.checkRuns
    , "latest_commit_sha" A..= prData.commits.latestSha
    ]
