package db

import "testing"

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

	now := "2026-07-06T10:00:00Z"
	d.Subscribe(Subscription{
		ID: "sub-1", SessionID: "rs-session-test",
		ResourceType: "pr", ResourceID: "owner/repo#1",
		CreatedAt: now,
	})
	d.Subscribe(Subscription{
		ID: "sub-2", SessionID: "rs-session-test",
		ResourceType: "jira", ResourceID: "PROJ-100",
		CreatedAt: now,
	})

	d.UpsertResourceState("pr", "owner/repo#1", `{"state":"open"}`, now, now)
	d.UpsertResourceState("jira", "PROJ-100", `{"status":"In Progress"}`, now, now)

	results, err := d.ListResourceStatesForSession("rs-session-test")
	if err != nil {
		t.Fatalf("ListResourceStatesForSession failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}
