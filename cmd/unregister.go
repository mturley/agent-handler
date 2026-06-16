package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var unregisterCmd = &cobra.Command{
	Use:   "unregister",
	Short: "Unregister a Claude Code agent session",
	RunE:  runUnregister,
}

func init() {
	unregisterCmd.GroupID = "agent"
	rootCmd.AddCommand(unregisterCmd)
	unregisterCmd.Flags().String("session-id", "", "session ID (auto-detected if omitted)")
}

func runUnregister(cmd *cobra.Command, args []string) error {
	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	sessionID, err := resolveSessionID(cmd)
	if err != nil {
		return fmt.Errorf("could not determine session: %w", err)
	}

	// Get session to find PID before archiving
	session, err := d.GetSession(sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	if session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Archive the session
	archived, err := d.ArchiveSessions([]string{sessionID})
	if err != nil {
		return fmt.Errorf("failed to archive session: %w", err)
	}
	if archived == 0 {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Reset inbox mode to manual
	d.ConfigureSession(sessionID, "manual", nil)

	// Soft-delete all subscriptions for this session
	subsDeleted, err := d.SoftDeleteSubscriptionsForSession(sessionID)
	if err != nil {
		return fmt.Errorf("failed to soft-delete subscriptions: %w", err)
	}

	// Emit session_end event (not addressed to anyone — for audit trail only)
	now := time.Now().UTC().Format(time.RFC3339)
	d.InsertEvent(
		db.Event{
			ID:        uuid.New().String(),
			TS:        now,
			Source:    "handler",
			SessionID: &sessionID,
			Type:      "session_end",
			Title:     fmt.Sprintf("Session %s ended", sessionID),
		},
		nil, nil,
	)

	// Clean up PID cache file
	sessionsDir := filepath.Join(filepath.Dir(db.DefaultPath()), "sessions")
	pidFile := filepath.Join(sessionsDir, strconv.Itoa(session.PID))
	os.Remove(pidFile) // Ignore errors — file may not exist

	// Output
	if jsonOutput {
		output := map[string]interface{}{
			"session_id":          sessionID,
			"status":              "archived",
			"subscriptions_deleted": subsDeleted,
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("✓ Unregistered session %s\n", sessionID)
		fmt.Printf("  Status: archived\n")
		if subsDeleted > 0 {
			fmt.Printf("  Subscriptions deleted: %d\n", subsDeleted)
		}
	}

	return nil
}
