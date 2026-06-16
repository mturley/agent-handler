package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mturley/agent-handler/cmd/resource"
	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var jsonOutput bool

var rootCmd = &cobra.Command{
	Use:   "handler",
	Short: "Agent handler CLI for managing Claude Code agent sessions",
	Long:  `A CLI tool backed by SQLite for managing Claude Code agent sessions.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "install" || cmd.Name() == "help" || cmd.Name() == "completion" {
			return nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		handlerDir := filepath.Join(home, ".agent-handler")
		if _, err := os.Stat(handlerDir); os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "agent-handler is not set up yet. Run 'handler install' to configure skills, hooks, and database.")
			os.Exit(1)
		}
		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output in JSON format")

	// Set up resource subcommand
	resource.JSONOutput = &jsonOutput
	rootCmd.AddCommand(resource.ResourceCmd)
}

func openDB() (*db.DB, error) {
	return db.Open(db.DefaultPath())
}

func openReadOnlyDB() (*db.DB, error) {
	return db.OpenReadOnly(db.DefaultPath())
}
