package watcher

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mturley/agent-handler/db"
)

// testDB creates a temporary database for testing.
func testDB(t *testing.T) *db.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestActiveResources(t *testing.T) {
	d := testDB(t)
	now := time.Now().UTC().Format(time.RFC3339)

	// Create an active session
	session := db.Session{
		SessionID:    "session-123",
		Harness:      "claude",
		Repo:         "github.com/example/repo",
		Branch:       "main",
		Status:       "active",
		InboxMode:    "manual",
		LastActive:   now,
		RegisteredAt: now,
		JSONLPath:    "/path/to/session.jsonl",
	}
	if err := d.UpsertSession(session); err != nil {
		t.Fatalf("failed to upsert session: %v", err)
	}

	// Create a subscription to a PR
	sub := db.Subscription{
		ID:           uuid.New().String(),
		SessionID:    "session-123",
		ResourceType: "github_pr",
		ResourceID:   "owner/repo#123",
		ResourceURL:  strPtr("https://github.com/owner/repo/pull/123"),
		CreatedAt:    now,
		DeletedAt:    nil,
	}
	if err := d.Subscribe(sub); err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}

	// Query active resources
	resources, err := ActiveResources(d, "github_pr")
	if err != nil {
		t.Fatalf("ActiveResources failed: %v", err)
	}

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}

	r := resources[0]
	if r.ResourceType != "github_pr" {
		t.Errorf("ResourceType: got %q, want %q", r.ResourceType, "github_pr")
	}
	if r.ResourceID != "owner/repo#123" {
		t.Errorf("ResourceID: got %q, want %q", r.ResourceID, "owner/repo#123")
	}
	if r.ResourceURL != "https://github.com/owner/repo/pull/123" {
		t.Errorf("ResourceURL: got %q, want %q", r.ResourceURL, "https://github.com/owner/repo/pull/123")
	}
}

func TestActiveResourcesSkipsArchived(t *testing.T) {
	d := testDB(t)
	now := time.Now().UTC().Format(time.RFC3339)

	// Create an archived session
	session := db.Session{
		SessionID:    "session-archived",
		Harness:      "claude",
		Repo:         "github.com/example/repo",
		Branch:       "main",
		Status:       "archived",
		InboxMode:    "manual",
		LastActive:   now,
		RegisteredAt: now,
		JSONLPath:    "/path/to/session.jsonl",
	}
	if err := d.UpsertSession(session); err != nil {
		t.Fatalf("failed to upsert session: %v", err)
	}

	// Create a subscription
	sub := db.Subscription{
		ID:           uuid.New().String(),
		SessionID:    "session-archived",
		ResourceType: "github_pr",
		ResourceID:   "owner/repo#456",
		ResourceURL:  strPtr("https://github.com/owner/repo/pull/456"),
		CreatedAt:    now,
		DeletedAt:    nil,
	}
	if err := d.Subscribe(sub); err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}

	// Query active resources - should find none
	resources, err := ActiveResources(d, "github_pr")
	if err != nil {
		t.Fatalf("ActiveResources failed: %v", err)
	}

	if len(resources) != 0 {
		t.Errorf("expected 0 resources for archived session, got %d", len(resources))
	}
}

func TestEventCursorEmpty(t *testing.T) {
	d := testDB(t)

	cursor := EventCursor(d, "github_pr_watcher", "github_pr", "owner/repo#123")
	if cursor != "" {
		t.Errorf("expected empty cursor, got %q", cursor)
	}
}

func TestEventCursorAfterEvent(t *testing.T) {
	d := testDB(t)
	externalTS := "2024-01-15T12:00:00Z"

	// Insert an event with external_ts
	event := db.Event{
		ID:         uuid.New().String(),
		TS:         time.Now().UTC().Format(time.RFC3339),
		ExternalTS: &externalTS,
		Source:     "github_pr_watcher",
		SessionID:  nil,
		Type:       "pr_comment",
		Title:      "New comment",
		Body:       nil,
		Author:     nil,
		AuthorType: nil,
		Broadcast:  false,
		Tags:       nil,
	}

	resources := []db.EventResource{
		{
			ResourceType: "github_pr",
			ResourceID:   "owner/repo#123",
			ResourceURL:  strPtr("https://github.com/owner/repo/pull/123"),
		},
	}

	if err := d.InsertEvent(event, nil, resources); err != nil {
		t.Fatalf("failed to insert event: %v", err)
	}

	cursor := EventCursor(d, "github_pr_watcher", "github_pr", "owner/repo#123")
	if cursor != externalTS {
		t.Errorf("cursor: got %q, want %q", cursor, externalTS)
	}
}

func TestIsDuplicate(t *testing.T) {
	d := testDB(t)
	externalTS := "2024-01-15T12:00:00Z"

	// Insert an event
	event := db.Event{
		ID:         uuid.New().String(),
		TS:         time.Now().UTC().Format(time.RFC3339),
		ExternalTS: &externalTS,
		Source:     "github_pr_watcher",
		SessionID:  nil,
		Type:       "pr_comment",
		Title:      "New comment",
		Body:       nil,
		Author:     nil,
		AuthorType: nil,
		Broadcast:  false,
		Tags:       nil,
	}

	resources := []db.EventResource{
		{
			ResourceType: "github_pr",
			ResourceID:   "owner/repo#123",
			ResourceURL:  strPtr("https://github.com/owner/repo/pull/123"),
		},
	}

	if err := d.InsertEvent(event, nil, resources); err != nil {
		t.Fatalf("failed to insert event: %v", err)
	}

	// Test duplicate detection
	isDup := IsDuplicate(d, "github_pr_watcher", "github_pr", "owner/repo#123", "pr_comment", externalTS)
	if !isDup {
		t.Error("expected duplicate to be detected")
	}

	// Test non-duplicate with different timestamp
	isDup = IsDuplicate(d, "github_pr_watcher", "github_pr", "owner/repo#123", "pr_comment", "2024-01-15T13:00:00Z")
	if isDup {
		t.Error("expected non-duplicate with different timestamp")
	}

	// Test non-duplicate with different event type
	isDup = IsDuplicate(d, "github_pr_watcher", "github_pr", "owner/repo#123", "pr_review", externalTS)
	if isDup {
		t.Error("expected non-duplicate with different event type")
	}

	// Test non-duplicate with different resource
	isDup = IsDuplicate(d, "github_pr_watcher", "github_pr", "owner/repo#456", "pr_comment", externalTS)
	if isDup {
		t.Error("expected non-duplicate with different resource")
	}
}
