-- | Port of cmd/cmux_config.go: handler keyboard-shortcut actions in the
-- cmux config, managed via the cmux-settings helper.
module Handler.CmuxConfig
  ( handlerCmuxActions
  , handlerCmuxActionIDs
  , actionShortcut
  , cmuxConfigFilePath
  , findCmuxSettings
  , configureCmuxActions
  , hasCmuxActions
  , removeCmuxActions
  , CmuxShortcuts(..)
  , getCmuxShortcuts
  ) where

import Control.Monad (forM_, void)
import qualified Data.Aeson as A
import qualified Data.Aeson.Key as Key
import qualified Data.Aeson.KeyMap as KM
import qualified Data.ByteString.Lazy.Char8 as BL
import Data.Maybe (catMaybes, fromMaybe, isJust)
import Data.Text (Text)
import qualified Data.Text as T
import System.Directory (doesFileExist, executable, getHomeDirectory, getPermissions)
import System.Exit (ExitCode(..))
import System.FilePath ((</>))
import System.Process (readProcessWithExitCode)

import Handler.Cli.Common (printfT, putTextLn)

-- | The cmux actions installed by handler setup, keyed by action ID.
handlerCmuxActions :: [(Text, A.Value)]
handlerCmuxActions =
  [ ( "handler-switch-to-awaiting"
    , A.object
        [ "type" A..= ("command" :: Text)
        , "title" A..= ("agent-handler: Switch to Awaiting Session" :: Text)
        , "subtitle" A..= ("Jump to the first session awaiting approval" :: Text)
        , "command" A..= ("handler switch -a --close-caller" :: Text)
        , "shortcut" A..= ("cmd+shift+a" :: Text)
        , "palette" A..= True
        ] )
  , ( "handler-switch-to-session"
    , A.object
        [ "type" A..= ("command" :: Text)
        , "title" A..= ("agent-handler: Switch to Session" :: Text)
        , "subtitle" A..= ("Interactive session switcher with tab completion" :: Text)
        , "command" A..= ("handler switch --close-caller" :: Text)
        , "shortcut" A..= ("cmd+shift+s" :: Text)
        , "palette" A..= True
        ] )
  , ( "handler-switch-to-unread"
    , A.object
        [ "type" A..= ("command" :: Text)
        , "title" A..= ("agent-handler: Switch to Unread Session" :: Text)
        , "subtitle" A..= ("Jump to the first session with unread messages" :: Text)
        , "command" A..= ("handler switch -u --close-caller" :: Text)
        , "shortcut" A..= ("cmd+shift+i" :: Text)
        , "palette" A..= True
        ] )
  ]

handlerCmuxActionIDs :: [Text]
handlerCmuxActionIDs =
  ["handler-switch-to-awaiting", "handler-switch-to-session", "handler-switch-to-unread"]

-- | The \"shortcut\" field of a configured action.
actionShortcut :: Text -> Maybe Text
actionShortcut actionId = do
  A.Object o <- lookup actionId handlerCmuxActions
  A.String s <- KM.lookup "shortcut" o
  pure s

cmuxConfigFilePath :: IO FilePath
cmuxConfigFilePath = do
  home <- getHomeDirectory
  pure (home </> ".config" </> "cmux" </> "cmux.json")

-- | Locates the cmux-settings helper script; Nothing if not installed.
findCmuxSettings :: IO (Maybe FilePath)
findCmuxSettings = do
  home <- getHomeDirectory
  let candidates =
        [ home </> ".agents/skills/cmux-settings/scripts/cmux-settings"
        , home </> ".codex/skills/cmux-settings/scripts/cmux-settings"
        ]
  found <- mapM check candidates
  pure (foldr (\m acc -> if isJust m then m else acc) Nothing found)
  where
    check path = do
      exists <- doesFileExist path
      if not exists
        then pure Nothing
        else do
          perms <- getPermissions path
          pure (if executable perms then Just path else Nothing)

runSettings :: FilePath -> [String] -> IO (Either Text Text)
runSettings helper args = do
  (code, out, errOut) <- readProcessWithExitCode helper args ""
  pure $ case code of
    ExitSuccess -> Right (T.pack out)
    _           -> Left (T.strip (T.pack (out <> errOut)))

