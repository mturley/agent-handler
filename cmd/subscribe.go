package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/worktree"
	"github.com/spf13/cobra"
)

var subscribeCmd = &cobra.Command{
	Use:   "subscribe",
	Short: "Subscribe a session to a resource",
	RunE:  runSubscribe,
}

var (
	subResource string
	subURL      string
)

func init() {
	subscribeCmd.GroupID = "agent"
	rootCmd.AddCommand(subscribeCmd)
	subscribeCmd.Flags().StringVar(&subResource, "resource", "", "resource ID (format: type:id, e.g., pr:owner/repo#42)")
	subscribeCmd.Flags().StringVar(&subURL, "url", "", "resource URL (optional)")
	subscribeCmd.Flags().String("session-id", "", "session ID (auto-detected if omitted)")
	subscribeCmd.Flags().Bool("primary", false, "mark as a primary resource for this worktree")
	subscribeCmd.MarkFlagRequired("resource")
}

func runSubscribe(cmd *cobra.Command, args []string) error {
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
	resourceType, resourceID := worktree.ParseResourceID(subResource)
	if resourceType == "" {
		return fmt.Errorf("invalid resource format (expected type:id): %s", subResource)
	}

	// Check if the corresponding service is configured
	service := config.ResourceTypeToService(resourceType)
	if service != "" {
		cfg, err := config.Read(config.DefaultPath())
		if err != nil {
			return fmt.Errorf("reading config: %w", err)
		}
		if !cfg.IsServiceConfigured(service) {
			return fmt.Errorf("%s is not configured. Run 'handler watcher auth %s' to set up API access", service, service)
		}
	}

	// Subscribe
	now := time.Now().UTC().Format(time.RFC3339)
	var urlPtr *string
	if subURL != "" {
		urlPtr = &subURL
	}

	err = d.Subscribe(db.Subscription{
		ID:           uuid.New().String(),
		SessionID:    sessionID,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		ResourceURL:  urlPtr,
		CreatedAt:    now,
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	// If URL provided, also sync to .worktree-resources
	if subURL != "" {
		resourcesPath := ".worktree-resources"
		primary, _ := cmd.Flags().GetBool("primary")
		if err := worktree.AppendResource(resourcesPath, subResource, subURL, primary); err != nil {
			return fmt.Errorf("failed to append to .worktree-resources: %w", err)
		}
	}

	// Output
	if jsonOutput {
		output := map[string]interface{}{
			"session_id":    sessionID,
			"resource_type": resourceType,
			"resource_id":   resourceID,
			"resource_url":  subURL,
			"status":        "subscribed",
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("✓ Subscribed session %s to %s:%s\n", sessionID, resourceType, resourceID)
		if subURL != "" {
			fmt.Printf("  URL: %s\n", subURL)
		}
	}

	return nil
}
