package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var snoozeCmd = &cobra.Command{
	Use:   "snooze <event-id>",
	Short: "Snooze an event — re-delivers it to the inbox on the next check",
	Args:  cobra.ExactArgs(1),
	RunE:  runSnooze,
}

func init() {
	snoozeCmd.GroupID = "agent"
	rootCmd.AddCommand(snoozeCmd)
}

func runSnooze(cmd *cobra.Command, args []string) error {
	eventID := args[0]

	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := d.Conn().Exec(`UPDATE events SET ts = ? WHERE id = ?`, now, eventID)
	if err != nil {
		return fmt.Errorf("failed to snooze event: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("event not found: %s", eventID)
	}

	fmt.Printf("✓ Snoozed — will reappear in inbox on next check\n")
	return nil
}
