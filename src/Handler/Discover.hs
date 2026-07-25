-- | Port of discover/: session ID resolution from env vars, PID cache,
-- and process ancestry.
module Handler.Discover
  ( writePidCache
  , readPidCache
  , cleanStalePidCaches
  , isProcessAlive
  , isSessionProcess
  , resolveSessionId
  ) where

import Control.Exception (IOException, try)
import Control.Monad (foldM)
import Data.List (isInfixOf)
import Data.Maybe (fromMaybe)
import Data.Text (Text)
import qualified Data.Text as T
import System.Directory (doesDirectoryExist, getHomeDirectory, listDirectory, removeFile)
import System.Environment (lookupEnv)
import System.Exit (ExitCode(..))
import System.FilePath ((</>))
import System.Posix.Process (getParentProcessID)
import System.Process (readProcessWithExitCode)
import Text.Read (readMaybe)

-- | Writes the session ID to a PID cache file.
writePidCache :: FilePath -> Int -> Text -> IO ()
writePidCache sessionsDir pid sessionId =
  writeFile (sessionsDir </> show pid) (T.unpack sessionId)

-- | Reads the session ID from a PID cache file; Nothing if unreadable.
readPidCache :: FilePath -> Int -> IO (Maybe Text)
readPidCache sessionsDir pid = do
  result <- try (readFile (sessionsDir </> show pid)) :: IO (Either IOException String)
  pure $ case result of
    Left _  -> Nothing
    Right s -> Just (T.strip (T.pack s))

-- | Removes PID cache files for processes that are no longer running.
cleanStalePidCaches :: FilePath -> IO Int
cleanStalePidCaches sessionsDir = do
  exists <- doesDirectoryExist sessionsDir
  if not exists
    then pure 0
    else do
      entries <- listDirectory sessionsDir
      foldM clean 0 entries
  where
    clean n entry = case readMaybe entry :: Maybe Int of
      Nothing -> pure n
      Just pid -> do
        alive <- isProcessAlive pid
        if alive
          then pure n
          else do
            removeFile (sessionsDir </> entry)
            pure (n + 1)

-- | Whether a process with the given PID is running (kill -0).
isProcessAlive :: Int -> IO Bool
isProcessAlive pid = do
  (code, _, _) <- readProcessWithExitCode "kill" ["-0", show pid] ""
  pure (code == ExitSuccess)

psCommand :: Int -> IO (Maybe String)
psCommand pid = do
  (code, out, _) <- readProcessWithExitCode "ps" ["-o", "command=", "-p", show pid] ""
  pure $ if code == ExitSuccess then Just out else Nothing

-- | Whether the process at the given PID belongs to the specified session.
-- Alive + unreadable command line → benefit of the doubt. Alive claude
-- process without the session ID on its command line → PID cache tiebreaker.
isSessionProcess :: Int -> Text -> IO Bool
isSessionProcess pid sessionId = do
  alive <- isProcessAlive pid
  if not alive
    then pure False
    else psCommand pid >>= \case
      Nothing -> pure True
      Just cmdline
        | not ("claude" `isInfixOf` cmdline) -> pure False
        | T.unpack sessionId `isInfixOf` cmdline -> pure True
        | otherwise -> do
            home <- lookupEnv "HANDLER_HOME" >>= \case
              Just h | not (null h) -> pure h
              _ -> do
                homeDir <- getHomeDirectory
                pure (homeDir </> ".agent-handler")
            cached <- readPidCache (home </> "data" </> "sessions") pid
            pure (cached == Just sessionId)

isClaudeProcess :: Int -> IO Bool
isClaudeProcess pid =
  maybe False ("claude" `isInfixOf`) <$> psCommand pid

getParentPid :: Int -> IO Int
getParentPid pid = do
  (code, out, _) <- readProcessWithExitCode "ps" ["-o", "ppid=", "-p", show pid] ""
  pure $ case code of
    ExitSuccess -> fromMaybe 0 (readMaybe (strip out))
    _           -> 0
  where strip = T.unpack . T.strip . T.pack

-- | Finds the current session ID: HANDLER_SESSION_ID env var, then the PID
-- cache for CLAUDE_PID, then claude processes in the ancestor tree (bounded
-- walk, cache entries trusted only for claude processes).
resolveSessionId :: FilePath -> IO (Either Text Text)
resolveSessionId handlerHome = do
  envId <- lookupEnv "HANDLER_SESSION_ID"
  case envId of
    Just sid | not (null sid) -> pure (Right (T.pack sid))
    _ -> do
      let sessionsDir = handlerHome </> "data" </> "sessions"
      claudePid <- lookupEnv "CLAUDE_PID"
      fromClaudePid <- case claudePid >>= readMaybe of
        Just pid -> readPidCache sessionsDir pid
        Nothing  -> pure Nothing
      case fromClaudePid of
        Just sid -> pure (Right sid)
        Nothing -> do
          ppid <- fromIntegral <$> getParentProcessID
          walk sessionsDir ppid (5 :: Int)
  where
    walk _ _ 0 = pure notFound
    walk sessionsDir pid n
      | pid <= 1 = pure notFound
      | otherwise = do
          claude <- isClaudeProcess pid
          cached <- if claude then readPidCache sessionsDir pid else pure Nothing
          case cached of
            Just sid -> pure (Right sid)
            Nothing -> do
              parent <- getParentPid pid
              walk sessionsDir parent (n - 1)
    notFound = Left "no claude process found in ancestor tree with a registered session"
