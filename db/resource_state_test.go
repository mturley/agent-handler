package db

import (
	"testing"

	wdb "github.com/mturley/watcher/db"
)

func TestUpsertAndGetResourceState(t *testing.T) {
	d := testDB(t)

	err := d.UpsertResourceState("pr", "owner/repo#1", `{"state":"open"}`, "2026-07-06T10:00:00Z", "2026-07-06T10:01:00Z")
	if err != nil {
		t.Fatalf("UpsertResourceState failed: %v", err)
	}

	rs, err := d.GetResourceState("pr", "owner/repo#1")
	if err != nil {
		t.Fatalf("GetResourceState failed: %v", err)
	}
	if rs == nil {
		t.Fatal("expected non-nil ResourceState")
	}
	if rs.StateJSON != `{"state":"open"}` {
		t.Errorf("expected state JSON %q, got %q", `{"state":"open"}`, rs.StateJSON)
	}
	if rs.ResourceUpdatedAt != "2026-07-06T10:00:00Z" {
		t.Errorf("expected resource_updated_at %q, got %q", "2026-07-06T10:00:00Z", rs.ResourceUpdatedAt)
	}
	if rs.WatcherUpdatedAt != "2026-07-06T10:01:00Z" {
		t.Errorf("expected watcher_updated_at %q, got %q", "2026-07-06T10:01:00Z", rs.WatcherUpdatedAt)
	}
}

func TestUpsertResourceStateOverwrites(t *testing.T) {
	d := testDB(t)

	d.UpsertResourceState("pr", "owner/repo#1", `{"state":"open"}`, "2026-07-06T10:00:00Z", "2026-07-06T10:01:00Z")
	d.UpsertResourceState("pr", "owner/repo#1", `{"state":"merged"}`, "2026-07-06T11:00:00Z", "2026-07-06T11:01:00Z")

	rs, _ := d.GetResourceState("pr", "owner/repo#1")
	if rs.StateJSON != `{"state":"merged"}` {
		t.Errorf("expected updated state, got %q", rs.StateJSON)
	}
}

func TestGetResourceStateNotFound(t *testing.T) {
	d := testDB(t)

	rs, err := d.GetResourceState("pr", "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rs != nil {
		t.Error("expected nil for nonexistent resource")
	}
}

func TestDeleteResourceState(t *testing.T) {
	d := testDB(t)

	d.UpsertResourceState("jira", "PROJ-1", `{"status":"open"}`, "2026-07-06T10:00:00Z", "2026-07-06T10:01:00Z")
	err := d.DeleteResourceState("jira", "PROJ-1")
	if err != nil {
		t.Fatalf("DeleteResourceState failed: %v", err)
	}

	rs, _ := d.GetResourceState("jira", "PROJ-1")
	if rs != nil {
		t.Error("expected nil after delete")
	}
}

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

