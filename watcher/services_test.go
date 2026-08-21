package watcher

import "testing"

// TestKnownWatchersIncludesSlack guards the single source of truth for the
// watcher service list. The status/statusline/triage/watching/uninstall paths
// and the watcher CLI all iterate KnownWatchers, so slack must be present here
// for it to be a first-class watcher everywhere.
func TestKnownWatchersIncludesSlack(t *testing.T) {
	for _, want := range []string{"github", "jira", "slack"} {
		found := false
		for _, w := range KnownWatchers {
			if w == want {
				found = true
			}
		}
		if !found {
			t.Errorf("KnownWatchers missing %q: %v", want, KnownWatchers)
		}
	}
}
