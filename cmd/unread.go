package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var unreadCmd = &cobra.Command{
	Use:   "unread",
	Short: "List unread events for a session",
	RunE:  runUnread,
}

func init() {
	unreadCmd.GroupID = "agent"
	rootCmd.AddCommand(unreadCmd)
	unreadCmd.Flags().String("session-id", "", "session ID (defaults to session from PID cache)")
	unreadCmd.Flags().Bool("ack", false, "acknowledge events after reading")
	unreadCmd.Flags().Bool("agent-only", false, "with --ack, advance only the agent cursor (not human cursor)")
	unreadCmd.Flags().Bool("count", false, "only print the unread count")
}

func runUnread(cmd *cobra.Command, args []string) error {
	ack, _ := cmd.Flags().GetBool("ack")

	var d *db.DB
	var err error
	if ack {
		d, err = openDB()
	} else {
		d, err = openReadOnlyDB()
	}
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	sessionID, err := resolveSessionID(cmd)
	if err != nil {
		return fmt.Errorf("could not determine session: %w", err)
	}

	countOnly, _ := cmd.Flags().GetBool("count")

	if countOnly {
		count, _, err := d.UnreadCountForSession(sessionID)
		if err != nil {
			return fmt.Errorf("failed to count unread events: %w", err)
		}
		fmt.Println(count)
		return nil
	}

	events, err := d.UnreadForSession(sessionID)
	if err != nil {
		return fmt.Errorf("failed to query unread events: %w", err)
	}

	if ack && len(events) > 0 {
		agentOnly, _ := cmd.Flags().GetBool("agent-only")
		ts := time.Now().UTC().Format(time.RFC3339)
		if agentOnly {
			d.AdvanceCursor(sessionID, ts)
		} else {
			d.AdvanceBothCursors(sessionID, ts)
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
			fmt.Println("No unread events")
			return nil
		}

		fmt.Printf("Unread events (%d):\n\n", len(events))
		for _, e := range events {
			author := "-"
			if e.Author != nil {
				author = *e.Author
			}
			fmt.Printf("  [%s] %s\n", e.Type, e.Title)
			fmt.Printf("  Author: %s | Time: %s\n", author, e.TS)
			if e.Body != nil && *e.Body != "" {
				fmt.Printf("  %s\n", *e.Body)
			}
			fmt.Println()
		}
	}

	return nil
}
