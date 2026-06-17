package watcher

import (
	"fmt"
	"strings"

	"github.com/mturley/agent-handler/watcher"
	"github.com/spf13/cobra"
)

func init() {
	WatcherCmd.AddCommand(uninstallCmd)
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall <name>",
	Short: "Remove a scheduled watcher",
	Long: `Remove a scheduled watcher from the system scheduler.

Valid watchers: github, jira

This removes the watcher from the system scheduler (LaunchAgent or cron entry)
but does not delete logs or database entries.`,
	Args: cobra.ExactArgs(1),
	RunE: uninstallWatcher,
}

func uninstallWatcher(cmd *cobra.Command, args []string) error {
	name := strings.ToLower(args[0])

	// Validate watcher name
	if name != "github" && name != "jira" {
		return fmt.Errorf("unknown watcher: %s (must be 'github' or 'jira')", name)
	}

	// Uninstall watcher
	if err := watcher.Uninstall(name); err != nil {
		return fmt.Errorf("failed to uninstall watcher: %w", err)
	}

	fmt.Printf("✓ Watcher %q uninstalled\n", name)

	return nil
}
