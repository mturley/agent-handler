package db

import (
	"testing"
	"time"

	watcher "github.com/mturley/watcher"
	wdb "github.com/mturley/watcher/db"
)

func TestLinkAndFindRelated(t *testing.T) {
	d := testDB(t)
	now := time.Now().UTC().Format(time.RFC3339)

	// Seed two sessions
	seedSession(t, d, "s1")
	seedSession(t, d, "s2")

	// Subscribe s1 to RHOAIENG-100
	sub1 := Subscription{
		ID:           "sub-1",
		SessionID:    "s1",
		ResourceType: "jira",
		ResourceID:   "RHOAIENG-100",
		ResourceURL:  strPtr("https://redhat.atlassian.net/browse/RHOAIENG-100"),
		CreatedAt:    now,
	}
	if err := d.Subscribe(sub1); err != nil {
		t.Fatalf("failed to subscribe s1: %v", err)
	}

	// Subscribe s2 to RHOAIENG-101
	sub2 := Subscription{
		ID:           "sub-2",
		SessionID:    "s2",
		ResourceType: "jira",
		ResourceID:   "RHOAIENG-101",
		ResourceURL:  strPtr("https://redhat.atlassian.net/browse/RHOAIENG-101"),
		CreatedAt:    now,
	}
	if err := d.Subscribe(sub2); err != nil {
		t.Fatalf("failed to subscribe s2: %v", err)
	}

	// Before linking, FindRelatedSessions("s1") should return empty
	related, err := d.FindRelatedSessions("s1")
	if err != nil {
		t.Fatalf("FindRelatedSessions failed: %v", err)
	}
	if len(related) != 0 {
		t.Errorf("expected no related sessions before linking, got %d", len(related))
	}

	// Link both as epic_child of RHOAIENG-50
	rel1 := ResourceRelationship{
		ID:           "rel-1",
		ChildType:    "jira",
		ChildID:      "RHOAIENG-100",
		ChildURL:     strPtr("https://redhat.atlassian.net/browse/RHOAIENG-100"),
		ParentType:   "jira",
		ParentID:     "RHOAIENG-50",
		ParentURL:    strPtr("https://redhat.atlassian.net/browse/RHOAIENG-50"),
		Relationship: "epic_child",
		Source:       "jira",
		CreatedAt:    now,
	}
	if err := d.LinkResources(rel1); err != nil {
		t.Fatalf("failed to link RHOAIENG-100: %v", err)
	}

	rel2 := ResourceRelationship{
		ID:           "rel-2",
		ChildType:    "jira",
		ChildID:      "RHOAIENG-101",
		ChildURL:     strPtr("https://redhat.atlassian.net/browse/RHOAIENG-101"),
		ParentType:   "jira",
		ParentID:     "RHOAIENG-50",
		ParentURL:    strPtr("https://redhat.atlassian.net/browse/RHOAIENG-50"),
		Relationship: "epic_child",
		Source:       "jira",
		CreatedAt:    now,
	}
	if err := d.LinkResources(rel2); err != nil {
		t.Fatalf("failed to link RHOAIENG-101: %v", err)
	}

	// Now FindRelatedSessions("s1") should return s2
	related, err = d.FindRelatedSessions("s1")
	if err != nil {
		t.Fatalf("FindRelatedSessions failed after linking: %v", err)
	}
	if len(related) != 1 {
		t.Fatalf("expected 1 related session, got %d", len(related))
	}
	if related[0].SessionID != "s2" {
		t.Errorf("expected session s2, got %q", related[0].SessionID)
	}

	// Similarly, FindRelatedSessions("s2") should return s1
	related, err = d.FindRelatedSessions("s2")
	if err != nil {
		t.Fatalf("FindRelatedSessions failed for s2: %v", err)
	}
	if len(related) != 1 {
		t.Fatalf("expected 1 related session for s2, got %d", len(related))
	}
	if related[0].SessionID != "s1" {
		t.Errorf("expected session s1, got %q", related[0].SessionID)
	}
}

func TestResourceHistory(t *testing.T) {
	d := testDB(t)

	// ResourceHistory reads watcher_events (via wdb.EventsForResource), since
	// resource-scoped events (github/jira) live there, not in handler's own
	// events table. Seed directly through the library's InsertEvent, mirroring
	// how the other repointed reads in this task are tested against watcher
	// tables.
	res := watcher.Resource{Type: "github_pr", ID: "owner/repo#123", URL: "https://github.com/owner/repo/pull/123"}

	older := watcher.Event{
		ID:     "event-1",
		TS:     "2026-01-01T00:00:00Z",
		Source: "github",
		Type:   "pr_comment",
		Title:  "New comment on PR #123",
	}
	if err := wdb.InsertEvent(d.Conn(), older, res); err != nil {
		t.Fatalf("failed to insert event: %v", err)
	}

	newer := watcher.Event{
		ID:     "event-2",
		TS:     "2026-01-02T00:00:00Z",
		Source: "github",
		Type:   "pr_comment",
		Title:  "Another comment on PR #123",
	}
	if err := wdb.InsertEvent(d.Conn(), newer, res); err != nil {
		t.Fatalf("failed to insert event: %v", err)
	}

	// Query ResourceHistory for the PR — should be ordered ts DESC.
	history, err := d.ResourceHistory("github_pr", "owner/repo#123", 10)
	if err != nil {
		t.Fatalf("ResourceHistory failed: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 events in history, got %d", len(history))
	}
	if history[0].ID != "event-2" || history[1].ID != "event-1" {
		t.Errorf("expected [event-2, event-1] (ts DESC), got [%s, %s]", history[0].ID, history[1].ID)
	}
	if history[0].Title != "Another comment on PR #123" {
		t.Errorf("expected title 'Another comment on PR #123', got %q", history[0].Title)
	}

	// Test limit=0 (no limit)
	historyAll, err := d.ResourceHistory("github_pr", "owner/repo#123", 0)
	if err != nil {
		t.Fatalf("ResourceHistory with limit=0 failed: %v", err)
	}
	if len(historyAll) != 2 {
		t.Errorf("expected 2 events with no limit, got %d", len(historyAll))
	}

	// Test limit=1
	historyLimited, err := d.ResourceHistory("github_pr", "owner/repo#123", 1)
	if err != nil {
		t.Fatalf("ResourceHistory with limit=1 failed: %v", err)
	}
	if len(historyLimited) != 1 || historyLimited[0].ID != "event-2" {
		t.Fatalf("expected [event-2] with limit=1, got %+v", historyLimited)
	}
}

func TestSessionsForResourceParsesSessionIDs(t *testing.T) {
	d := testDB(t)
	d.SubscribeIfNew(Subscription{SessionID: "s1", ResourceType: "pr", ResourceID: "o/r#1"})
	d.SubscribeIfNew(Subscription{SessionID: "s2", ResourceType: "pr", ResourceID: "o/r#1"})
	subs, err := d.SessionsForResource("pr", "o/r#1")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, s := range subs {
		ids[s.SessionID] = true
	}
	if !ids["s1"] || !ids["s2"] || len(ids) != 2 {
		t.Fatalf("want s1,s2; got %v", ids)
	}
}
