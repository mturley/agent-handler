package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
	"github.com/spf13/cobra"
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Show database health and statistics",
	RunE:  runHealth,
}

func init() {
	healthCmd.GroupID = "human"
	rootCmd.AddCommand(healthCmd)
}

func runHealth(cmd *cobra.Command, args []string) error {
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	// Get DB size
	var pageCount, pageSize int
	err = d.QueryRow("PRAGMA page_count").Scan(&pageCount)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to get page count: %w", err)
	}
	err = d.QueryRow("PRAGMA page_size").Scan(&pageSize)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to get page size: %w", err)
	}
	dbSize := pageCount * pageSize

	// Count sessions by status
	sessionCounts := make(map[string]int)
	rows, err := d.Query("SELECT status, COUNT(*) FROM sessions GROUP BY status")
	if err != nil {
		return fmt.Errorf("failed to count sessions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return fmt.Errorf("failed to scan session count: %w", err)
		}
		sessionCounts[status] = count
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating session counts: %w", err)
	}

	// Count total subscriptions (active and deleted)
	var totalSubs, activeSubs int
	err = d.QueryRow("SELECT COUNT(*) FROM subscriptions").Scan(&totalSubs)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to count subscriptions: %w", err)
	}
	err = d.QueryRow("SELECT COUNT(*) FROM subscriptions WHERE deleted_at IS NULL").Scan(&activeSubs)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to count active subscriptions: %w", err)
	}

	// Clean stale PID caches
	sessionsDir := filepath.Join(filepath.Dir(db.DefaultPath()), "sessions")
	staleCleaned := 0
	if _, err := os.Stat(sessionsDir); err == nil {
		staleCleaned, err = discover.CleanStalePIDCaches(sessionsDir)
		if err != nil {
			return fmt.Errorf("failed to clean stale PID caches: %w", err)
		}
	}

	if jsonOutput {
		output := map[string]interface{}{
			"db_size_bytes":      dbSize,
			"db_size_mb":         float64(dbSize) / (1024 * 1024),
			"session_counts":     sessionCounts,
			"total_subscriptions": totalSubs,
			"active_subscriptions": activeSubs,
			"stale_pid_caches_cleaned": staleCleaned,
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Println("Database Health")
		fmt.Println("─────────────────────────────")
		fmt.Printf("DB size: %.2f MB (%d bytes)\n", float64(dbSize)/(1024*1024), dbSize)
		fmt.Println("\nSessions by status:")
		for status, count := range sessionCounts {
			fmt.Printf("  %s: %d\n", status, count)
		}
		fmt.Printf("\nSubscriptions:\n")
		fmt.Printf("  Active: %d\n", activeSubs)
		fmt.Printf("  Total (incl. deleted): %d\n", totalSubs)
		if staleCleaned > 0 {
			fmt.Printf("\nCleaned %d stale PID cache(s)\n", staleCleaned)
		}
	}

	return nil
}
