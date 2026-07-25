-- | Port of cmd/switch.go: switch to another session's cmux workspace and
-- surface. (Interactive selection uses a plain prompt; the Go version's
-- readline tab completion is not replicated.)
module Handler.Cmd.Switch (switchCommand) where

import Control.Exception (IOException, try)
import Control.Monad (void, when)
import Data.List (find)
import Data.Maybe (fromMaybe)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.IO as TIO
import Options.Applicative
import System.Environment (lookupEnv)
import System.Exit (ExitCode(..))
import System.IO (hFlush, hPutStr, hPutStrLn, isEOF, stderr, stdout)
import System.Process (readProcessWithExitCode)

import Handler.Cli.Common
import Handler.Cmd.Status (buildSessionStatuses, findSessionsAwaitingApproval, renderSessionList)
import Handler.Db (Db, close)
import Handler.Db.Sessions (Session(..), listSessions)
import Handler.Discover (isSessionProcess)

data SwitchOpts = SwitchOpts
  { swSession       :: Text
  , swFirstAwaiting :: Bool
  , swFirstUnread   :: Bool
  , swCloseCaller   :: Bool
  , swArgs          :: [Text]
  }

switchCommand :: Mod CommandFields NamedCommand
switchCommand = mkCommand "switch" "Switch to another session's cmux workspace and surface" (runSwitch <$> opts)
  where
    opts = SwitchOpts
      <$> strOption (long "session" <> value "" <> help "session name, ID, or branch to switch to")
      <*> switch (long "first-awaiting" <> short 'a' <> help "switch to the first session awaiting approval")
      <*> switch (long "first-unread" <> short 'u' <> help "switch to the first session with unread messages")
      <*> switch (long "close-caller" <> help "close the calling cmux surface after switching (for keyboard shortcut actions)")
      <*> many (strArgument (metavar "[session-name]"))

cmux :: [String] -> IO (Either Text ())
cmux args = do
  result <- try (readProcessWithExitCode "cmux" args "")
  pure $ case result :: Either IOException (ExitCode, String, String) of
    Right (ExitSuccess, _, _) -> Right ()
    Right (_, out, errOut) -> Left (T.pack (out <> errOut))
    Left e -> Left (T.pack (show e))

runSwitch :: SwitchOpts -> Ctx -> IO ()
runSwitch o _ctx = do
  selfSurface <- fromMaybe "" <$> lookupEnv "CMUX_SURFACE_ID"
  selfWorkspace <- fromMaybe "" <$> lookupEnv "CMUX_WORKSPACE_ID"
  when (selfSurface == "") $ dieText "not running inside cmux"

  -- Accept session as positional arg
  let target = if o.swSession == "" then T.intercalate " " o.swArgs else o.swSession

  db <- openReadOnlyDb

  session <-
    if | o.swFirstAwaiting -> findFirstAwaiting db `orCloseCaller` o
       | o.swFirstUnread -> findFirstWithUnread db selfSurface `orCloseCaller` o
       | target /= "" -> resolveSessionByTarget db target
       | otherwise -> interactiveSwitch db selfSurface `orCloseCaller` o

  when (session.terminalType /= "cmux") $
    dieText ("session " <> T.pack (show session.sessionName)
             <> " is not a cmux session (terminal type: " <> T.pack (show session.terminalType) <> ")")
  when (session.terminalId == "" || session.cmuxWorkspaceId == "") $
    dieText ("session " <> T.pack (show session.sessionName) <> " is missing cmux surface or workspace ID")

  -- Reposition the caller tab before closing so focus lands correctly.
  -- Same workspace: put it just before the target so closing advances to
  -- the target. Cross workspace: put it at index 0 so closing advances to
  -- the first real tab.
  when (o.swCloseCaller && selfSurface /= "" && T.pack selfSurface /= session.terminalId) $
    void $ if T.pack selfWorkspace == session.cmuxWorkspaceId
      then cmux ["reorder-surface", "--surface", selfSurface, "--before", T.unpack session.terminalId]
      else cmux ["reorder-surface", "--surface", selfSurface, "--index", "0"]

  cmux ["workspace", "select", "--workspace", T.unpack session.cmuxWorkspaceId] >>= \case
    Left out -> dieText ("cmux workspace select failed: " <> out)
    Right () -> pure ()
  cmux ["focus-panel", "--panel", T.unpack session.terminalId,
        "--workspace", T.unpack session.cmuxWorkspaceId] >>= \case
    Left out -> dieText ("cmux focus-panel failed: " <> out)
    Right () -> pure ()

  -- Close the caller surface after switching.
  when (o.swCloseCaller && selfSurface /= "" && T.pack selfSurface /= session.terminalId) $
    void $ cmux ["close-surface", "--surface", selfSurface, "--workspace", selfWorkspace]

  let name = if session.sessionName == "" then T.take 8 session.sessionId else session.sessionName
  printfT ["Switched to ", name]
  close db

