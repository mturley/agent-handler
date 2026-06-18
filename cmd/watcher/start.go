package watcher

import (
	"fmt"

	watcherPkg "github.com/mturley/agent-handler/watcher"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start [name]",
	Short: "Resume paused watchers",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runStart,
}

func init() {
	WatcherCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	targets := knownWatchers
	if len(args) == 1 {
		targets = []string{args[0]}
	}

	started := 0
	for _, name := range targets {
		if watcherPkg.IsInstalled(name) && !watcherPkg.IsRunning(name) {
			if err := watcherPkg.Start(name); err != nil {
				fmt.Printf("  ⚠ Failed to start %s: %v\n", name, err)
				continue
			}
			fmt.Printf("  ✓ Started %s watcher\n", name)
			started++
		}
	}

	if started == 0 {
		fmt.Println("No paused watchers to start.")
		anyInstalled := false
		for _, name := range targets {
			if watcherPkg.IsInstalled(name) {
				anyInstalled = true
				break
			}
		}
		if !anyInstalled {
			fmt.Println("Install watchers with: handler watcher install")
		}
	}

	return nil
}
