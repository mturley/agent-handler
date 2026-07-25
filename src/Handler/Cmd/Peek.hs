-- | Port of cmd/peek.go and cmd/peek_cache.go: terminal capture for
-- sessions, plus the cached needs-input scan used by the statusline.
module Handler.Cmd.Peek
  ( peekCommand
  , peekScanWithCache
  , findSessionsAwaitingApproval
  ) where

import Control.Monad (forM, forM_, when)
import Data.Aeson (toJSON, object, (.=))
import qualified Data.Map.Strict as Map
import Data.Maybe (catMaybes, mapMaybe)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.IO as TIO
import Options.Applicative

import Handler.Cli.Common
import Handler.Db (Db, close)
import Handler.Db.Peek (PeekState(..), listPeekStates, peekStatesAgeSeconds, upsertPeekState)
import Handler.Db.Sessions (Session(..), listSessions)
import Handler.Discover (isSessionProcess)
import Handler.Terminal (Backend(..), needsInput, newBackend)
import Handler.Util (nowIso)

data PeekOpts = PeekOpts
  { pSession       :: Text
  , pLines         :: Int
  , pListNeedInput :: Bool
  }

peekCommand :: Mod CommandFields NamedCommand
peekCommand = mkCommand "peek" "Capture terminal content for a session" (runPeek <$> opts)
  where
    opts = PeekOpts
      <$> strOption (long "session" <> value "" <> metavar "ID"
            <> help "session ID, name, or branch")
      <*> option auto (long "lines" <> value 0
            <> help "limit capture to last N lines (0 = full pane)")
      <*> switch (long "list-need-input"
            <> help "list all sessions waiting for user input")

runPeek :: PeekOpts -> Ctx -> IO ()
runPeek o ctx
  | o.pListNeedInput = runListNeedInput ctx
  | o.pSession == "" = dieText "required flag \"session\" not set"
  | otherwise = do
      db <- openReadOnlyDb
      session <- resolveSessionByTarget db o.pSession

      when (session.terminalType == "" || session.terminalId == "") $
        dieText "session is not peekable (not started via handler claude or not in a supported terminal)"

      running <- isSessionProcess session.pid session.sessionId
      when (not running) $
        dieText ("session process is not running (PID " <> T.pack (show session.pid) <> " not found)")

      backend <- either (\e -> dieText ("failed to create terminal backend: " <> e)) pure
        (newBackend session.terminalType)

      content <- backend.capture session.terminalId o.pLines >>= \case
        Left e -> dieText ("failed to capture terminal: " <> e)
        Right c -> pure c

      if ctx.jsonOutput
        then do
          capturedAt <- nowIso
          printJson $ object
            [ "session_id" .= session.sessionId
            , "session_name" .= session.sessionName
            , "terminal_type" .= session.terminalType
            , "captured_at" .= capturedAt
            , "content" .= content
            ]
        else do
          TIO.putStr content
          when (not (T.null content) && T.last content /= '\n') $ putTextLn ""
      close db

runListNeedInput :: Ctx -> IO ()
runListNeedInput ctx = do
  db <- openReadOnlyDb
  awaiting <- findSessionsAwaitingApproval db
  let results =
        [ object
            [ "session_id" .= s.sessionId
            , "session_name" .= s.sessionName
            , "reason" .= ("awaiting approval" :: Text)
            ]
        | s <- awaiting
        ]
  if ctx.jsonOutput
    then printJson (toJSON results)
    else if null awaiting
      then putTextLn "No sessions waiting for input."
      else forM_ awaiting $ \s -> do
        let name = if s.sessionName == "" then T.take 8 s.sessionId else s.sessionName
        printfT ["  ", name, " — awaiting approval"]
  close db

-- | Cached peek states if fresh (within maxAge seconds), otherwise a full
-- capture-pane scan over all peekable sessions, updating the cache.
peekScanWithCache :: Db -> Double -> IO [PeekState]
peekScanWithCache db maxAge = do
  age <- peekStatesAgeSeconds db
  if age <= maxAge
    then listPeekStates db
    else do
      sessions <- listSessions db False 1000 0
      now <- nowIso
      results <- forM sessions $ \s ->
        if s.terminalType == "" || s.terminalId == "" || s.role == "handler"
          then pure Nothing
          else do
            aliveOk <- if s.pid > 0 then isSessionProcess s.pid s.sessionId else pure True
            if not aliveOk
              then pure Nothing
              else case newBackend s.terminalType of
                Left _ -> pure Nothing
                Right backend ->
                  backend.capture s.terminalId 0 >>= \case
                    Left _ -> pure Nothing
                    Right content -> do
                      let (needs, reason) = needsInput content
                      upsertPeekState db s.sessionId content needs reason now
                      pure $ Just PeekState
                        { sessionId = s.sessionId
                        , content = content
                        , needsInput = needs
                        , reason = reason
                        , updatedAt = now
                        }
      pure (catMaybes results)

-- | Sessions that need input, using the peek cache (5s freshness).
findSessionsAwaitingApproval :: Db -> IO [Session]
findSessionsAwaitingApproval db = do
  states <- peekScanWithCache db 5
  sessions <- listSessions db False 1000 0
  let sessionMap = Map.fromList [(s.sessionId, s) | s <- sessions]
  pure $ mapMaybe
    (\ps -> if ps.needsInput then Map.lookup ps.sessionId sessionMap else Nothing)
    states
