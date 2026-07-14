package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
	"github.com/spf13/cobra"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Archive dead sessions and optionally stale ones",
	RunE:  runCleanup,
}

var (
	cleanupStale string
	cleanupYes   bool
)

func init() {
	cleanupCmd.GroupID = "admin"
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().StringVar(&cleanupStale, "stale", "", "also archive sessions idle beyond this threshold (e.g., '14d')")
	cleanupCmd.Flags().BoolVarP(&cleanupYes, "yes", "y", false, "skip confirmation prompt")
}

func runCleanup(cmd *cobra.Command, args []string) error {
	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	sessions, err := d.ListSessions(false, 1000, 0)
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	var staleDuration time.Duration
	if cleanupStale != "" {
		staleDuration, err = time.ParseDuration(cleanupStale)
		if err != nil {
			return fmt.Errorf("invalid --stale duration: %w", err)
		}
	}

	type candidate struct {
		session db.Session
		reason  string
	}
	var candidates []candidate

	for _, s := range sessions {
		processAlive := discover.IsSessionProcess(s.PID, s.SessionID)
		if !processAlive {
			candidates = append(candidates, candidate{session: s, reason: "dead"})
			continue
		}

		if cleanupStale != "" {
			lastActive, err := time.Parse(time.RFC3339, s.LastActive)
			if err == nil && time.Since(lastActive) > staleDuration {
				candidates = append(candidates, candidate{session: s, reason: "stale"})
			}
		}
	}

	if len(candidates) == 0 {
		if jsonOutput {
			fmt.Println(`{"archived": 0}`)
		} else {
			fmt.Println("No sessions to archive")
		}
		return nil
	}

	// Show candidates and confirm (unless --yes)
	if !cleanupYes {
		fmt.Printf("Sessions to archive (%d):\n", len(candidates))
		for _, c := range candidates {
			name := c.session.SessionName
			if name == "" {
				name = c.session.SessionID[:8]
			}
			fmt.Printf("  %s (%s) — %s\n", name, c.reason, c.session.SessionID)
		}
		fmt.Println()
		if !confirm("Archive these sessions?") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	var toArchive []string
	for _, c := range candidates {
		toArchive = append(toArchive, c.session.SessionID)
	}

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
		for _, c := range candidates {
			name := c.session.SessionName
			if name == "" {
				name = c.session.SessionID[:8]
			}
			fmt.Printf("  - %s (%s)\n", name, c.reason)
		}
	}

	return nil
}
