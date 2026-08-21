package watcher

import "github.com/spf13/cobra"

// JSONOutput is set by the parent cmd package to enable JSON output
var JSONOutput *bool

// knownWatchers lists the services handler can actually poll/install
// watchers for. Slack is a first-class watcher alongside github and jira:
// it has credential auth/test+repair support (via `handler watcher auth
// slack` and credsetup.TestAndRepair) and a poller implementation
// (wslack.Poll), so it is offered for `handler watcher install`/`run`.
var knownWatchers = []string{"github", "jira", "slack"}

var WatcherCmd = &cobra.Command{
	Use:   "watcher",
	Short: "Manage external event watchers",
}
