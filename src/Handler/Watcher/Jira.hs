-- | Port of watcher/jira/: issue polling via the Jira REST API v3.
module Handler.Watcher.Jira
  ( JiraClient(..)
  , IssueData(..)
  , IssueComment(..)
  , ChangelogEntry(..)
  , fetchIssue
  , buildJiraStateJson
  , isTerminalStatus
  , linkEpic
  , poll
  ) where

import Control.Exception (SomeException, try)
import Control.Monad (forM)
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
import qualified Data.Text.Lazy as TL
import qualified Data.Text.Lazy.Encoding as TLE
import qualified Data.Vector as V
import Network.HTTP.Simple

import Handler.Config (Config(..), JiraConfig(..), Services(..))
import Handler.Db (Db)
import Handler.Db.ResourceState (upsertResourceState)
import Handler.Db.Resources (ResourceRelationship(..), linkResources)
import Handler.Db.WatcherStatus (recordWatcherError, recordWatcherSuccess)
import Handler.Util (newUuid, nowIso)
import Handler.Watcher.Framework
  ( WatchedResource(..)
  , emitWatcherError
  , emitWatcherEvent
  , eventCursor
  , isDuplicate
  , watcherLog
  )

-- | A REST client for Jira API v3.
data JiraClient = JiraClient
  { baseUrl :: Text
  , email   :: Text
  , token   :: Text
  } deriving (Show, Eq)

-- | Jira issue data with changelog and comments.
data IssueData = IssueData
  { key          :: Text
  , summary      :: Text
  , status       :: Text
  , priority     :: Text
  , issueType    :: Text
  , assignee     :: Maybe Text
  , labels       :: [Text]
  , createdAt    :: Text
  , updatedAt    :: Text
  , comments     :: [IssueComment]
  , changelog    :: [ChangelogEntry]
  , customFields :: Map Text A.Value
  } deriving (Show, Eq)

-- | A Jira issue comment.
data IssueComment = IssueComment
  { author    :: Text
  , createdAt :: Text
  , body      :: Text  -- summary text, not full ADF
  } deriving (Show, Eq)

-- | A single Jira changelog item.
data ChangelogEntry = ChangelogEntry
  { author    :: Text
  , createdAt :: Text
  , field     :: Text
  , from      :: Text
  , to        :: Text
  } deriving (Show, Eq)

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

arrAt :: A.Object -> Text -> [A.Value]
arrAt o k = case o .? k of
  Just (A.Array v) -> V.toList v
  _ -> []

objsAt :: A.Object -> Text -> [A.Object]
objsAt o k = catMaybes (map asObj (arrAt o k))

-- | Fetches issue data from Jira API v3.
fetchIssue :: JiraClient -> Text -> Map Text Text -> IO (Either Text IssueData)
fetchIssue client issueKey customFieldIds = do
  let fields = T.intercalate ","
        ("summary,status,assignee,labels,comment,priority,issuetype,created,updated"
         : Map.elems customFieldIds)
      url = client.baseUrl <> "/rest/api/3/issue/" <> issueKey
            <> "?expand=changelog&fields=" <> fields
  result <- try $ do
    req0 <- parseRequest ("GET " <> T.unpack url)
    let auth = B64.encode (TE.encodeUtf8 (client.email <> ":" <> client.token))
        req = setRequestHeader "Authorization" ["Basic " <> auth]
            $ setRequestHeader "Accept" ["application/json"]
              req0
    httpLBS req
  case result of
    Left (e :: SomeException) -> pure (Left ("failed to execute request: " <> T.pack (show e)))
    Right resp -> do
      let code = getResponseStatusCode resp
          bodyBytes = getResponseBody resp
      if code /= 200
        then pure (Left ("API returned status " <> T.pack (show code) <> ": "
                         <> TL.toStrict (TLE.decodeUtf8 bodyBytes)))
        else case A.decode bodyBytes >>= asObj of
          Nothing -> pure (Left "failed to decode response")
          Just raw -> pure (Right (parseIssue raw customFieldIds))

