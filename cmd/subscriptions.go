package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var subscriptionsCmd = &cobra.Command{
	Use:   "subscriptions",
	Short: "List subscriptions for a session",
	RunE:  runSubscriptions,
}

var subsIncludeDeleted bool

func init() {
	subscriptionsCmd.GroupID = "human"
	rootCmd.AddCommand(subscriptionsCmd)
	subscriptionsCmd.Flags().String("session-id", "", "session ID (auto-detected if omitted)")
	subscriptionsCmd.Flags().BoolVar(&subsIncludeDeleted, "all", false, "include deleted subscriptions")
}

func runSubscriptions(cmd *cobra.Command, args []string) error {
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	sessionID, err := resolveSessionID(cmd)
	if err != nil {
		return fmt.Errorf("could not determine session: %w", err)
	}

	// List subscriptions
	subs, err := d.ListSubscriptions(sessionID, subsIncludeDeleted)
	if err != nil {
		return fmt.Errorf("failed to list subscriptions: %w", err)
	}

	// Output
	if jsonOutput {
		data, err := json.MarshalIndent(subs, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		if len(subs) == 0 {
			fmt.Println("No subscriptions found")
			return nil
		}

		fmt.Printf("Subscriptions for session %s:\n\n", sessionID)
		for _, sub := range subs {
			status := "active"
			if sub.DeletedAt != nil {
				status = "deleted"
			}
			fmt.Printf("  %s:%s [%s]\n", sub.ResourceType, sub.ResourceID, status)
			if sub.ResourceURL != nil {
				fmt.Printf("    URL: %s\n", *sub.ResourceURL)
			}
			fmt.Printf("    Created: %s\n", sub.CreatedAt)
			if sub.DeletedAt != nil {
				fmt.Printf("    Deleted: %s\n", *sub.DeletedAt)
			}
			fmt.Println()
		}
	}

	return nil
}
