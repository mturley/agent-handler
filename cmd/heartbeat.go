package cmd

import (
	"fmt"
	"time"

	"github.com/mturley/agent-handler/discover"
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
	if err := d.BumpLastActive(sessionID, now); err != nil {
		return err
	}

	// Refresh session name if it changed
	session, err := d.GetSession(sessionID)
	if err != nil || session.JSONLPath == "" {
		return nil
	}
	currentName := discover.DiscoverSessionNameFast(session.JSONLPath)
	if currentName != "" && currentName != session.SessionName {
		d.Conn().Exec(`UPDATE sessions SET session_name = ? WHERE session_id = ?`, currentName, sessionID)
	}

	return nil
}
