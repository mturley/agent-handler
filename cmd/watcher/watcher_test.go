package watcher

import "testing"

func TestKnownWatchersIncludesSlack(t *testing.T) {
	found := false
	for _, w := range knownWatchers {
		if w == "slack" {
			found = true
		}
	}
	if !found {
		t.Errorf("knownWatchers missing slack: %v", knownWatchers)
	}
}

func TestServiceToResourceType_Slack(t *testing.T) {
	if got := serviceToResourceType("slack"); got != "slack" {
		t.Errorf("got %q want slack", got)
	}
}
