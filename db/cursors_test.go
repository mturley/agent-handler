package db

import (
	"testing"
	"time"
)

// seedSession inserts a minimal session for use in tests.
func seedSession(t *testing.T, d *DB, id string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	s := Session{
		SessionID:    id,
		Harness:      "claude",
		Repo:         "github.com/example/repo",
		Branch:       "main",
		Status:       "active",
		InboxMode:    "manual",
		LastActive:   now,
		RegisteredAt: now,
		JSONLPath:    "/path/to/session.jsonl",
	}
	if err := d.UpsertSession(s); err != nil {
		t.Fatalf("failed to seed session %q: %v", id, err)
	}
}

// strPtr returns a pointer to the given string.
func strPtr(s string) *string {
	return &s
}

func TestGetAndAdvanceCursor(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "session-cursor-test")

	// No cursor should return empty string
	cursor, err := d.GetCursor("session-cursor-test")
	if err != nil {
		t.Fatalf("GetCursor failed: %v", err)
	}
	if cursor != "" {
		t.Errorf("expected empty cursor, got %q", cursor)
	}

	// Advance cursor
	ts := "2026-06-15T10:00:00Z"
	if err := d.AdvanceCursor("session-cursor-test", ts); err != nil {
		t.Fatalf("AdvanceCursor failed: %v", err)
	}

	// Read back
	cursor, err = d.GetCursor("session-cursor-test")
	if err != nil {
		t.Fatalf("GetCursor failed after advance: %v", err)
	}
	if cursor != ts {
		t.Errorf("cursor: got %q, want %q", cursor, ts)
	}

	// Advance again
	ts2 := "2026-06-15T11:00:00Z"
	if err := d.AdvanceCursor("session-cursor-test", ts2); err != nil {
		t.Fatalf("second AdvanceCursor failed: %v", err)
	}

	cursor, err = d.GetCursor("session-cursor-test")
	if err != nil {
		t.Fatalf("GetCursor failed after second advance: %v", err)
	}
	if cursor != ts2 {
		t.Errorf("cursor: got %q, want %q", cursor, ts2)
	}
}
