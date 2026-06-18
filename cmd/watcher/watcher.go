package watcher

import "github.com/spf13/cobra"

// JSONOutput is set by the parent cmd package to enable JSON output
var JSONOutput *bool

var knownWatchers = []string{"github", "jira"}

var WatcherCmd = &cobra.Command{
	Use:   "watcher",
	Short: "Manage external event watchers",
}
