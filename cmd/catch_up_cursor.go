package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var catchUpCursorCmd = &cobra.Command{
	Use:   "catch-up-human-cursor",
	Short: "Advance the human cursor to match the agent cursor",
	RunE:  runCatchUpCursor,
}

func init() {
	catchUpCursorCmd.GroupID = "agent"
	rootCmd.AddCommand(catchUpCursorCmd)
}

func runCatchUpCursor(cmd *cobra.Command, args []string) error {
	sessionID, err := resolveSessionID(cmd)
	if err != nil {
		return err
	}

	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	if err := d.CatchUpHumanCursor(sessionID); err != nil {
		return fmt.Errorf("failed to advance human cursor: %w", err)
	}

	fmt.Println("✓ Human cursor advanced")
	return nil
}
