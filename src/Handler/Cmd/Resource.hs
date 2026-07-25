-- | Port of cmd/resource/: the `handler resource` subcommand tree
-- (history, related, link).
module Handler.Cmd.Resource (resourceCommand) where

import Control.Monad (forM_, when)
import Data.Aeson (object, toJSON, (.=))
import Data.Text (Text)
import qualified Data.Text as T
import Options.Applicative

import Handler.Cli.Common
import Handler.Db (close)
import Handler.Db.Events (Event(..), eventToJson)
import Handler.Db.Resources (ResourceRelationship(..), findRelatedSessions, linkResources, resourceHistory)
import Handler.Db.Sessions (Session(..))
import Handler.Util (newUuid, nowIso)
import Handler.Worktree (parseResourceId)

resourceCommand :: Mod CommandFields NamedCommand
resourceCommand = mkCommand "resource" "Resource relationship management" $ hsubparser $ mconcat
  [ command "history" (info (historyParser <**> helper)
      (progDesc "Get event history for a resource"))
  , command "related" (info (relatedParser <**> helper)
      (progDesc "Find sessions related to a session via shared resources"))
  , command "link" (info (linkParser <**> helper)
      (progDesc "Link two resources in a parent-child relationship"))
  ]

-- history ---------------------------------------------------------------

historyParser :: Parser CommandAction
historyParser = runHistory
  <$> strArgument (metavar "RESOURCE" <> help "resource ID (format: type:id)")
  <*> option auto (long "limit" <> value 50 <> help "maximum number of events to return")

runHistory :: Text -> Int -> Ctx -> IO ()
runHistory resourceArg limit ctx = do
  db <- openReadOnlyDb
  let (rType, rId) = parseResourceId resourceArg
  when (rType == "") $
    dieText ("invalid resource format (expected type:id): " <> resourceArg)
  events <- resourceHistory db rType rId limit
  close db
  if ctx.jsonOutput
    then printJson (toJSON (map eventToJson events))
    else if null events
      then printfT ["No events found for ", rType, ":", rId]
      else do
        printfT ["Event history for ", rType, ":", rId, ":\n"]
        forM_ events $ \e -> do
          printfT ["  [", e.ts, "] ", e.eventType]
          printfT ["    Title: ", e.title]
          case e.author of
            Just a | a /= "" -> printfT ["    Author: ", a]
            _ -> pure ()
          case e.sessionId of
            Just sid | sid /= "" -> printfT ["    Session: ", sid]
            _ -> pure ()
          case e.body of
            Just b | b /= "" ->
              -- Truncate long bodies
              printfT ["    Body: ", if T.length b > 100 then T.take 97 b <> "..." else b]
            _ -> pure ()
          putTextLn ""

-- related ---------------------------------------------------------------

relatedParser :: Parser CommandAction
relatedParser = runRelated
  <$> strOption (long "session" <> metavar "ID" <> help "session ID")

runRelated :: Text -> Ctx -> IO ()
runRelated sessionId ctx = do
  db <- openReadOnlyDb
  sessions <- findRelatedSessions db sessionId
  close db
  if ctx.jsonOutput
    -- The Go version marshals db.Session directly (no json tags), so the
    -- keys are the Go field names.
    then printJson $ toJSON
      [ object
          [ "SessionID" .= s.sessionId
          , "Harness" .= s.harness
          , "Repo" .= s.repo
          , "Branch" .= s.branch
          , "SessionName" .= s.sessionName
          , "PID" .= s.pid
          , "Status" .= s.status
          , "InboxMode" .= s.inboxMode
          , "AutoPollInterval" .= s.autoPollInterval
          , "Role" .= s.role
          , "TerminalType" .= s.terminalType
          , "TerminalID" .= s.terminalId
          , "CmuxWorkspaceID" .= s.cmuxWorkspaceId
          , "CmuxWorkspaceName" .= s.cmuxWorkspaceName
          , "CmuxWorkspaceColor" .= s.cmuxWorkspaceColor
          , "LastActive" .= s.lastActive
          , "LastPrompt" .= s.lastPrompt
          , "CWD" .= s.cwd
          , "RegisteredAt" .= s.registeredAt
          , "JSONLPath" .= s.jsonlPath
          ]
      | s <- sessions
      ]
    else if null sessions
      then putTextLn "No related sessions found"
      else do
        printfT ["Related sessions for ", sessionId, ":\n"]
        forM_ sessions $ \s -> do
          printfT ["  ", s.sessionId, " [", s.status, "]"]
          when (s.sessionName /= "") $ printfT ["    Name: ", s.sessionName]
          printfT ["    Repo: ", s.repo]
          printfT ["    Branch: ", s.branch]
          printfT ["    Last active: ", s.lastActive]
          putTextLn ""

-- link ------------------------------------------------------------------

data LinkOpts = LinkOpts
  { lChild        :: Text
  , lParent       :: Text
  , lRelationship :: Text
  , lChildUrl     :: Text
  , lParentUrl    :: Text
  , lSource       :: Text
  }

linkParser :: Parser CommandAction
linkParser = fmap runLink $ LinkOpts
  <$> strOption (long "child" <> metavar "RESOURCE" <> help "child resource ID (format: type:id)")
  <*> strOption (long "parent" <> metavar "RESOURCE" <> help "parent resource ID (format: type:id)")
  <*> strOption (long "relationship" <> metavar "REL" <> help "relationship type (e.g., epic_child)")
  <*> strOption (long "child-url" <> value "" <> help "child resource URL (optional)")
  <*> strOption (long "parent-url" <> value "" <> help "parent resource URL (optional)")
  <*> strOption (long "source" <> value "manual" <> help "source of the relationship")

runLink :: LinkOpts -> Ctx -> IO ()
runLink o ctx = do
  db <- openDb
  let (childType, childId) = parseResourceId o.lChild
  when (childType == "") $
    dieText ("invalid child resource format (expected type:id): " <> o.lChild)
  let (parentType, parentId) = parseResourceId o.lParent
  when (parentType == "") $
    dieText ("invalid parent resource format (expected type:id): " <> o.lParent)
  relId <- newUuid
  now <- nowIso
  linkResources db ResourceRelationship
    { relId = relId
    , childType = childType
    , childId = childId
    , childUrl = if o.lChildUrl == "" then Nothing else Just o.lChildUrl
    , parentType = parentType
    , parentId = parentId
    , parentUrl = if o.lParentUrl == "" then Nothing else Just o.lParentUrl
    , relationship = o.lRelationship
    , source = o.lSource
    , createdAt = now
    }
  close db
  if ctx.jsonOutput
    then printJson $ object
      [ "child" .= o.lChild
      , "parent" .= o.lParent
      , "relationship" .= o.lRelationship
      , "source" .= o.lSource
      , "status" .= ("linked" :: Text)
      ]
    else do
      printfT ["✓ Linked ", o.lChild, " → ", o.lParent, " (", o.lRelationship, ")"]
      printfT ["  Source: ", o.lSource]
