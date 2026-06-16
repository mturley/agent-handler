package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
	"github.com/spf13/cobra"
)

var unreadCmd = &cobra.Command{
	Use:   "unread",
	Short: "List unread events for a session",
	RunE:  runUnread,
}

func init() {
	rootCmd.AddCommand(unreadCmd)
	unreadCmd.Flags().String("session-id", "", "session ID (defaults to session from PID cache)")
	unreadCmd.Flags().Bool("ack", false, "acknowledge events after reading")
}

func runUnread(cmd *cobra.Command, args []string) error {
	ack, _ := cmd.Flags().GetBool("ack")

	var d *db.DB
	var err error
	if ack {
		d, err = openDB()
	} else {
		d, err = openReadOnlyDB()
	}
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	sessionID, _ := cmd.Flags().GetString("session-id")
	if sessionID == "" {
		pid := os.Getpid()
		sessionsDir := filepath.Join(filepath.Dir(db.DefaultPath()), "sessions")
		sessionID, err = discover.ReadPIDCache(sessionsDir, pid)
		if err != nil {
			return fmt.Errorf("--session-id is required (could not detect from PID cache: %w)", err)
		}
	}

	events, err := d.UnreadForSession(sessionID)
	if err != nil {
		return fmt.Errorf("failed to query unread events: %w", err)
	}

	if ack && len(events) > 0 {
		d.AdvanceCursor(sessionID, time.Now().UTC().Format(time.RFC3339))
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
