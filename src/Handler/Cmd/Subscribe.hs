-- | Port of cmd/subscribe.go, cmd/unsubscribe.go, cmd/subscriptions.go:
-- resource subscription management.
module Handler.Cmd.Subscribe
  ( subscribeCommand
  , unsubscribeCommand
  , subscriptionsCommand
  ) where

import Control.Monad (forM_, when)
import Data.Aeson (object, toJSON, (.=))
import Data.Maybe (isJust)
import Data.Text (Text)
import Options.Applicative

import Handler.Cli.Common
import Handler.Config
  ( configDefaultPath, defaultResourceUrl, isServiceConfigured
  , readConfig, resourceTypeToService )
import Handler.Db (close)
import Handler.Db.Subscriptions
  ( Subscription(..), listSubscriptions, subscribe, subscriptionToJson, unsubscribe )
import Handler.Util (newUuid, nowIso)
import Handler.Worktree (appendResource, parseResourceId, removeResource)

data SubscribeOpts = SubscribeOpts
  { sResource  :: Text
  , sUrl       :: Text
  , sSessionId :: Maybe Text
  , sPrimary   :: Bool
  , sPersist   :: Bool
  }

subscribeCommand :: Mod CommandFields NamedCommand
subscribeCommand = mkCommand "subscribe" "Subscribe a session to a resource" (runSubscribe <$> opts)
  where
    opts = SubscribeOpts
      <$> strOption (long "resource" <> metavar "RES"
            <> help "resource ID (format: type:id, e.g., pr:owner/repo#42)")
      <*> strOption (long "url" <> value "" <> help "resource URL (optional)")
      <*> sessionIdOption
      <*> switch (long "primary" <> help "mark as a primary resource for this worktree")
      <*> switch (long "persist" <> help "also write to .worktree-resources so future sessions in this worktree auto-subscribe")

runSubscribe :: SubscribeOpts -> Ctx -> IO ()
runSubscribe o ctx = do
  db <- openDb
  sessionId <- resolveSessionIdOpt o.sSessionId

  let (resourceType, resourceId) = parseResourceId o.sResource
  when (resourceType == "") $
    dieText ("invalid resource format (expected type:id): " <> o.sResource)

  -- Check if the corresponding service is configured
  cfgPath <- configDefaultPath
  cfg <- readConfig cfgPath
  let service = resourceTypeToService resourceType
  when (service /= "" && not (isServiceConfigured cfg service)) $
    dieText (service <> " is not configured. Run 'handler watcher auth " <> service <> "' to set up API access")

  -- Auto-fill URL from config if not provided
  let url = if o.sUrl == "" then defaultResourceUrl cfg resourceType resourceId else o.sUrl

  now <- nowIso
  subId <- newUuid
  subscribe db Subscription
    { subId = subId
    , sessionId = sessionId
    , resourceType = resourceType
    , resourceId = resourceId
    , resourceUrl = if url == "" then Nothing else Just url
    , createdAt = now
    , deletedAt = Nothing
    }

  when (o.sPersist && url /= "") $
    appendResource ".worktree-resources" o.sResource url o.sPrimary
  close db

  if ctx.jsonOutput
    then printJson $ object
      [ "session_id" .= sessionId
      , "resource_type" .= resourceType
      , "resource_id" .= resourceId
      , "resource_url" .= url
      , "status" .= ("subscribed" :: Text)
      ]
    else do
      printfT ["✓ Subscribed session ", sessionId, " to ", resourceType, ":", resourceId]
      when (url /= "") $ printfT ["  URL: ", url]

data UnsubscribeOpts = UnsubscribeOpts
  { unResource  :: Text
  , unSessionId :: Maybe Text
  , unPersist   :: Bool
  }

unsubscribeCommand :: Mod CommandFields NamedCommand
unsubscribeCommand = mkCommand "unsubscribe" "Unsubscribe a session from a resource" (runUnsubscribe <$> opts)
  where
    opts = UnsubscribeOpts
      <$> strOption (long "resource" <> metavar "RES" <> help "resource ID (format: type:id)")
      <*> sessionIdOption
      <*> switch (long "persist" <> help "also remove from .worktree-resources so future sessions won't auto-subscribe")

runUnsubscribe :: UnsubscribeOpts -> Ctx -> IO ()
runUnsubscribe o ctx = do
  db <- openDb
  sessionId <- resolveSessionIdOpt o.unSessionId

  let (resourceType, resourceId) = parseResourceId o.unResource
  when (resourceType == "") $
    dieText ("invalid resource format (expected type:id): " <> o.unResource)

  unsubscribe db sessionId resourceType resourceId

  when o.unPersist $
    removeResource ".worktree-resources" o.unResource
  close db

  if ctx.jsonOutput
    then printJson $ object
      [ "session_id" .= sessionId
      , "resource_type" .= resourceType
      , "resource_id" .= resourceId
      , "status" .= ("unsubscribed" :: Text)
      ]
    else printfT ["✓ Unsubscribed session ", sessionId, " from ", resourceType, ":", resourceId]

subscriptionsCommand :: Mod CommandFields NamedCommand
subscriptionsCommand = mkCommand "subscriptions" "List subscriptions for a session"
  (runSubscriptions <$> sessionIdOption
    <*> switch (long "all" <> help "include deleted subscriptions"))

runSubscriptions :: Maybe Text -> Bool -> Ctx -> IO ()
runSubscriptions msid includeDeleted ctx = do
  db <- openReadOnlyDb
  sessionId <- resolveSessionIdOpt msid
  subs <- listSubscriptions db sessionId includeDeleted
  close db

  if ctx.jsonOutput
    then printJson (toJSON (map subscriptionToJson subs))
    else if null subs
      then putTextLn "No subscriptions found"
      else do
        printfT ["Subscriptions for session ", sessionId, ":\n"]
        forM_ subs $ \sub -> do
          let status = if isJust sub.deletedAt then "deleted" else "active"
          printfT ["  ", sub.resourceType, ":", sub.resourceId, " [", status, "]"]
          forM_ sub.resourceUrl $ \u -> printfT ["    URL: ", u]
          printfT ["    Created: ", sub.createdAt]
          forM_ sub.deletedAt $ \d -> printfT ["    Deleted: ", d]
          putTextLn ""
