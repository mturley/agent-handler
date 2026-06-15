package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/mturley/agent-handler/worktree"
	"github.com/spf13/cobra"
)

var unsubscribeCmd = &cobra.Command{
	Use:   "unsubscribe",
	Short: "Unsubscribe a session from a resource",
	RunE:  runUnsubscribe,
}

var (
	unsubResource  string
	unsubSessionID string
)

func init() {
	rootCmd.AddCommand(unsubscribeCmd)
	unsubscribeCmd.Flags().StringVar(&unsubResource, "resource", "", "resource ID (format: type:id)")
	unsubscribeCmd.Flags().StringVar(&unsubSessionID, "session-id", "", "session ID")
	unsubscribeCmd.MarkFlagRequired("resource")
	unsubscribeCmd.MarkFlagRequired("session-id")
}

func runUnsubscribe(cmd *cobra.Command, args []string) error {
	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	// Parse resource
	resourceType, resourceID := worktree.ParseResourceID(unsubResource)
	if resourceType == "" {
		return fmt.Errorf("invalid resource format (expected type:id): %s", unsubResource)
	}

	// Unsubscribe
	err = d.Unsubscribe(unsubSessionID, resourceType, resourceID)
	if err != nil {
		return fmt.Errorf("failed to unsubscribe: %w", err)
	}

	// Sync to .worktree-resources
	resourcesPath := ".worktree-resources"
	if err := worktree.RemoveResource(resourcesPath, unsubResource); err != nil {
		return fmt.Errorf("failed to remove from .worktree-resources: %w", err)
	}

	// Output
	if jsonOutput {
		output := map[string]interface{}{
			"session_id":    unsubSessionID,
			"resource_type": resourceType,
			"resource_id":   resourceID,
			"status":        "unsubscribed",
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("✓ Unsubscribed session %s from %s:%s\n", unsubSessionID, resourceType, resourceID)
	}

	return nil
}
