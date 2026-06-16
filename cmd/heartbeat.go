package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
	"github.com/spf13/cobra"
)

var heartbeatCmd = &cobra.Command{
	Use:   "heartbeat",
	Short: "Update last_active timestamp for a session",
	RunE:  runHeartbeat,
}

var hbSessionID string

func init() {
	heartbeatCmd.GroupID = "agent"
	rootCmd.AddCommand(heartbeatCmd)
	heartbeatCmd.Flags().StringVar(&hbSessionID, "session-id", "", "session ID (optional, reads from PID cache if omitted)")
}

func runHeartbeat(cmd *cobra.Command, args []string) error {
	sessionID := hbSessionID

	// If session ID not provided, read from PID cache using $PPID
	if sessionID == "" {
		ppidStr := os.Getenv("PPID")
		if ppidStr == "" {
			return fmt.Errorf("--session-id not provided and $PPID not set")
		}
		ppid, err := strconv.Atoi(ppidStr)
		if err != nil {
			return fmt.Errorf("invalid $PPID: %w", err)
		}

		sessionsDir := filepath.Join(filepath.Dir(db.DefaultPath()), "sessions")
		sessionID, err = discover.ReadPIDCache(sessionsDir, ppid)
		if err != nil {
			return fmt.Errorf("failed to read PID cache for PPID %d: %w", ppid, err)
		}
	}

	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	err = d.BumpLastActive(sessionID, now)
	if err != nil {
		return fmt.Errorf("failed to bump last_active: %w", err)
	}

	// Silent success for speed
	return nil
}
