package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Show event log for a session",
	RunE:  runLog,
}

var (
	logLimit int
	logSince string
)

func init() {
	logCmd.GroupID = "human"
	rootCmd.AddCommand(logCmd)
	logCmd.Flags().String("session-id", "", "session ID (auto-detected if omitted)")
	logCmd.Flags().IntVar(&logLimit, "limit", 50, "maximum number of events to show")
	logCmd.Flags().StringVar(&logSince, "since", "", "show events since this timestamp (RFC3339)")
}

func runLog(cmd *cobra.Command, args []string) error {
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	sessionID, err := resolveSessionID(cmd)
	if err != nil {
		return fmt.Errorf("could not determine session: %w", err)
	}

	filter := db.EventFilter{
		SessionID: &sessionID,
		Limit:     logLimit,
	}
	if logSince != "" {
		filter.Since = &logSince
	}

	events, err := d.QueryEvents(filter)
	if err != nil {
		return fmt.Errorf("failed to query events: %w", err)
	}

	if jsonOutput {
		data, err := json.MarshalIndent(events, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		if len(events) == 0 {
			fmt.Println("No events found")
			return nil
		}

		fmt.Printf("Event log for session %s (showing %d):\n\n", sessionID, len(events))
		// Events are in DESC order from query, so reverse for timeline display
		for i := len(events) - 1; i >= 0; i-- {
			e := events[i]
			author := "-"
			if e.Author != nil {
				author = *e.Author
			}
			fmt.Printf("  %s [%s] %s\n", e.TS, e.Type, e.Title)
			fmt.Printf("  Author: %s | Source: %s\n", author, e.Source)
			if e.Body != nil && *e.Body != "" {
				fmt.Printf("  %s\n", *e.Body)
			}
			fmt.Println()
		}
	}

	return nil
}
