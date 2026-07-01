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

var unsubResource string

func init() {
	unsubscribeCmd.GroupID = "agent"
	rootCmd.AddCommand(unsubscribeCmd)
	unsubscribeCmd.Flags().StringVar(&unsubResource, "resource", "", "resource ID (format: type:id)")
	unsubscribeCmd.Flags().String("session-id", "", "session ID (auto-detected if omitted)")
	unsubscribeCmd.Flags().Bool("persist", false, "also remove from .worktree-resources so future sessions won't auto-subscribe")
	unsubscribeCmd.MarkFlagRequired("resource")
}

func runUnsubscribe(cmd *cobra.Command, args []string) error {
	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	sessionID, err := resolveSessionID(cmd)
	if err != nil {
		return fmt.Errorf("could not determine session: %w", err)
	}

	// Parse resource
	resourceType, resourceID := worktree.ParseResourceID(unsubResource)
	if resourceType == "" {
		return fmt.Errorf("invalid resource format (expected type:id): %s", unsubResource)
	}

	// Unsubscribe
	err = d.Unsubscribe(sessionID, resourceType, resourceID)
	if err != nil {
		return fmt.Errorf("failed to unsubscribe: %w", err)
	}

	// Remove from .worktree-resources if requested
	persist, _ := cmd.Flags().GetBool("persist")
	if persist {
		resourcesPath := ".worktree-resources"
		if err := worktree.RemoveResource(resourcesPath, unsubResource); err != nil {
			return fmt.Errorf("failed to remove from .worktree-resources: %w", err)
		}
	}

	// Output
	if jsonOutput {
		output := map[string]interface{}{
			"session_id":    sessionID,
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
		fmt.Printf("✓ Unsubscribed session %s from %s:%s\n", sessionID, resourceType, resourceID)
	}

	return nil
}
