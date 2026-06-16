package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var tailCmd = &cobra.Command{
	Use:   "tail",
	Short: "Watch for new events in real-time",
	RunE:  runTail,
}

var (
	tailSource  string
	tailType    string
	tailSession string
)

func init() {
	tailCmd.GroupID = "human"
	rootCmd.AddCommand(tailCmd)
	tailCmd.Flags().StringVar(&tailSource, "source", "", "filter by event source")
	tailCmd.Flags().StringVar(&tailType, "type", "", "filter by event type")
	tailCmd.Flags().StringVar(&tailSession, "session", "", "filter by session ID")
}

func runTail(cmd *cobra.Command, args []string) error {
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	// Set initial cursor to now
	cursor := time.Now().UTC().Format(time.RFC3339)

	// Build filter
	filter := db.EventFilter{
		Limit: 100,
	}
	if tailSource != "" {
		filter.Source = &tailSource
	}
	if tailType != "" {
		filter.Type = &tailType
	}
	if tailSession != "" {
		filter.SessionID = &tailSession
	}

	// Setup signal handling for Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	fmt.Println("Watching for events... (Ctrl+C to stop)")
	if tailSource != "" {
		fmt.Printf("  Source filter: %s\n", tailSource)
	}
	if tailType != "" {
		fmt.Printf("  Type filter: %s\n", tailType)
	}
	if tailSession != "" {
		fmt.Printf("  Session filter: %s\n", tailSession)
	}
	fmt.Println()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sigChan:
			fmt.Println("\nStopped")
			return nil

		case <-ticker.C:
			// Query events since cursor
			filter.Since = &cursor
			events, err := d.QueryEvents(filter)
			if err != nil {
				return fmt.Errorf("failed to query events: %w", err)
			}

			// Events are in DESC order, reverse for chronological display
			for i := len(events) - 1; i >= 0; i-- {
				e := events[i]
				if jsonOutput {
					data, err := json.Marshal(e)
					if err != nil {
						return err
					}
					fmt.Println(string(data))
				} else {
					author := "-"
					if e.Author != nil {
						author = *e.Author
					}
					fmt.Printf("%s [%s] %s (by %s)\n", e.TS, e.Type, e.Title, author)
					if e.Body != nil && *e.Body != "" {
						fmt.Printf("  %s\n", *e.Body)
					}
				}

				// Update cursor to the latest event timestamp
				if e.TS > cursor {
					cursor = e.TS
				}
			}
		}
	}
}
