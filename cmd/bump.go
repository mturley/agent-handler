package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var bumpCmd = &cobra.Command{
	Use:   "bump <event-id>",
	Short: "Bump an event's timestamp to now (re-delivers it to the inbox)",
	Args:  cobra.ExactArgs(1),
	RunE:  runBump,
}

func init() {
	bumpCmd.GroupID = "agent"
	rootCmd.AddCommand(bumpCmd)
}

func runBump(cmd *cobra.Command, args []string) error {
	eventID := args[0]

	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := d.Conn().Exec(`UPDATE events SET ts = ? WHERE id = ?`, now, eventID)
	if err != nil {
		return fmt.Errorf("failed to bump event: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("event not found: %s", eventID)
	}

	fmt.Printf("✓ Bumped event %s to %s\n", eventID, now)
	return nil
}
