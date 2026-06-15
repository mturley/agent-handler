package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
	"github.com/spf13/cobra"
)

var unreadCmd = &cobra.Command{
	Use:   "unread",
	Short: "List unread events for a session",
	RunE:  runUnread,
}

var unreadSessionID string

func init() {
	rootCmd.AddCommand(unreadCmd)
	unreadCmd.Flags().StringVar(&unreadSessionID, "session-id", "", "session ID (defaults to session from PID cache)")
}

func runUnread(cmd *cobra.Command, args []string) error {
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	sessionID := unreadSessionID
	if sessionID == "" {
		// Try to detect from PID cache
		pid := os.Getpid()
		sessionsDir := filepath.Join(filepath.Dir(db.DefaultPath()), "sessions")
		detectedSessionID, err := discover.ReadPIDCache(sessionsDir, pid)
		if err != nil {
			return fmt.Errorf("--session-id is required (could not detect from PID cache: %w)", err)
		}
		sessionID = detectedSessionID
	}

	events, err := d.UnreadForSession(sessionID)
	if err != nil {
		return fmt.Errorf("failed to query unread events: %w", err)
	}

	if jsonOutput {
		data, err := json.MarshalIndent(events, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		if len(events) == 0 {
			fmt.Println("No unread events")
			return nil
		}

		fmt.Printf("Unread events (%d):\n\n", len(events))
		for _, e := range events {
			author := "-"
			if e.Author != nil {
				author = *e.Author
			}
			fmt.Printf("  [%s] %s\n", e.Type, e.Title)
			fmt.Printf("  Author: %s | Time: %s\n", author, e.TS)
			if e.Body != nil && *e.Body != "" {
				fmt.Printf("  %s\n", *e.Body)
			}
			fmt.Println()
		}
	}

	return nil
}
