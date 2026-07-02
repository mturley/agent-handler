package cmd

import (
	"fmt"
	"os"

	"github.com/mturley/agent-handler/cmd/resource"
	"github.com/mturley/agent-handler/cmd/watcher"
	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
	"github.com/spf13/cobra"
)

var jsonOutput bool

var rootCmd = &cobra.Command{
	Use:   "handler",
	Short: "Agent handler CLI for managing Claude Code agent sessions",
	Long:  `A CLI tool backed by SQLite for managing Claude Code agent sessions.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "setup" || cmd.Name() == "help" || cmd.Name() == "completion" || cmd.Name() == "claude" {
			return nil
		}
		dbPath := db.DefaultPath()
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "agent-handler is not set up yet. Run 'handler setup' to configure skills, hooks, and database.")
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

	rootCmd.AddGroup(
		&cobra.Group{ID: "human", Title: "Commands for humans:"},
		&cobra.Group{ID: "agent", Title: "Commands for agents (used by hooks and skills):"},
		&cobra.Group{ID: "admin", Title: "Admin:"},
	)

	// Set up resource subcommand
	resource.JSONOutput = &jsonOutput
	resource.ResourceCmd.GroupID = "human"
	rootCmd.AddCommand(resource.ResourceCmd)

	// Set up watcher subcommand
	watcher.JSONOutput = &jsonOutput
	watcher.WatcherCmd.GroupID = "human"
	rootCmd.AddCommand(watcher.WatcherCmd)
}

func openDB() (*db.DB, error) {
	return db.Open(db.DefaultPath())
}

func openReadOnlyDB() (*db.DB, error) {
	return db.OpenReadOnly(db.DefaultPath())
}

func resolveSessionID(cmd *cobra.Command) (string, error) {
	sessionID, _ := cmd.Flags().GetString("session-id")
	if sessionID != "" {
		return sessionID, nil
	}
	return discover.ResolveSessionID(db.HandlerHome())
}

// resolveSessionByTarget finds a session by UUID, name, or branch.
func resolveSessionByTarget(d *db.DB, target string) (*db.Session, error) {
	// Try exact session ID match first
	session, err := d.GetSession(target)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}
	if session != nil {
		return session, nil
	}

	// Try session name match
	sessions, err := d.ListSessions(false, 100, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	var matches []*db.Session
	for i := range sessions {
		s := &sessions[i]
		if s.SessionName == target || s.Branch == target {
			matches = append(matches, s)
		}
	}

	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("multiple sessions match %q — use full session ID", target)
	}

	return nil, fmt.Errorf("session %q not found", target)
}
