package watcher

import (
	"fmt"
	"strings"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/watcher"
	"github.com/mturley/agent-handler/watcher/github"
	"github.com/spf13/cobra"
)

var runResources string

func init() {
	WatcherCmd.AddCommand(runCmd)
	runCmd.Flags().StringVar(&runResources, "resources", "", "Comma-separated resource IDs for catch-up mode")
}

var runCmd = &cobra.Command{
	Use:   "run <name>",
	Short: "Run a watcher once (one-shot poll)",
	Long: `Run a watcher once to poll for new events.

Valid watchers: github, jira

Use --resources to poll specific resources instead of all active subscriptions.
Example: handler watcher run github --resources "owner/repo#123,owner/repo#456"`,
	Args: cobra.ExactArgs(1),
	RunE: runWatcher,
}

func runWatcher(cmd *cobra.Command, args []string) error {
	name := strings.ToLower(args[0])

	// Validate watcher name
	if name != "github" && name != "jira" {
		return fmt.Errorf("unknown watcher: %s (must be 'github' or 'jira')", name)
	}

	// Check service is configured
	configPath := config.DefaultPath()
	cfg, err := config.Read(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	if !cfg.IsServiceConfigured(name) {
		return fmt.Errorf("service %q is not configured. Run 'handler watcher auth %s' first", name, name)
	}

	// Open database
	dbPath := db.DefaultPath()
	d, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	// Determine resources to poll
	var resources []watcher.Resource
	if runResources != "" {
		// Parse comma-separated resource IDs
		resourceType := serviceToResourceType(name)
		resourceIDs := strings.Split(runResources, ",")
		for _, id := range resourceIDs {
			id = strings.TrimSpace(id)
			if id != "" {
				resources = append(resources, watcher.Resource{
					ResourceType: resourceType,
					ResourceID:   id,
				})
			}
		}
	} else {
		// Get all active resources
		resourceType := serviceToResourceType(name)
		activeResources, err := watcher.ActiveResources(d, resourceType)
		if err != nil {
			return fmt.Errorf("failed to get active resources: %w", err)
		}
		resources = activeResources
	}

	if len(resources) == 0 {
		fmt.Printf("No resources to poll for %s watcher.\n", name)
		return nil
	}

	fmt.Printf("Polling %d resources for %s watcher...\n", len(resources), name)

	// Open watcher log
	logger := watcher.OpenLog(name)

	// Run watcher-specific poll
	switch name {
	case "github":
		return github.Poll(d, cfg, resources, logger)
	case "jira":
		// TODO: Implement in Task 9
		fmt.Printf("Watcher %q not yet implemented\n", name)
		return nil
	default:
		return fmt.Errorf("unknown watcher: %s", name)
	}
}

// serviceToResourceType maps service names to resource types
func serviceToResourceType(service string) string {
	switch service {
	case "github":
		return "pr"
	case "jira":
		return "jira"
	default:
		return ""
	}
}
