package db

import (
	"path/filepath"
	"testing"
	"time"
)

// testDB creates a temporary database for testing.
func testDB(t *testing.T) *DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestUpsertAndGetSession(t *testing.T) {
	db := testDB(t)
	now := time.Now().UTC().Format(time.RFC3339)

	autoPollInterval := 300
	s := Session{
		SessionID:        "session-123",
		Harness:          "claude",
		Repo:             "github.com/example/repo",
		Branch:           "main",
		SessionName:      "test-session",
		PID:              12345,
		Status:           "active",
		InboxMode:        "auto",
		AutoPollInterval: &autoPollInterval,
		LastActive:       now,
		RegisteredAt:     now,
		JSONLPath:        "/path/to/session.jsonl",
	}

	if err := db.UpsertSession(s); err != nil {
		t.Fatalf("UpsertSession failed: %v", err)
	}

	got, err := db.GetSession("session-123")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}

	if got == nil {
		t.Fatal("expected session, got nil")
	}

	if got.SessionID != s.SessionID {
		t.Errorf("SessionID: got %q, want %q", got.SessionID, s.SessionID)
	}
	if got.Harness != s.Harness {
		t.Errorf("Harness: got %q, want %q", got.Harness, s.Harness)
	}
	if got.Repo != s.Repo {
		t.Errorf("Repo: got %q, want %q", got.Repo, s.Repo)
	}
	if got.Branch != s.Branch {
		t.Errorf("Branch: got %q, want %q", got.Branch, s.Branch)
	}
	if got.SessionName != s.SessionName {
		t.Errorf("SessionName: got %q, want %q", got.SessionName, s.SessionName)
	}
	if got.PID != s.PID {
		t.Errorf("PID: got %d, want %d", got.PID, s.PID)
	}
	if got.Status != s.Status {
		t.Errorf("Status: got %q, want %q", got.Status, s.Status)
	}
	if got.InboxMode != s.InboxMode {
		t.Errorf("InboxMode: got %q, want %q", got.InboxMode, s.InboxMode)
	}
	if got.AutoPollInterval == nil || *got.AutoPollInterval != *s.AutoPollInterval {
		t.Errorf("AutoPollInterval: got %v, want %v", got.AutoPollInterval, s.AutoPollInterval)
	}
	if got.LastActive != s.LastActive {
		t.Errorf("LastActive: got %q, want %q", got.LastActive, s.LastActive)
	}
	if got.RegisteredAt != s.RegisteredAt {
		t.Errorf("RegisteredAt: got %q, want %q", got.RegisteredAt, s.RegisteredAt)
	}
	if got.JSONLPath != s.JSONLPath {
		t.Errorf("JSONLPath: got %q, want %q", got.JSONLPath, s.JSONLPath)
	}
}

func TestUpsertSessionUpdatesExisting(t *testing.T) {
	db := testDB(t)
	now := time.Now().UTC().Format(time.RFC3339)

	autoPollInterval := 300
	s := Session{
		SessionID:        "session-456",
		Harness:          "claude",
		Repo:             "github.com/example/repo",
		Branch:           "main",
		SessionName:      "old-name",
		PID:              12345,
		Status:           "active",
		InboxMode:        "auto",
		AutoPollInterval: &autoPollInterval,
		LastActive:       now,
		RegisteredAt:     now,
		JSONLPath:        "/path/to/session.jsonl",
	}

	if err := db.UpsertSession(s); err != nil {
		t.Fatalf("initial UpsertSession failed: %v", err)
	}

	// Update with new PID and name, but keep existing inbox_mode and auto_poll_interval
	s.PID = 67890
	s.SessionName = "new-name"
	s.InboxMode = ""                 // should preserve existing "auto"
	s.AutoPollInterval = nil         // should preserve existing 300
	s.LastActive = time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339)

	if err := db.UpsertSession(s); err != nil {
		t.Fatalf("update UpsertSession failed: %v", err)
	}

	got, err := db.GetSession("session-456")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}

	if got.PID != 67890 {
		t.Errorf("PID not updated: got %d, want 67890", got.PID)
	}
	if got.SessionName != "new-name" {
		t.Errorf("SessionName not updated: got %q, want %q", got.SessionName, "new-name")
	}
	if got.InboxMode != "auto" {
		t.Errorf("InboxMode not preserved: got %q, want %q", got.InboxMode, "auto")
	}
	if got.AutoPollInterval == nil || *got.AutoPollInterval != 300 {
		t.Errorf("AutoPollInterval not preserved: got %v, want 300", got.AutoPollInterval)
	}
}

