package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/worktreeinterop"
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
	subscribeCmd.Flags().Bool("related", false, "propagate to worktree as a related (not primary) resource")
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
	resourceType, resourceID := worktreeinterop.ParseResourceID(subResource)
	if resourceType == "" {
		return fmt.Errorf("invalid resource format (expected type:id): %s", subResource)
	}

	// Check if the corresponding service is configured
	service := config.ResourceTypeToService(resourceType)
	if service != "" {
		if !config.ServiceConfiguredForWatching(service) {
			return fmt.Errorf("%s is not configured. Run 'handler watcher auth %s' to set up API access", service, service)
		}
	}

	// Auto-fill URL from config if not provided
	if subURL == "" {
		cfg, cfgErr := config.Read(config.DefaultPath())
		if cfgErr == nil && cfg != nil {
			subURL = cfg.DefaultResourceURL(resourceType, resourceID)
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

	// Propagate to the worktree CLI if present (best-effort).
	if worktreeinterop.Available() {
		related, _ := cmd.Flags().GetBool("related")
		cwd, _ := os.Getwd()
		u := ""
		if urlPtr != nil {
			u = *urlPtr
		}
		if err := worktreeinterop.AddResource(cwd, worktreeinterop.Resource{
			Type: resourceType, ID: resourceID, URL: u,
		}, related); err != nil {
			// soft degradation: subscribe already succeeded
			fmt.Fprintf(os.Stderr, "warning: could not propagate to worktree: %v\n", err)
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
