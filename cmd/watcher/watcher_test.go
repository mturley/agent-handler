package watcher

import (
	"path/filepath"
	"testing"

	"github.com/mturley/agent-handler/db"
	wdb "github.com/mturley/watcher/db"
)

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

// A run that finds nothing to poll is a successful run, not a silent no-op:
// if it leaves last_success frozen, an error from a previous run outlives the
// subscription that caused it and the watcher shows ✗ error forever.
func TestRecordIdleRunClearsStaleError(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	if err := wdb.RecordPollerError(d.Conn(), "github", "Could not resolve to a Repository"); err != nil {
		t.Fatalf("record error: %v", err)
	}
	if !wdb.HasPollerError(d.Conn(), "github") {
		t.Fatalf("precondition: expected poller to be in error state")
	}

	if err := recordIdleRun(d.Conn(), "github"); err != nil {
		t.Fatalf("recordIdleRun: %v", err)
	}

	if wdb.HasPollerError(d.Conn(), "github") {
		t.Errorf("poller still reports an error after a clean run with no resources")
	}
}
