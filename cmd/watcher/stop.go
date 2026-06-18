package watcher

import (
	"fmt"

	watcherPkg "github.com/mturley/agent-handler/watcher"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Pause all installed watchers",
	RunE:  runStop,
}

func init() {
	WatcherCmd.AddCommand(stopCmd)
}

func runStop(cmd *cobra.Command, args []string) error {
	stopped := 0
	for _, name := range knownWatchers {
		if watcherPkg.IsInstalled(name) && watcherPkg.IsRunning(name) {
			if err := watcherPkg.Stop(name); err != nil {
				fmt.Printf("  ⚠ Failed to stop %s: %v\n", name, err)
				continue
			}
			fmt.Printf("  ✓ Stopped %s watcher\n", name)
			stopped++
		}
	}

	if stopped == 0 {
		fmt.Println("No running watchers to stop.")
		fmt.Println("Install watchers with: handler watcher install")
	}

	return nil
}
