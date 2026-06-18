package watcher

import (
	"fmt"

	watcherPkg "github.com/mturley/agent-handler/watcher"
	"github.com/spf13/cobra"
)

var uninstallWatcherCmd = &cobra.Command{
	Use:   "uninstall [name]",
	Short: "Remove scheduled watchers",
	Long: `With no arguments: uninstalls all installed watchers.
With a name argument: uninstalls a specific watcher.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runUninstallWatcher,
}

func init() {
	WatcherCmd.AddCommand(uninstallWatcherCmd)
}

func runUninstallWatcher(cmd *cobra.Command, args []string) error {
	targets := knownWatchers
	if len(args) == 1 {
		targets = []string{args[0]}
	}

	uninstalled := 0
	for _, name := range targets {
		if watcherPkg.IsInstalled(name) {
			if err := watcherPkg.Uninstall(name); err != nil {
				fmt.Printf("  ⚠ Failed to uninstall %s: %v\n", name, err)
				continue
			}
			fmt.Printf("  ✓ Uninstalled %s watcher\n", name)
			uninstalled++
		}
	}

	if uninstalled == 0 {
		fmt.Println("No installed watchers to uninstall.")
	}

	return nil
}
