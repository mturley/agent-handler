package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var ackCmd = &cobra.Command{
	Use:   "ack",
	Short: "Acknowledge all unread events for a session",
	RunE:  runAck,
}

func init() {
	ackCmd.GroupID = "agent"
	rootCmd.AddCommand(ackCmd)
	ackCmd.Flags().String("session-id", "", "session ID (auto-detected if omitted)")
}

func runAck(cmd *cobra.Command, args []string) error {
	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	sessionID, err := resolveSessionID(cmd)
	if err != nil {
		return fmt.Errorf("could not determine session: %w", err)
	}

	unreadCount, _, err := d.UnreadCountForSession(sessionID)
	if err != nil {
		return fmt.Errorf("failed to query unread count: %w", err)
	}

	ts := time.Now().UTC().Format(time.RFC3339)
	if err := d.AdvanceBothCursors(sessionID, ts); err != nil {
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
