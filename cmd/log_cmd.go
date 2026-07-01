package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Show event log for a session",
	RunE:  runLog,
}

var (
	logLimit      int
	logSince      string
	logGlobal     bool
	logSinceCursor bool
)

func init() {
	logCmd.GroupID = "human"
	rootCmd.AddCommand(logCmd)
	logCmd.Flags().String("session-id", "", "session ID (auto-detected if omitted)")
	logCmd.Flags().IntVar(&logLimit, "limit", 50, "maximum number of events to show")
	logCmd.Flags().StringVar(&logSince, "since", "", "show events since this timestamp (RFC3339)")
	logCmd.Flags().BoolVar(&logGlobal, "global", false, "show events from all sessions and watchers")
	logCmd.Flags().BoolVar(&logSinceCursor, "since-cursor", false, "show events since this session's cursor and advance it")
}

func runLog(cmd *cobra.Command, args []string) error {
	// Open DB read-write if we need to advance cursor, read-only otherwise
	var d *db.DB
	var err error
	if logSinceCursor {
		d, err = openDB()
	} else {
		d, err = openReadOnlyDB()
	}
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	var sessionID string
	var filter db.EventFilter

	// Build the event filter based on flags
	if logGlobal {
		// Global mode: no session filter
		filter.SessionID = nil
	} else {
		// Session-specific mode
		sessionID, err = resolveSessionID(cmd)
		if err != nil {
			return fmt.Errorf("could not determine session: %w", err)
		}
		filter.SessionID = &sessionID
	}

	// Handle --since-cursor
	if logSinceCursor {
		if logGlobal {
			// For global --since-cursor, we still need a session ID to track the cursor
			sessionID, err = resolveSessionID(cmd)
			if err != nil {
				return fmt.Errorf("could not determine session for cursor: %w", err)
			}
		}

		cursor, err := d.GetCursor(sessionID)
		if err != nil {
			return fmt.Errorf("failed to get cursor: %w", err)
		}

		if cursor == "" {
			// No cursor exists, default to last 24 hours
			defaultSince := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
			filter.Since = &defaultSince
		} else {
			filter.Since = &cursor
		}
	} else if logSince != "" {
		// Use explicit --since if provided
		filter.Since = &logSince
	}

	filter.Limit = logLimit

	events, err := d.QueryEvents(filter)
	if err != nil {
		return fmt.Errorf("failed to query events: %w", err)
	}

	// Advance cursor if --since-cursor was used
	if logSinceCursor && len(events) > 0 {
		if err := d.AdvanceBothCursors(sessionID, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return fmt.Errorf("failed to advance cursor: %w", err)
		}
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

		// Build session name cache if in global mode
		sessionNames := make(map[string]string)
		if logGlobal {
			for _, e := range events {
				if e.SessionID != nil {
					if _, exists := sessionNames[*e.SessionID]; !exists {
						session, err := d.GetSession(*e.SessionID)
						if err == nil && session != nil {
							sessionNames[*e.SessionID] = session.SessionName
						} else {
							sessionNames[*e.SessionID] = *e.SessionID
						}
					}
				}
			}
		}

		if logGlobal {
			fmt.Printf("Global event log (showing %d):\n\n", len(events))
		} else {
			fmt.Printf("Event log for session %s (showing %d):\n\n", sessionID, len(events))
		}

		// Events are in DESC order from query, so reverse for timeline display
		for i := len(events) - 1; i >= 0; i-- {
			e := events[i]
			author := "-"
			if e.Author != nil {
				author = *e.Author
			}

			// Format attribution prefix for global mode
			attribution := ""
			if logGlobal {
				if e.SessionID != nil {
					if name, ok := sessionNames[*e.SessionID]; ok {
						attribution = fmt.Sprintf("[%s] ", name)
					} else {
						attribution = fmt.Sprintf("[%s] ", *e.SessionID)
					}
				} else {
					// Watcher event
					attribution = fmt.Sprintf("[%s] ", e.Source)
				}
			}

			fmt.Printf("  %s%s [%s] %s\n", attribution, e.TS, e.Type, e.Title)
			fmt.Printf("  Author: %s | Source: %s\n", author, e.Source)
			if e.Body != nil && *e.Body != "" {
				fmt.Printf("  %s\n", *e.Body)
			}
			fmt.Println()
		}
	}

	return nil
}
