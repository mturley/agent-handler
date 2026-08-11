package db

import (
	"testing"
	"time"

	watcher "github.com/mturley/watcher"
	wdb "github.com/mturley/watcher/db"
)

// TestWatcherMigrationMarkerDefaultsFalse verifies the handler-owned gate
// defaults false and flips true only after setWatcherMigrated. The inbox
// UNION's watcher arm must gate on THIS marker, not wdb.SchemaVersion (which
// is already >=1 as soon as the watcher tables exist, before any data moves).
func TestWatcherMigrationMarkerDefaultsFalse(t *testing.T) {
	d := testDB(t)
	if d.watcherMigrationDone() {
		t.Fatal("marker should default false")
	}
	if err := d.setWatcherMigrated(); err != nil {
		t.Fatal(err)
	}
	if !d.watcherMigrationDone() {
		t.Fatal("marker should be true after set")
	}
}

// insertAgentEventForSession inserts a handler event addressed directly to a
// session via event_recipients (the agent arm's routing).
func insertAgentEventForSession(t *testing.T, d *DB, id, sessionID, ts string) {
	t.Helper()
	e := Event{ID: id, TS: ts, Source: "handler", Type: "message", Title: "agent event"}
	recips := []EventRecipient{{RecipientType: "session", RecipientValue: sessionID}}
	if err := d.InsertEvent(e, recips, nil); err != nil {
		t.Fatalf("InsertEvent(agent) failed: %v", err)
	}
}

// insertWatcherEvent inserts a library event into watcher_events referencing a
// resource (the watcher arm's routing), using the library's own writer.
func insertWatcherEvent(t *testing.T, d *DB, id, resType, resID, ts string, typ watcher.EventType) {
	t.Helper()
	e := watcher.Event{ID: id, TS: ts, Source: "github", Type: typ, Title: "watcher event"}
	r := watcher.Resource{Type: resType, ID: resID}
	if err := wdb.InsertEvent(d.conn, e, r); err != nil {
		t.Fatalf("wdb.InsertEvent failed: %v", err)
	}
}

// TestInboxUnionReturnsBothArms exercises the (now unconditional) UNION:
// UnreadForSession returns BOTH an agent-routed event and a watcher-routed
// (subscription) event. A dismissed watcher event is excluded even though it
// lives in watcher_events.
func TestInboxUnionReturnsBothArms(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "s1")

	cursor := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	if err := d.AdvanceCursor("s1", cursor); err != nil {
		t.Fatalf("AdvanceCursor failed: %v", err)
	}

	base := time.Now().UTC()
	agentTS := base.Add(1 * time.Second).Format(time.RFC3339)
	watcherTS := base.Add(2 * time.Second).Format(time.RFC3339)

	insertAgentEventForSession(t, d, "agent-1", "s1", agentTS)
	if err := d.SubscribeIfNew(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "o/r#1"}); err != nil {
		t.Fatalf("SubscribeIfNew failed: %v", err)
	}
	insertWatcherEvent(t, d, "watch-1", "pr", "o/r#1", watcherTS, watcher.EventTypePRComment)

	// Both arms contribute.
	events, err := d.UnreadForSession("s1")
	if err != nil {
		t.Fatalf("UnreadForSession(union) failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("want 2 (agent+watcher), got %d: %+v", len(events), events)
	}
	// Ordered by ts ASC: agent (t+1) then watcher (t+2).
	if events[0].ID != "agent-1" || events[1].ID != "watch-1" {
		t.Fatalf("want [agent-1, watch-1] ordered by ts, got [%s, %s]", events[0].ID, events[1].ID)
	}

	// A dismissed watcher event must be excluded (dismissal applies on the
	// watcher arm too, not just the agent arm).
	if err := d.DismissEvent("s1", "watch-1"); err != nil {
		t.Fatalf("DismissEvent failed: %v", err)
	}
	afterDismiss, err := d.UnreadForSession("s1")
	if err != nil {
		t.Fatalf("UnreadForSession(after dismiss) failed: %v", err)
	}
	if len(afterDismiss) != 1 || afterDismiss[0].ID != "agent-1" {
		t.Fatalf("after dismissing watcher event: want [agent-1], got %+v", afterDismiss)
	}
}
