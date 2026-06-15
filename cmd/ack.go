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

var ackCmd = &cobra.Command{
	Use:   "ack",
	Short: "Acknowledge all unread events for a session",
	RunE:  runAck,
}

var ackSessionID string

func init() {
	rootCmd.AddCommand(ackCmd)
	ackCmd.Flags().StringVar(&ackSessionID, "session-id", "", "session ID (defaults to session from PID cache)")
}

func runAck(cmd *cobra.Command, args []string) error {
	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	sessionID := ackSessionID
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

	// Get unread count before acknowledging
	unreadCount, _, err := d.UnreadCountForSession(sessionID)
	if err != nil {
		return fmt.Errorf("failed to query unread count: %w", err)
	}

	// Advance cursor to now
	ts := time.Now().UTC().Format(time.RFC3339)
	if err := d.AdvanceCursor(sessionID, ts); err != nil {
		return fmt.Errorf("failed to advance cursor: %w", err)
	}

	if jsonOutput {
		output := map[string]interface{}{
			"acknowledged": unreadCount,
			"cursor":       ts,
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("✓ Acknowledged %d event(s)\n", unreadCount)
		fmt.Printf("  Cursor advanced to: %s\n", ts)
	}

	return nil
}
