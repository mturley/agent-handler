package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mturley/agent-handler/discover"
	"github.com/spf13/cobra"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Archive dead sessions and optionally stale ones",
	RunE:  runCleanup,
}

var cleanupStale string

func init() {
	cleanupCmd.GroupID = "admin"
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().StringVar(&cleanupStale, "stale", "", "also archive sessions idle beyond this threshold (e.g., '14d')")
}

func runCleanup(cmd *cobra.Command, args []string) error {
	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	// List all active sessions
	sessions, err := d.ListSessions(false, 1000, 0)
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	var toArchive []string

	// Parse stale duration if provided
	var staleDuration time.Duration
	if cleanupStale != "" {
		staleDuration, err = time.ParseDuration(cleanupStale)
		if err != nil {
			return fmt.Errorf("invalid --stale duration: %w", err)
		}
	}

	for _, s := range sessions {
		// Check process liveness
		processAlive := discover.IsProcessAlive(s.PID)
		if !processAlive {
			toArchive = append(toArchive, s.SessionID)
			continue
		}

		// Check staleness if --stale provided
		if cleanupStale != "" {
			lastActive, err := time.Parse(time.RFC3339, s.LastActive)
			if err == nil {
				age := time.Since(lastActive)
				if age > staleDuration {
					toArchive = append(toArchive, s.SessionID)
				}
			}
		}
	}

	if len(toArchive) == 0 {
		if jsonOutput {
			fmt.Println(`{"archived": 0}`)
		} else {
			fmt.Println("No sessions to archive")
		}
		return nil
	}

	// Archive sessions
	archived, err := d.ArchiveSessions(toArchive)
	if err != nil {
		return fmt.Errorf("failed to archive sessions: %w", err)
	}

	if jsonOutput {
		output := map[string]interface{}{
			"archived":    archived,
			"session_ids": toArchive,
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("✓ Archived %d session(s)\n", archived)
		for _, sid := range toArchive {
			fmt.Printf("  - %s\n", sid)
		}
	}

	return nil
}
