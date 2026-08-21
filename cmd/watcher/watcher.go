package watcher

import (
	"github.com/mturley/agent-handler/watcher"
	"github.com/spf13/cobra"
)

// JSONOutput is set by the parent cmd package to enable JSON output
var JSONOutput *bool

// knownWatchers is this package's handle on the canonical watcher service list
// (watcher.KnownWatchers) — the single source of truth. Slack is first-class
// alongside github and jira.
var knownWatchers = watcher.KnownWatchers

var WatcherCmd = &cobra.Command{
	Use:   "watcher",
	Short: "Manage external event watchers",
}
