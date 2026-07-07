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
	heartbeatCmd.Flags().Bool("catch-up-human-cursor", false, "advance human cursor to match agent cursor (auto inbox mode)")
	heartbeatCmd.Flags().String("session-name", "", "update session display name")
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

	// Catch up human cursor if requested (auto inbox mode)
	catchUp, _ := cmd.Flags().GetBool("catch-up-human-cursor")
	if catchUp {
		session, err := d.GetSession(sessionID)
		if err == nil && session != nil && session.InboxMode == "auto" {
			d.CatchUpHumanCursor(sessionID)
		}
	}

	// Update session name if provided via flag
	nameFlag, _ := cmd.Flags().GetString("session-name")
	if nameFlag != "" {
		session, err := d.GetSession(sessionID)
		if err == nil && session != nil && session.SessionName != nameFlag {
			d.Conn().Exec(`UPDATE sessions SET session_name = ? WHERE session_id = ?`, nameFlag, sessionID)
		}
	}

	return nil
}
