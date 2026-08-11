package cmd

import (
	"testing"
	"time"

	"github.com/mturley/agent-handler/db"
)

// TestMigrateSubscriptionsFromArchived verifies subscriptions (including a
// soft-deleted one, matching the legacy query's lack of a deleted_at filter)
// are carried from the most-recently-archived same-named session onto a new
// session, reading from the watcher library rather than the legacy
// subscriptions table.
func TestMigrateSubscriptionsFromArchived(t *testing.T) {
	isolateHomes(t)

	d, err := db.Open(db.DefaultPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	if err := d.UpsertSession(db.Session{
		SessionID: "old-session", Harness: "claude-code", Repo: "r", Branch: "main",
		SessionName: "my-session", Status: "archived", LastActive: now, RegisteredAt: now, JSONLPath: "/tmp/old.jsonl",
	}); err != nil {
		t.Fatalf("upsert old session: %v", err)
	}
	if err := d.UpsertSession(db.Session{
		SessionID: "new-session", Harness: "claude-code", Repo: "r", Branch: "main",
		SessionName: "my-session", Status: "active", LastActive: now, RegisteredAt: now, JSONLPath: "/tmp/new.jsonl",
	}); err != nil {
		t.Fatalf("upsert new session: %v", err)
	}

	if err := d.Subscribe(db.Subscription{ID: "a", SessionID: "old-session", ResourceType: "pr", ResourceID: "1", CreatedAt: now}); err != nil {
		t.Fatalf("subscribe a: %v", err)
	}
	if err := d.Subscribe(db.Subscription{ID: "b", SessionID: "old-session", ResourceType: "jira", ResourceID: "PROJ-1", CreatedAt: now}); err != nil {
		t.Fatalf("subscribe b: %v", err)
	}
	// Soft-deleted subscription on the old session: the legacy behavior
	// (no deleted_at filter) still carries this over to the new session.
	if err := d.Unsubscribe("old-session", "jira", "PROJ-1"); err != nil {
		t.Fatalf("unsubscribe b: %v", err)
	}

	migrateSubscriptionsFromArchived(d, "new-session", "my-session")

	newSubs, err := d.ListSubscriptions("new-session", false)
	if err != nil {
		t.Fatalf("list new session subs: %v", err)
	}
	if len(newSubs) != 2 {
		t.Fatalf("new session active subscriptions = %v, want 2 (pr/1 and jira/PROJ-1, both re-subscribed live)", newSubs)
	}
	byType := map[string]bool{}
	for _, s := range newSubs {
		byType[s.ResourceType+"/"+s.ResourceID] = true
	}
	if !byType["pr/1"] || !byType["jira/PROJ-1"] {
		t.Errorf("new session subscriptions = %v, want pr/1 and jira/PROJ-1", newSubs)
	}
}
