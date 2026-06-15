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

var unregSessionID string

func init() {
	rootCmd.AddCommand(unregisterCmd)
	unregisterCmd.Flags().StringVar(&unregSessionID, "session-id", "", "session ID")
	unregisterCmd.MarkFlagRequired("session-id")
}

func runUnregister(cmd *cobra.Command, args []string) error {
	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	// Get session to find PID before archiving
	session, err := d.GetSession(unregSessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	if session == nil {
		return fmt.Errorf("session not found: %s", unregSessionID)
	}

	// Archive the session
	archived, err := d.ArchiveSessions([]string{unregSessionID})
	if err != nil {
		return fmt.Errorf("failed to archive session: %w", err)
	}
	if archived == 0 {
		return fmt.Errorf("session not found: %s", unregSessionID)
	}

	// Soft-delete all subscriptions for this session
	subsDeleted, err := d.SoftDeleteSubscriptionsForSession(unregSessionID)
	if err != nil {
		return fmt.Errorf("failed to soft-delete subscriptions: %w", err)
	}

	// Emit session_end event
	now := time.Now().UTC().Format(time.RFC3339)
	eventID := uuid.New().String()
	err = d.InsertEvent(
		db.Event{
			ID:        eventID,
			TS:        now,
			Source:    "handler",
			SessionID: &unregSessionID,
			Type:      "session_end",
			Title:     fmt.Sprintf("Session %s ended", unregSessionID),
			Broadcast: false,
		},
		[]db.EventRecipient{
			{RecipientType: "session", RecipientValue: unregSessionID},
		},
		[]db.EventResource{},
	)
	if err != nil {
		return fmt.Errorf("failed to insert session_end event: %w", err)
	}

	// Clean up PID cache file
	sessionsDir := filepath.Join(filepath.Dir(db.DefaultPath()), "sessions")
	pidFile := filepath.Join(sessionsDir, strconv.Itoa(session.PID))
	os.Remove(pidFile) // Ignore errors — file may not exist

	// Output
	if jsonOutput {
		output := map[string]interface{}{
			"session_id":          unregSessionID,
			"status":              "archived",
			"subscriptions_deleted": subsDeleted,
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("✓ Unregistered session %s\n", unregSessionID)
		fmt.Printf("  Status: archived\n")
		if subsDeleted > 0 {
			fmt.Printf("  Subscriptions deleted: %d\n", subsDeleted)
		}
	}

	return nil
}
