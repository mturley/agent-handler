package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var heartbeatCmd = &cobra.Command{
	Use:   "heartbeat",
	Short: "Update last_active timestamp for a session",
	RunE:  runHeartbeat,
}

func init() {
	heartbeatCmd.GroupID = "agent"
	rootCmd.AddCommand(heartbeatCmd)
	heartbeatCmd.Flags().String("session-id", "", "session ID (auto-detected if omitted)")
}

func runHeartbeat(cmd *cobra.Command, args []string) error {
	sessionID, err := resolveSessionID(cmd)
	if err != nil {
		return fmt.Errorf("could not determine session: %w", err)
	}

	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	return d.BumpLastActive(sessionID, now)
}