-- | Runs a session-finding action; on failure with --close-caller, prompts
-- and closes the calling surface before exiting (closeCallerOnError).
orCloseCaller :: IO (Either Text Session) -> SwitchOpts -> IO Session
orCloseCaller act o =
  act >>= \case
    Right s -> pure s
    Left err -> do
      when o.swCloseCaller $ do
        selfSurface <- fromMaybe "" <$> lookupEnv "CMUX_SURFACE_ID"
        when (selfSurface /= "") $ do
          hPutStrLn stderr ("Error: " <> T.unpack err)
          hPutStr stderr "Press Enter to close..."
          eof <- isEOF
          _ <- if eof then pure "" else getLine
          selfWorkspace <- fromMaybe "" <$> lookupEnv "CMUX_WORKSPACE_ID"
          void $ cmux ["close-surface", "--surface", selfSurface, "--workspace", selfWorkspace]
      dieText err

findFirstWithUnread :: Db -> String -> IO (Either Text Session)
findFirstWithUnread db selfSurface = do
  unreads <- findSessionsWithUnreads db ""
  pure $ case find eligible unreads of
    Just s -> Right s
    Nothing -> Left "no cmux sessions with unread messages"
  where
    eligible s = s.terminalType == "cmux" && s.terminalId /= ""
      && s.cmuxWorkspaceId /= "" && s.terminalId /= T.pack selfSurface

findFirstAwaiting :: Db -> IO (Either Text Session)
findFirstAwaiting db = do
  awaiting <- findSessionsAwaitingApproval db
  pure $ case find eligible awaiting of
    Just s -> Right s
    Nothing -> Left "no cmux sessions awaiting approval"
  where
    eligible s = s.terminalType == "cmux" && s.terminalId /= "" && s.cmuxWorkspaceId /= ""

interactiveSwitch :: Db -> String -> IO (Either Text Session)
interactiveSwitch db selfSurface = do
  sessions <- listSessions db False 100 0

  -- Filter to switchable cmux sessions (exclude self, dead, non-cmux)
  candidates <- filterCandidates sessions
  let names = [displayName s | s <- candidates]

  if null candidates
    then pure (Left "no other cmux sessions to switch to")
    else do
      statuses <- buildSessionStatuses candidates Nothing
      renderSessionList candidates statuses
      putTextLn ""
      TIO.putStr "Switch to session \ESC[2m(tab-complete supported)\ESC[0m: "
      hFlush stdout
      eof <- isEOF
      if eof
        then pure (Left "cancelled")
        else do
          input <- T.strip <$> TIO.getLine
          if input == ""
            then pure (Left "no selection")
            else pure $ case find (matches input) (zip names candidates) of
              Just (_, s) -> Right s
              Nothing -> Left ("session " <> T.pack (show input) <> " not found")
  where
    displayName s = if s.sessionName == "" then T.take 8 s.sessionId else s.sessionName
    matches input (name, s) = name == input || s.sessionName == input || s.sessionId == input
    filterCandidates [] = pure []
    filterCandidates (s : rest)
      | s.terminalType /= "cmux" || s.terminalId == "" || s.cmuxWorkspaceId == "" = filterCandidates rest
      | s.terminalId == T.pack selfSurface = filterCandidates rest
      | otherwise = do
          aliveOk <- if s.pid > 0 then isSessionProcess s.pid s.sessionId else pure True
          if aliveOk
            then (s :) <$> filterCandidates rest
            else filterCandidates rest
