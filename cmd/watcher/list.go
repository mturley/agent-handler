package watcher

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mturley/agent-handler/watcher"
	"github.com/spf13/cobra"
)

func init() {
	WatcherCmd.AddCommand(listCmd)
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed watchers",
	Long: `List all known watchers and their installation status.

Shows which watchers are installed, when they last ran, and their configuration.`,
	RunE: listWatchers,
}

type watcherInfo struct {
	Name      string     `json:"name"`
	Installed bool       `json:"installed"`
	LastRun   *time.Time `json:"last_run,omitempty"`
}

func listWatchers(cmd *cobra.Command, args []string) error {
	knownWatchers := []string{"github", "jira"}
	var watchers []watcherInfo

	for _, name := range knownWatchers {
		info := watcherInfo{
			Name:      name,
			Installed: watcher.IsInstalled(name),
			LastRun:   watcher.LastRunTime(name),
		}
		watchers = append(watchers, info)
	}

	if *JSONOutput {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(watchers)
	}

	fmt.Println("Watchers:")
	for _, w := range watchers {
		status := "not installed"
		if w.Installed {
			status = "installed"
		}

		lastRunStr := "never"
		if w.LastRun != nil {
			lastRunStr = w.LastRun.Format(time.RFC3339)
		}

		fmt.Printf("  %s: %s (last run: %s)\n", w.Name, status, lastRunStr)
	}

	return nil
}
