-- | Port of terminal/: cmux and tmux backends, environment detection,
-- and needs-input heuristics.
module Handler.Terminal
  ( Backend(..)
  , detect
  , newBackend
  , needsInput
  , cmuxWorkspaceInfo
  ) where

import Control.Exception (IOException, try)
import qualified Data.Aeson as A
import qualified Data.Aeson.KeyMap as KM
import qualified Data.ByteString.Lazy.Char8 as BL
import Data.Maybe (fromMaybe, listToMaybe)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Vector as V
import System.Environment (lookupEnv)
import System.Exit (ExitCode(..))
import System.Process (readProcessWithExitCode)

-- | Interface for interacting with a terminal environment.
data Backend = Backend
  { capture :: Text -> Int -> IO (Either Text Text)
  , notify  :: Text -> Text -> Text -> IO (Either Text ())
  , flash   :: Text -> IO (Either Text ())
  , bell    :: Text -> IO (Either Text ())
  }

-- | Checks the current environment: (backendType, terminalID, workspaceID).
-- cmux first, then tmux.
detect :: IO (Text, Text, Text)
detect = do
  surfaceId <- lookupEnv "CMUX_SURFACE_ID"
  case surfaceId of
    Just sid | not (null sid) -> do
      wsId <- fromMaybe "" <$> lookupEnv "CMUX_WORKSPACE_ID"
      pure ("cmux", T.pack sid, T.pack wsId)
    _ -> do
      tmux <- lookupEnv "TMUX"
      case tmux of
        Just t | not (null t) -> do
          result <- try (readProcessWithExitCode "tmux" ["display-message", "-p", "#{pane_id}"] "")
          case result :: Either IOException (ExitCode, String, String) of
            Right (ExitSuccess, out, _)
              | paneId <- T.strip (T.pack out), not (T.null paneId)
              -> pure ("tmux", paneId, "")
            _ -> pure ("", "", "")
        _ -> pure ("", "", "")

-- | A Backend implementation for the given type.
newBackend :: Text -> Either Text Backend
newBackend = \case
  "cmux" -> Right cmuxBackend
  "tmux" -> Right tmuxBackend
  other  -> Left ("unsupported terminal backend: " <> T.pack (show other))

run :: String -> [String] -> IO (Either Text Text)
run cmd args = do
  result <- try (readProcessWithExitCode cmd args "")
  pure $ case result :: Either IOException (ExitCode, String, String) of
    Right (ExitSuccess, out, _) -> Right (T.pack out)
    Right (_, out, errOut) ->
      let msg = T.strip (T.pack (out <> errOut))
      in Left (if T.null msg then T.pack (cmd <> " failed") else msg)
    Left e -> Left (T.pack (show e))

cmuxBackend :: Backend
cmuxBackend = Backend
  { capture = \tid lines' -> do
      let args = ["capture-pane", "--surface", T.unpack tid, "--window", "window:1"]
                 ++ (if lines' > 0 then ["--lines", show lines'] else [])
      result <- run "cmux" args
      pure $ case result of
        Right out -> Right (T.dropWhileEnd (== '\n') out)
        Left msg  -> Left ("cmux capture-pane: " <> msg)
  , notify = \tid title body -> do
      let args = ["notify", "--surface", T.unpack tid, "--window", "window:1", "--title", T.unpack title]
                 ++ (if body /= "" then ["--body", T.unpack body] else [])
      fmap (const ()) <$> run "cmux" args
  , flash = \tid ->
      fmap (const ()) <$> run "cmux" ["trigger-flash", "--surface", T.unpack tid, "--window", "window:1"]
  , bell = \_ -> pure (Right ())  -- cmux has better notification primitives
  }

tmuxBackend :: Backend
tmuxBackend = Backend
  { capture = \tid lines' -> do
      let args = ["capture-pane", "-t", T.unpack tid, "-p"]
                 ++ (if lines' > 0 then ["-S", "-" <> show lines'] else [])
      result <- run "tmux" args
      pure $ case result of
        Right out -> Right (T.dropWhileEnd (== '\n') out)
        Left msg  -> Left ("tmux capture-pane failed: " <> msg)
  , notify = \_ _ _ -> pure (Right ())  -- tmux has no native notifications
  , flash = \_ -> pure (Right ())       -- tmux has no flash equivalent
  , bell = \tid ->
      fmap (const ()) <$> run "tmux" ["send-keys", "-t", T.unpack tid, "printf", "'\\a'", "Enter"]
  }

-- | Checks capture content for patterns indicating the session is waiting
-- for user input: (needsInput, reason).
needsInput :: Text -> (Bool, Text)
needsInput content =
  if any waiting (map T.strip (T.lines content))
    then (True, "awaiting approval")
    else (False, "")
  where
    waiting line =
      "Esc to cancel" `T.isInfixOf` line
      || "shift+tab to approve" `T.isInfixOf` line

-- | Resolves the workspace (name, color) for a cmux surface UUID.
-- Empty strings if resolution fails.
cmuxWorkspaceInfo :: Text -> IO (Text, Text)
cmuxWorkspaceInfo surfaceId = do
  idResult <- run "cmux" ["identify", "--surface", T.unpack surfaceId]
  case idResult of
    Left _ -> pure ("", "")
    Right idOut ->
      case decodeObj idOut >>= lookupObj "caller" >>= lookupStr "workspace_ref" of
        Nothing -> pure ("", "")
        Just wsRef -> do
          listResult <- run "cmux" ["workspace", "list", "--json"]
          case listResult of
            Left _ -> pure ("", "")
            Right listOut ->
              pure $ fromMaybe ("", "") $ do
                o <- decodeObj listOut
                A.Array wss <- KM.lookup "workspaces" o
                listToMaybe
                  [ (name, color)
                  | A.Object w <- V.toList wss
                  , lookupStr' "ref" w == Just wsRef
                  , let name = case lookupStr' "custom_title" w of
                          Just t | t /= "" -> t
                          _ -> fromMaybe "" (lookupStr' "title" w)
                  , let color = fromMaybe "" (lookupStr' "custom_color" w)
                  ]
  where
    decodeObj t = case A.decode (BL.pack (T.unpack t)) of
      Just (A.Object o) -> Just o
      _ -> Nothing
    lookupObj k o = case KM.lookup k o of
      Just (A.Object o') -> Just o'
      _ -> Nothing
    lookupStr k o = case KM.lookup k o of
      Just (A.String s) | s /= "" -> Just s
      _ -> Nothing
    lookupStr' k o = case KM.lookup k o of
      Just (A.String s) -> Just s
      _ -> Nothing