-- | Builds an IssueData from the decoded issue JSON.
parseIssue :: A.Object -> Map Text Text -> IssueData
parseIssue raw customFieldIds = IssueData
  { key = textAt raw "key"
  , summary = maybe "" (`textAt` "summary") fieldsObj
  , status = fromMaybe "" (nameOf "status")
  , priority = fromMaybe "" (nameOf "priority")
  , issueType = fromMaybe "" (nameOf "issuetype")
  , assignee = do
      f <- fieldsObj
      a <- objAt f "assignee"
      pure (textAt a "displayName")
  , labels = [ l | Just f <- [fieldsObj], A.String l <- arrAt f "labels" ]
  , createdAt = maybe "" (`textAt` "created") fieldsObj
  , updatedAt = maybe "" (`textAt` "updated") fieldsObj
  , comments =
      [ IssueComment
          { author = authorName
          , createdAt = created
          , body = "Comment by " <> authorName <> " on " <> T.take 10 created
          }
      | Just f <- [fieldsObj]
      , Just commentObj <- [objAt f "comment"]
      , c <- objsAt commentObj "comments"
      , let authorName = maybe "" (`textAt` "displayName") (objAt c "author")
      , let created = textAt c "created"
      ]
  , changelog =
      [ ChangelogEntry
          { author = maybe "" (`textAt` "displayName") (objAt history "author")
          , createdAt = textAt history "created"
          , field = textAt item "field"
          , from = textAt item "fromString"
          , to = textAt item "toString"
          }
      | Just cl <- [objAt raw "changelog"]
      , history <- objsAt cl "histories"
      , item <- objsAt history "items"
      ]
  , customFields = Map.fromList
      [ (displayName, extractFieldValue rawVal)
      | Just f <- [fieldsObj]
      , (displayName, fieldId) <- Map.toList customFieldIds
      , Just rawVal <- [f .? fieldId]
      ]
  }
  where
    fieldsObj = objAt raw "fields"
    nameOf k = do
      f <- fieldsObj
      o <- objAt f k
      pure (textAt o "name")

-- | Extracts a display value from a Jira field's raw JSON. Objects with
-- .value or .name use that; strings, numbers, arrays, nulls are direct.
extractFieldValue :: A.Value -> A.Value
extractFieldValue = \case
  A.String s -> A.String s
  A.Number n -> A.Number n
  A.Object o -> case o .? "value" of
    Just v -> v
    Nothing -> case o .? "name" of
      Just v -> v
      Nothing -> A.Object o
  A.Array a -> A.Array a
  _ -> A.Null

-- | Polls Jira for issue updates and emits events.
poll :: Db -> Config -> [WatchedResource] -> IO (Either Text ())
poll db cfg resources =
  case cfg.services.jira of
    Just j | j.token /= "" -> pollWith db j resources
    _ -> pure (Left "Jira token not configured")

pollWith :: Db -> JiraConfig -> [WatchedResource] -> IO (Either Text ())
pollWith db jiraCfg resources = do
  let logger = watcherLog "jira"
      client = JiraClient { baseUrl = jiraCfg.url, email = jiraCfg.email, token = jiraCfg.token }
  counts <- forM resources $ \resource -> do
    let issueKey = resource.resourceId
    logger ("Fetching issue " <> issueKey <> "...")
    fetched <- fetchIssue client issueKey jiraCfg.customFields
    case fetched of
      Left err -> do
        logger ("ERROR: failed to fetch issue " <> issueKey <> ": " <> err)
        let errBody = "Failed to fetch issue: " <> err
        emitWatcherError db "jira" ("Failed to fetch " <> issueKey) (Just errBody) resource
        recordWatcherError db "jira" errBody
        pure 0
      Right issueData -> do
        processed <- try (processIssue db jiraCfg issueData resource)
        case processed :: Either SomeException Int of
          Left err -> do
            logger ("ERROR: failed to process issue " <> issueKey <> ": " <> T.pack (show err))
            emitWatcherError db "jira" ("Error processing " <> issueKey)
              (Just ("Failed to process issue: " <> T.pack (show err))) resource
            pure 0
          Right count -> do
            now <- nowIso
            written <- try $ upsertResourceState db "jira" issueKey
                         (buildJiraStateJson issueData) issueData.updatedAt now
            case written :: Either SomeException () of
              Left err -> logger ("WARNING: failed to upsert resource state for " <> issueKey
                                  <> ": " <> T.pack (show err))
              Right () -> pure ()
            pure count
  logger ("Emitted " <> T.pack (show (sum counts)) <> " events")
  recordWatcherSuccess db "jira"
  pure (Right ())

