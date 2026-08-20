package watcher

import "github.com/spf13/cobra"

// JSONOutput is set by the parent cmd package to enable JSON output
var JSONOutput *bool

// knownWatchers lists the services handler can actually poll/install
// watchers for. Slack is deliberately excluded: it has credential
// auth/test+repair support (via `handler watcher auth slack` and
// credsetup.TestAndRepair) but no poller implementation on the handler side
// yet, so it must not be offered for `handler watcher install`/`run`.
var knownWatchers = []string{"github", "jira"}

var WatcherCmd = &cobra.Command{
	Use:   "watcher",
	Short: "Manage external event watchers",
}
