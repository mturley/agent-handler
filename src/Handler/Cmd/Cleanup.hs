-- | Port of cmd/cleanup.go: archive dead sessions and optionally stale ones.
module Handler.Cmd.Cleanup (cleanupCommand) where

import Control.Monad (filterM, forM_, unless)
import Data.Aeson (object, (.=))
import Data.Char (isDigit)
import Data.Maybe (mapMaybe)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.IO as TIO
import Data.Time.Clock (diffUTCTime, getCurrentTime)
import Data.Time.Format (defaultTimeLocale, parseTimeM)
import Options.Applicative
import System.IO (hFlush, stdout)

import Handler.Cli.Common
import Handler.Db (close)
import Handler.Db.Peek (deletePeekStatesForSessions)
import Handler.Db.Sessions (Session(..), archiveSessions, listSessions)
import Handler.Discover (isSessionProcess)

data CleanupOpts = CleanupOpts
  { clStale :: Text
  , clYes   :: Bool
  }

cleanupCommand :: Mod CommandFields NamedCommand
cleanupCommand = mkCommand "cleanup" "Archive dead sessions and optionally stale ones" (runCleanup <$> opts)
  where
    opts = CleanupOpts
      <$> strOption (long "stale" <> value ""
            <> help "also archive sessions idle beyond this threshold (e.g., '14d')")
      <*> switch (long "yes" <> short 'y' <> help "skip confirmation prompt")

-- | Go's time.ParseDuration: sequences of <number><unit> with units
-- ns/us/ms/s/m/h. (Notably no 'd' — the flag help lies, same as Go.)
parseGoDuration :: Text -> Maybe Double
parseGoDuration t0
  | T.null t0 = Nothing
  | otherwise = go t0 0
  where
    go t acc
      | T.null t = Just acc
      | otherwise = do
          let (numPart, rest) = T.span (\c -> isDigit c || c == '.') t
          num <- if T.null numPart then Nothing else readMaybeT numPart
          let (unitPart, rest') = T.span (`elem` ("nsumh" :: String)) rest
          mult <- case unitPart of
            "ns" -> Just 1e-9
            "us" -> Just 1e-6
            "ms" -> Just 1e-3
            "s"  -> Just 1
            "m"  -> Just 60
            "h"  -> Just 3600
            _    -> Nothing
          go rest' (acc + num * mult)
    readMaybeT s = case reads (T.unpack s) of
      [(x, "")] -> Just x
      _ -> Nothing

runCleanup :: CleanupOpts -> Ctx -> IO ()
runCleanup o ctx = do
  db <- openDb
  sessions <- listSessions db False 1000 0

  staleDuration <-
    if o.clStale == ""
      then pure Nothing
      else case parseGoDuration o.clStale of
        Just d -> pure (Just d)
        Nothing -> dieText ("invalid --stale duration: " <> o.clStale)

  now <- getCurrentTime
  candidates <- fmap (mapMaybe id) $ mapM (classify now staleDuration) sessions

  if null candidates
    then do
      close db
      if ctx.jsonOutput
        then putTextLn "{\"archived\": 0}"
        else putTextLn "No sessions to archive"
    else do
      proceed <-
        if not o.clYes && not ctx.jsonOutput
          then do
            printfT ["Sessions to archive (", T.pack (show (length candidates)), "):"]
            forM_ candidates $ \(s, reason) ->
              printfT ["  ", candidateName s, " (", reason, ") — ", s.sessionId]
            putTextLn ""
            confirm "Archive these sessions?"
          else pure True
      if not proceed
        then do
          close db
          putTextLn "Aborted."
        else do
          let toArchive = [s.sessionId | (s, _) <- candidates]
          archived <- archiveSessions db toArchive
          deletePeekStatesForSessions db toArchive
          close db
          if ctx.jsonOutput
            then printJson $ object
              [ "archived" .= archived, "session_ids" .= toArchive ]
            else do
              printfT ["✓ Archived ", T.pack (show archived), " session(s)"]
              forM_ candidates $ \(s, reason) ->
                printfT ["  - ", candidateName s, " (", reason, ")"]
  where
    candidateName s = if s.sessionName == "" then T.take 8 s.sessionId else s.sessionName

    classify now staleDuration s = do
      alive <- isSessionProcess s.pid s.sessionId
      if not alive
        then pure (Just (s, "dead" :: Text))
        else case staleDuration of
          Just d
            | Just lastActive <- parseTimeM True defaultTimeLocale "%Y-%m-%dT%H:%M:%S%QZ" (T.unpack s.lastActive)
            , realToFrac (diffUTCTime now lastActive) > d
            -> pure (Just (s, "stale"))
          _ -> pure Nothing

-- | y/N confirmation prompt.
confirm :: Text -> IO Bool
confirm prompt = do
  TIO.putStr (prompt <> " [y/N] ")
  hFlush stdout
  answer <- getLine
  pure (answer `elem` ["y", "Y", "yes", "Yes"])