func TestListSessionsFiltersArchived(t *testing.T) {
	db := testDB(t)
	now := time.Now().UTC().Format(time.RFC3339)

	active := Session{
		SessionID:    "session-active",
		Harness:      "claude",
		Repo:         "github.com/example/repo",
		Branch:       "main",
		Status:       "active",
		InboxMode:    "manual",
		LastActive:   now,
		RegisteredAt: now,
		JSONLPath:    "/path/to/active.jsonl",
	}

	archived := Session{
		SessionID:    "session-archived",
		Harness:      "claude",
		Repo:         "github.com/example/repo",
		Branch:       "main",
		Status:       "archived",
		InboxMode:    "manual",
		LastActive:   now,
		RegisteredAt: now,
		JSONLPath:    "/path/to/archived.jsonl",
	}

	if err := db.UpsertSession(active); err != nil {
		t.Fatalf("failed to insert active session: %v", err)
	}
	if err := db.UpsertSession(archived); err != nil {
		t.Fatalf("failed to insert archived session: %v", err)
	}

	// List without includeArchived
	sessions, err := db.ListSessions(false, 100, 0)
	if err != nil {
		t.Fatalf("ListSessions(false) failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("ListSessions(false): got %d sessions, want 1", len(sessions))
	}
	if len(sessions) > 0 && sessions[0].SessionID != "session-active" {
		t.Errorf("ListSessions(false): got session %q, want %q", sessions[0].SessionID, "session-active")
	}

	// List with includeArchived
	sessions, err = db.ListSessions(true, 100, 0)
	if err != nil {
		t.Fatalf("ListSessions(true) failed: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("ListSessions(true): got %d sessions, want 2", len(sessions))
	}
}

func TestBumpLastActive(t *testing.T) {
	db := testDB(t)
	now := time.Now().UTC().Format(time.RFC3339)

	s := Session{
		SessionID:    "session-bump",
		Harness:      "claude",
		Repo:         "github.com/example/repo",
		Branch:       "main",
		Status:       "active",
		InboxMode:    "manual",
		LastActive:   now,
		RegisteredAt: now,
		JSONLPath:    "/path/to/session.jsonl",
	}

	if err := db.UpsertSession(s); err != nil {
		t.Fatalf("UpsertSession failed: %v", err)
	}

	newTs := time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339)
	if err := db.BumpLastActive("session-bump", newTs); err != nil {
		t.Fatalf("BumpLastActive failed: %v", err)
	}

	got, err := db.GetSession("session-bump")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}

	if got.LastActive != newTs {
		t.Errorf("LastActive not updated: got %q, want %q", got.LastActive, newTs)
	}

	// Test error case: session not found
	err = db.BumpLastActive("nonexistent", newTs)
	if err == nil {
		t.Error("expected error for nonexistent session, got nil")
	}
}

func TestArchiveDeadSessions(t *testing.T) {
	db := testDB(t)
	now := time.Now().UTC().Format(time.RFC3339)

	s1 := Session{
		SessionID:    "session-to-archive-1",
		Harness:      "claude",
		Repo:         "github.com/example/repo",
		Branch:       "main",
		Status:       "active",
		InboxMode:    "manual",
		LastActive:   now,
		RegisteredAt: now,
		JSONLPath:    "/path/to/session1.jsonl",
	}

	s2 := Session{
		SessionID:    "session-to-archive-2",
		Harness:      "claude",
		Repo:         "github.com/example/repo",
		Branch:       "feature",
		Status:       "active",
		InboxMode:    "manual",
		LastActive:   now,
		RegisteredAt: now,
		JSONLPath:    "/path/to/session2.jsonl",
	}

	if err := db.UpsertSession(s1); err != nil {
		t.Fatalf("failed to insert s1: %v", err)
	}
	if err := db.UpsertSession(s2); err != nil {
		t.Fatalf("failed to insert s2: %v", err)
	}

	count, err := db.ArchiveSessions([]string{"session-to-archive-1", "session-to-archive-2"})
	if err != nil {
		t.Fatalf("ArchiveSessions failed: %v", err)
	}

	if count != 2 {
		t.Errorf("ArchiveSessions: got count %d, want 2", count)
	}

	// Verify both are archived
	got1, err := db.GetSession("session-to-archive-1")
	if err != nil {
		t.Fatalf("GetSession(s1) failed: %v", err)
	}
	if got1.Status != "archived" {
		t.Errorf("s1 Status: got %q, want %q", got1.Status, "archived")
	}

	got2, err := db.GetSession("session-to-archive-2")
	if err != nil {
		t.Fatalf("GetSession(s2) failed: %v", err)
	}
	if got2.Status != "archived" {
		t.Errorf("s2 Status: got %q, want %q", got2.Status, "archived")
	}
}
