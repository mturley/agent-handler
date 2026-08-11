package db

import (
	"testing"

	wdb "github.com/mturley/watcher/db"
)

func TestListResourceStatesForSession(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "rs-session-test")

	// Seed subscriptions via watcher library
	sessionID := "rs-session-test"

	if err := d.SubscribeIfNew(Subscription{
		SessionID: sessionID, ResourceType: "pr", ResourceID: "owner/repo#1",
	}); err != nil {
		t.Fatalf("SubscribeIfNew for pr failed: %v", err)
	}
	if err := d.SubscribeIfNew(Subscription{
		SessionID: sessionID, ResourceType: "jira", ResourceID: "PROJ-100",
	}); err != nil {
		t.Fatalf("SubscribeIfNew for jira failed: %v", err)
	}

	// Seed resource state via watcher library for only ONE subscription
	// to test both the state-present and state-absent (default-fallback) paths
	now := "2026-07-06T10:00:00Z"
	if err := wdb.UpsertResourceState(d.Conn(), "pr", "owner/repo#1", `{"state":"open"}`, now, now); err != nil {
		t.Fatalf("UpsertResourceState for pr failed: %v", err)
	}
	// Intentionally do NOT seed state for jira/PROJ-100 to test the default fallback

	results, err := d.ListResourceStatesForSession(sessionID)
	if err != nil {
		t.Fatalf("ListResourceStatesForSession failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Verify both resources are returned with correct state handling
	for _, r := range results {
		if r.ResourceType == "pr" && r.ResourceID == "owner/repo#1" {
			// pr has state row: verify real values
			if r.StateJSON != `{"state":"open"}` {
				t.Errorf("pr resource: expected state %q, got %q", `{"state":"open"}`, r.StateJSON)
			}
			if r.ResourceUpdatedAt != now {
				t.Errorf("pr resource: expected resource_updated_at %q, got %q", now, r.ResourceUpdatedAt)
			}
			if r.WatcherUpdatedAt != now {
				t.Errorf("pr resource: expected watcher_updated_at %q, got %q", now, r.WatcherUpdatedAt)
			}
		}
		if r.ResourceType == "jira" && r.ResourceID == "PROJ-100" {
			// jira has NO state row: verify COALESCE-equivalent defaults
			if r.StateJSON != "{}" {
				t.Errorf("jira resource (no state): expected StateJSON default %q, got %q", "{}", r.StateJSON)
			}
			if r.ResourceUpdatedAt != "" {
				t.Errorf("jira resource (no state): expected ResourceUpdatedAt default %q, got %q", "", r.ResourceUpdatedAt)
			}
			if r.WatcherUpdatedAt != "" {
				t.Errorf("jira resource (no state): expected WatcherUpdatedAt default %q, got %q", "", r.WatcherUpdatedAt)
			}
		}
	}
}

