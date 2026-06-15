package resource

import (
	"encoding/json"
	"fmt"

	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/worktree"
	"github.com/spf13/cobra"
)

var historyCmd = &cobra.Command{
	Use:   "history <resource-id>",
	Short: "Get event history for a resource",
	Args:  cobra.ExactArgs(1),
	RunE:  runHistory,
}

var historyLimit int

func init() {
	ResourceCmd.AddCommand(historyCmd)
	historyCmd.Flags().IntVar(&historyLimit, "limit", 50, "maximum number of events to return")
}

func runHistory(cmd *cobra.Command, args []string) error {
	resourceArg := args[0]

	d, err := db.OpenReadOnly(db.DefaultPath())
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	// Parse resource
	resourceType, resourceID := worktree.ParseResourceID(resourceArg)
	if resourceType == "" {
		return fmt.Errorf("invalid resource format (expected type:id): %s", resourceArg)
	}

	// Get history
	events, err := d.ResourceHistory(resourceType, resourceID, historyLimit)
	if err != nil {
		return fmt.Errorf("failed to get resource history: %w", err)
	}

	// Output
	if JSONOutput != nil && *JSONOutput {
		data, err := json.MarshalIndent(events, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		if len(events) == 0 {
			fmt.Printf("No events found for %s:%s\n", resourceType, resourceID)
			return nil
		}

		fmt.Printf("Event history for %s:%s:\n\n", resourceType, resourceID)
		for _, e := range events {
			fmt.Printf("  [%s] %s\n", e.TS, e.Type)
			fmt.Printf("    Title: %s\n", e.Title)
			if e.Author != nil && *e.Author != "" {
				fmt.Printf("    Author: %s\n", *e.Author)
			}
			if e.SessionID != nil && *e.SessionID != "" {
				fmt.Printf("    Session: %s\n", *e.SessionID)
			}
			if e.Body != nil && *e.Body != "" {
				// Truncate long bodies
				body := *e.Body
				if len(body) > 100 {
					body = body[:97] + "..."
				}
				fmt.Printf("    Body: %s\n", body)
			}
			fmt.Println()
		}
	}

	return nil
}