-- | Sets each handler action in the cmux config (always overwriting to pick
-- up updates), then reloads cmux config.
configureCmuxActions :: IO ()
configureCmuxActions = do
  findCmuxSettings >>= \case
    Nothing ->
      putTextLn "  \ESC[2mcmux-settings helper not found, skipping cmux action configuration\ESC[0m"
    Just helper -> go helper
  where
    go helper = setAll helper handlerCmuxActionIDs
    setAll helper [] = do
      void $ readProcessWithExitCode "cmux" ["reload-config"] ""
      let summary = T.intercalate ", "
            [ actionId <> " (" <> fromMaybe "" (actionShortcut actionId) <> ")"
            | actionId <- handlerCmuxActionIDs
            ]
      printfT ["  ✓ Configured cmux actions: ", summary]
    setAll helper (actionId : rest) = do
      let actionJson = BL.unpack (A.encode (fromMaybe A.Null (lookup actionId handlerCmuxActions)))
      result <- runSettings helper ["set", "actions." <> T.unpack actionId, actionJson]
      case result of
        Left err -> printfT ["  ⚠ Failed to set cmux action ", actionId, ": ", err]
        Right _  -> setAll helper rest

-- | Whether any handler actions are present in the cmux config.
hasCmuxActions :: IO Bool
hasCmuxActions =
  findCmuxSettings >>= \case
    Nothing -> pure False
    Just helper -> do
      result <- runSettings helper ["get", "actions"]
      pure $ case result of
        Right out | Just (A.Object existing) <- A.decode (BL.pack (T.unpack out)) ->
          any (\actionId -> KM.member (Key.fromText actionId) existing) handlerCmuxActionIDs
        _ -> False

-- | Removes the handler actions from the cmux config and reloads.
removeCmuxActions :: IO ()
removeCmuxActions =
  findCmuxSettings >>= \case
    Nothing -> pure ()
    Just helper -> do
      result <- runSettings helper ["get", "actions"]
      case result of
        Right out | Just (A.Object existing) <- A.decode (BL.pack (T.unpack out))
                  , any (\actionId -> KM.member (Key.fromText actionId) existing) handlerCmuxActionIDs -> do
          forM_ handlerCmuxActionIDs $ \actionId ->
            runSettings helper ["unset", "actions." <> T.unpack actionId]
          void $ readProcessWithExitCode "cmux" ["reload-config"] ""
          putTextLn "  ✓ Removed cmux actions (handler-switch-to-awaiting, handler-switch-to-session)"
        _ -> pure ()

-- | The configured keyboard shortcuts for handler cmux actions.
data CmuxShortcuts = CmuxShortcuts
  { switchToAwaiting :: Text
  , switchToSession  :: Text
  , switchToUnread   :: Text
  , focusBack        :: Text
  , focusForward     :: Text
  } deriving (Show, Eq)

-- | Reads the configured shortcuts from the cmux config.
-- Nothing if cmux-settings is unavailable or actions aren't configured.
getCmuxShortcuts :: IO (Maybe CmuxShortcuts)
getCmuxShortcuts =
  findCmuxSettings >>= \case
    Nothing -> pure Nothing
    Just helper -> do
      result <- runSettings helper ["get", "actions"]
      case result of
        Right out | not (T.null (T.strip out))
                  , Just (A.Object actions) <- A.decode (BL.pack (T.unpack out)) -> do
          let shortcutOf actionId = fromMaybe "" $ do
                A.Object a <- KM.lookup (Key.fromText actionId) actions
                A.String s <- KM.lookup "shortcut" a
                pure s
              awaiting = shortcutOf "handler-switch-to-awaiting"
              session = shortcutOf "handler-switch-to-session"
              unread = shortcutOf "handler-switch-to-unread"
          back0 <- binding helper "shortcuts.bindings.browserBack"
          forward0 <- binding helper "shortcuts.bindings.browserForward"
          -- Default cmux shortcuts if not explicitly configured
          let back = if back0 == "" then "cmd+[" else back0
              forward = if forward0 == "" then "cmd+]" else forward0
          pure $ if awaiting == "" && session == ""
            then Nothing
            else Just CmuxShortcuts
              { switchToAwaiting = awaiting
              , switchToSession = session
              , switchToUnread = unread
              , focusBack = back
              , focusForward = forward
              }
        _ -> pure Nothing
  where
    binding helper key = do
      result <- runSettings helper ["get", key]
      pure $ case result of
        Right out -> T.strip (T.dropAround (== '"') (T.strip out))
        Left _    -> ""
