package watcher

import (
	"fmt"
	"strings"
	"time"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/watcher"
	"github.com/spf13/cobra"
)

var installInterval time.Duration

func init() {
	WatcherCmd.AddCommand(installCmd)
	installCmd.Flags().DurationVar(&installInterval, "interval", 0, "Polling interval (e.g. 3m, 5m). Default: 3m for github, 5m for jira")
}

var installCmd = &cobra.Command{
	Use:   "install <name>",
	Short: "Schedule a watcher to run periodically",
	Long: `Schedule a watcher to run periodically using the system scheduler.

Valid watchers: github, jira

On macOS, creates a LaunchAgent. On Linux, adds a cron entry.
The watcher will poll for events at the specified interval.`,
	Args: cobra.ExactArgs(1),
	RunE: installWatcher,
}

func installWatcher(cmd *cobra.Command, args []string) error {
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

	// Determine interval
	interval := installInterval
	if interval == 0 {
		// Use default intervals
		switch name {
		case "github":
			interval = 3 * time.Minute
		case "jira":
			interval = 5 * time.Minute
		}
	}

	intervalSeconds := int(interval.Seconds())
	if intervalSeconds < 60 {
		return fmt.Errorf("interval must be at least 1 minute")
	}

	// Install watcher
	if err := watcher.Install(name, intervalSeconds); err != nil {
		return fmt.Errorf("failed to install watcher: %w", err)
	}

	fmt.Printf("✓ Watcher %q installed (polling every %s)\n", name, interval)
	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  - View logs:   handler watcher logs %s\n", name)
	fmt.Printf("  - Run now:     handler watcher run %s\n", name)
	fmt.Printf("  - Uninstall:   handler watcher uninstall %s\n", name)

	return nil
}