-- | Processes a single Jira issue and emits events; returns the count emitted.
processIssue :: Db -> JiraConfig -> IssueData -> WatchedResource -> IO Int
processIssue db jiraCfg issue resource = do
  let logger = watcherLog "jira"
  cursor <- eventCursor db "jira" resource.resourceType resource.resourceId

  if cursor == ""
    then do
      let title = "Started watching issue: " <> issue.summary
          body = issue.key <> "\nStatus: " <> issue.status
      emitWatcherEvent db "jira" "watch_started" title (Just body) (latestTimestamp issue) Nothing Nothing resource
      logger ("Emitted watch_started for " <> resource.resourceId)
      pure 1
    else do
      commentCount <- fmap sum $ forM issue.comments $ \comment ->
        if comment.createdAt <= cursor
          then pure 0
          else do
            dup <- isDuplicate db "jira" resource.resourceType resource.resourceId
                     "jira_comment" comment.createdAt
            if dup then pure 0 else do
              let authorType = authorTypeFromUsername jiraCfg.botUsernames comment.author
              emitWatcherEvent db "jira" "jira_comment"
                ("Comment by " <> comment.author <> " on " <> issue.key) (Just comment.body)
                comment.createdAt (Just comment.author) (Just authorType) resource
              logger ("Emitted jira_comment for " <> resource.resourceId <> " by " <> comment.author)
              pure 1

      changelogCount <- fmap sum $ forM issue.changelog $ \entry ->
        if entry.createdAt <= cursor
          then pure 0
          else case entry.field of
            "status" -> emitChangelog "jira_status_change"
              (issue.key <> ": " <> entry.from <> " → " <> entry.to) entry
              ("Emitted jira_status_change for " <> resource.resourceId
               <> ": " <> entry.from <> " → " <> entry.to)
            "assignee" -> emitChangelog "jira_assigned"
              (issue.key <> " assigned to " <> entry.to) entry
              ("Emitted jira_assigned for " <> resource.resourceId <> ": " <> entry.to)
            "description" -> emitChangelog "jira_description_changed"
              (issue.key <> " description changed") entry
              ("Emitted jira_description_changed for " <> resource.resourceId)
            "labels" -> emitChangelog "jira_labels_changed"
              (labelChangeTitle issue.key entry.from entry.to) entry
              ("Emitted jira_labels_changed for " <> resource.resourceId)
            _ -> pure 0

      pure (commentCount + changelogCount)
  where
    emitChangelog eventType title entry logMsg = do
      dup <- isDuplicate db "jira" resource.resourceType resource.resourceId
               eventType entry.createdAt
      if dup then pure (0 :: Int) else do
        let authorType = authorTypeFromUsername jiraCfg.botUsernames entry.author
        emitWatcherEvent db "jira" eventType title Nothing
          entry.createdAt (Just entry.author) (Just authorType) resource
        watcherLog "jira" logMsg
        pure 1

-- | Determines if a username is a bot.
authorTypeFromUsername :: [Text] -> Text -> Text
authorTypeFromUsername botUsernames username =
  if username `elem` botUsernames then "bot" else "human"

-- | Creates a title for label changes showing +added and -removed.
labelChangeTitle :: Text -> Text -> Text -> Text
labelChangeTitle issueKey from to =
  let fromLabels = parseLabels from
      toLabels = parseLabels to
      added = [l | l <- toLabels, l `notElem` fromLabels]
      removed = [l | l <- fromLabels, l `notElem` toLabels]
      parts = [ "+" <> T.intercalate " +" added | not (null added) ]
           ++ [ "-" <> T.intercalate " -" removed | not (null removed) ]
  in if null parts
       then issueKey <> " labels changed"
       else issueKey <> " labels: " <> T.intercalate ", " parts

-- | Parses space-separated labels from a string.
parseLabels :: Text -> [Text]
parseLabels = T.words

-- | Whether a status is terminal (issue is done\/closed).
isTerminalStatus :: Text -> Bool
isTerminalStatus status =
  T.toCaseFold status `elem`
    map T.toCaseFold ["Done", "Resolved", "Won't Fix", "Closed", "Won't Do", "Cancelled"]

-- | Creates a resource relationship linking this issue to its epic.
linkEpic :: Db -> WatchedResource -> Text -> IO ()
linkEpic db resource epicKey = do
  relId <- newUuid
  now <- nowIso
  linkResources db ResourceRelationship
    { relId = relId
    , childType = resource.resourceType
    , childId = resource.resourceId
    , childUrl = if resource.resourceUrl == "" then Nothing else Just resource.resourceUrl
    , parentType = "jira"
    , parentId = epicKey
    , parentUrl = Nothing
    , relationship = "epic"
    , source = "jira"
    , createdAt = now
    }
  watcherLog "jira" ("Linked " <> resource.resourceId <> " to epic " <> epicKey)

-- | The most recent timestamp from an issue's comments and changelog.
latestTimestamp :: IssueData -> Text
latestTimestamp issue =
  let latest = maximum ("" : [c.createdAt | c <- issue.comments]
                           ++ [e.createdAt | e <- issue.changelog])
  in if latest == "" then "2000-01-01T00:00:00.000+0000" else latest

-- | Builds a JSON representation of a Jira issue's current state.
buildJiraStateJson :: IssueData -> Text
buildJiraStateJson issue =
  TL.toStrict $ TLE.decodeUtf8 $ A.encode $ A.Object $
    KM.fromList $
      [ ("summary", A.toJSON issue.summary)
      , ("status", A.toJSON issue.status)
      , ("priority", A.toJSON issue.priority)
      , ("assignee", A.toJSON issue.assignee)
      , ("issue_type", A.toJSON issue.issueType)
      , ("labels", A.toJSON issue.labels)
      , ("created_at", A.toJSON issue.createdAt)
      , ("updated_at", A.toJSON issue.updatedAt)
      ]
      ++ [ (Key.fromText k, v) | (k, v) <- Map.toList issue.customFields ]
