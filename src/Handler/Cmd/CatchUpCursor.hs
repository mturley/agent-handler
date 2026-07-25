-- | Port of cmd/catch_up_cursor.go: advance the human cursor to match the
-- agent cursor.
module Handler.Cmd.CatchUpCursor (catchUpCursorCommand) where

import Options.Applicative

import Handler.Cli.Common
import Handler.Db (close)
import Handler.Db.Cursors (catchUpHumanCursor)

catchUpCursorCommand :: Mod CommandFields NamedCommand
catchUpCursorCommand = mkCommand "catch-up-human-cursor"
  "Advance the human cursor to match the agent cursor"
  (pure runCatchUpCursor)

runCatchUpCursor :: Ctx -> IO ()
runCatchUpCursor _ctx = do
  sessionId <- resolveSessionIdOpt Nothing
  db <- openDb
  catchUpHumanCursor db sessionId
  putTextLn "✓ Human cursor advanced"
  close db
