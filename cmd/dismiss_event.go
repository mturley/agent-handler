package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var dismissEventCmd = &cobra.Command{
	Use:   "dismiss-event",
	Short: "Dismiss a single event from a session's inbox without advancing the cursor",
	RunE:  runDismissEvent,
}

func init() {
	dismissEventCmd.GroupID = "agent"
	rootCmd.AddCommand(dismissEventCmd)
	dismissEventCmd.Flags().String("session-id", "", "session ID (auto-detected if omitted)")
	dismissEventCmd.Flags().String("event", "", "event ID to dismiss (required)")
}

func runDismissEvent(cmd *cobra.Command, args []string) error {
	eventID, _ := cmd.Flags().GetString("event")
	if eventID == "" {
		return fmt.Errorf("--event is required")
	}

	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	sessionID, err := resolveSessionID(cmd)
	if err != nil {
		return fmt.Errorf("could not determine session: %w", err)
	}

	if err := d.DismissEvent(sessionID, eventID); err != nil {
		return fmt.Errorf("failed to dismiss event: %w", err)
	}

	fmt.Printf("✓ Dismissed event %s from session %s\n", eventID, sessionID)
	return nil
}
