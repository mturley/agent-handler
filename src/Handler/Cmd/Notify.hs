-- | Port of cmd/notify.go: terminal notification when unread count increases
-- (used by the statusline hook). Hidden command.
module Handler.Cmd.Notify (notifyCommand, dispatchNotification) where

import Control.Exception (IOException, try)
import Control.Monad (void, when)
import Data.Text (Text)
import qualified Data.Text as T
import Options.Applicative
import System.Directory (createDirectoryIfMissing, removeFile)
import System.FilePath (takeDirectory, (</>))
import Text.Read (readMaybe)

import Handler.Cli.Common
import Handler.Db (close, handlerHome)
import Handler.Db.Sessions (Session(..), getSession)
import Handler.Terminal (Backend(..), newBackend)

data NotifyOpts = NotifyOpts
  { nSession :: Text
  , nCount   :: Int
  , nMessage :: Text
  }

notifyCommand :: Mod CommandFields NamedCommand
notifyCommand = mkCommand "notify"
  "Send notification if unread count increased (used by statusline hook)"
  (runNotify <$> opts)
  where
    opts = NotifyOpts
      <$> strOption (long "session" <> metavar "ID" <> help "session ID")
      <*> option auto (long "count" <> metavar "N" <> help "current unread count")
      <*> strOption (long "message" <> value "" <> help "notification body")

runNotify :: NotifyOpts -> Ctx -> IO ()
runNotify o _ = do
  db <- openReadOnlyDb
  msession <- getSession db o.nSession
  case msession of
    Nothing -> pure ()
    Just session -> dispatchNotification session o.nCount o.nMessage
  close db

-- | Sends a terminal notification if the unread count increased since the
-- last notification for this session.
dispatchNotification :: Session -> Int -> Text -> IO ()
dispatchNotification session unreadCount message = do
  countFile <- notifiedCountPath session.sessionId
  if unreadCount == 0
    then void (try (removeFile countFile) :: IO (Either IOException ()))
    else when (session.terminalType /= "" && session.terminalId /= "") $ do
      cachedCount <- do
        result <- try (readFile countFile) :: IO (Either IOException String)
        pure $ case result of
          Right s -> maybe 0 id (readMaybe s)
          Left _  -> 0
      when (unreadCount > cachedCount) $
        case newBackend session.terminalType of
          Left _ -> pure ()
          Right backend -> do
            let body = if message == ""
                  then T.pack (show unreadCount) <> " unread event(s)"
                  else message
            backend.notify session.terminalId "handler" body
            backend.flash session.terminalId
            createDirectoryIfMissing True (takeDirectory countFile)
            writeFile countFile (show unreadCount)

notifiedCountPath :: Text -> IO FilePath
notifiedCountPath sessionId = do
  home <- handlerHome
  pure (home </> "sessions" </> (T.unpack sessionId <> ".notified_count"))
