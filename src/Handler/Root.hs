-- | Port of cmd/root.go: the handler CLI entry point and command registry.
module Handler.Root (main) where

import Control.Monad (unless)
import Options.Applicative

import Handler.Cli.Common (Ctx(..), NamedCommand, ensureSetup)
import Handler.Cmd.Ack (ackCommand)
import Handler.Cmd.Claude (claudeCommand)
import Handler.Cmd.Cleanup (cleanupCommand)
import Handler.Cmd.Configure (configureCommand)
import Handler.Cmd.Setup (setupCommand)
import Handler.Cmd.Ui (uiCommand)
import Handler.Cmd.Uninstall (uninstallCommand)
import Handler.Cmd.Cost (costCommand)
import Handler.Cmd.Debug (debugCommand)
import Handler.Cmd.Emit (emitCommand)
import Handler.Cmd.Health (healthCommand)
import Handler.Cmd.Resource (resourceCommand)
import Handler.Cmd.WatcherCli (watcherCommand)
import Handler.Cmd.Log (logCommand)
import Handler.Cmd.Notify (notifyCommand)
import Handler.Cmd.CatchUpCursor (catchUpCursorCommand)
import Handler.Cmd.Peek (peekCommand)
import Handler.Cmd.Register (registerCommand)
import Handler.Cmd.SessionName (sessionNameCommand)
import Handler.Cmd.Statusline (statuslineCommand)
import Handler.Cmd.Status (statusCommand)
import Handler.Cmd.Switch (switchCommand)
import Handler.Cmd.Triage (triageCommand)
import Handler.Cmd.Unregister (unregisterCommand)
import Handler.Cmd.UserPromptSubmit (userPromptSubmitCommand)
import Handler.Cmd.Watching (watchingCommand)
import Handler.Cmd.Query (queryCommand, schemaCommand)
import Handler.Cmd.Subscribe (subscribeCommand, subscriptionsCommand, unsubscribeCommand)
import Handler.Cmd.Tail (tailCommand)
import Handler.Cmd.Unread (unreadCommand)
import Handler.Cmd.Whoami (whoamiCommand)

-- | Commands that may run before 'handler setup' has been completed.
setupExemptCommands :: [String]
setupExemptCommands = ["setup", "help", "completion", "claude", "ui"]

rootParser :: Parser (Bool, NamedCommand)
rootParser = (,)
  <$> switch (long "json" <> help "output in JSON format")
  <*> hsubparser (mconcat commands)
  where
    commands =
      [ emitCommand
      , logCommand
      , ackCommand
      , unreadCommand
      , whoamiCommand
      , subscribeCommand
      , unsubscribeCommand
      , subscriptionsCommand
      , tailCommand
      , queryCommand
      , schemaCommand
      , notifyCommand
      , costCommand
      , healthCommand
      , cleanupCommand
      , debugCommand
      , watcherCommand
      , resourceCommand
      , setupCommand
      , uninstallCommand
      , claudeCommand
      , configureCommand
      , uiCommand
      , peekCommand
      , statuslineCommand
      , registerCommand
      , unregisterCommand
      , switchCommand
      , statusCommand
      , triageCommand
      , watchingCommand
      , sessionNameCommand
      , userPromptSubmitCommand
      , catchUpCursorCommand
      ]

main :: IO ()
main = do
  (json, (name, action)) <- customExecParser
    (prefs showHelpOnEmpty)
    (info (rootParser <**> helper)
      ( fullDesc
      <> header "handler — Agent handler CLI for managing Claude Code agent sessions"
      <> progDesc "A CLI tool backed by SQLite for managing Claude Code agent sessions."
      ))
  unless (name `elem` setupExemptCommands) ensureSetup
  action (Ctx json)
