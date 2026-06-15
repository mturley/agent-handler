package db

import (
	"testing"
	"time"
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
	now := time.Now().UTC().Format(time.RFC3339)

	// Seed a session
	seedSession(t, d, "s1")

	// Subscribe s1 to a PR
	sub := Subscription{
		ID:           "sub-pr",
		SessionID:    "s1",
		ResourceType: "github_pr",
		ResourceID:   "owner/repo#123",
		ResourceURL:  strPtr("https://github.com/owner/repo/pull/123"),
		CreatedAt:    now,
	}
	if err := d.Subscribe(sub); err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}

	// Insert an event referencing that PR
	event := Event{
		ID:        "event-1",
		TS:        now,
		Source:    "github",
		Type:      "pr_comment",
		Title:     "New comment on PR #123",
		Broadcast: false,
	}
	resources := []EventResource{
		{
			ResourceType: "github_pr",
			ResourceID:   "owner/repo#123",
			ResourceURL:  strPtr("https://github.com/owner/repo/pull/123"),
		},
	}
	if err := d.InsertEvent(event, nil, resources); err != nil {
		t.Fatalf("failed to insert event: %v", err)
	}

	// Query ResourceHistory for the PR
	history, err := d.ResourceHistory("github_pr", "owner/repo#123", 10)
	if err != nil {
		t.Fatalf("ResourceHistory failed: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 event in history, got %d", len(history))
	}
	if history[0].ID != "event-1" {
		t.Errorf("expected event-1, got %q", history[0].ID)
	}
	if history[0].Title != "New comment on PR #123" {
		t.Errorf("expected title 'New comment on PR #123', got %q", history[0].Title)
	}

	// Test limit=0 (no limit)
	historyAll, err := d.ResourceHistory("github_pr", "owner/repo#123", 0)
	if err != nil {
		t.Fatalf("ResourceHistory with limit=0 failed: %v", err)
	}
	if len(historyAll) != 1 {
		t.Errorf("expected 1 event with no limit, got %d", len(historyAll))
	}
}
