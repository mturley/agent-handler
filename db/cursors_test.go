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

func TestDualCursors(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "dual-cursor-test")

	// AdvanceBothCursors sets both
	if err := d.AdvanceBothCursors("dual-cursor-test", "2026-06-15T10:00:00Z"); err != nil {
		t.Fatalf("AdvanceBothCursors failed: %v", err)
	}
	agent, err := d.GetCursor("dual-cursor-test")
	if err != nil {
		t.Fatalf("GetCursor failed: %v", err)
	}
	if agent != "2026-06-15T10:00:00Z" {
		t.Errorf("agent cursor: got %q, want %q", agent, "2026-06-15T10:00:00Z")
	}

	// AutoDeliveredCount should be 0 when cursors match
	count, err := d.AutoDeliveredCount("dual-cursor-test")
	if err != nil {
		t.Fatalf("AutoDeliveredCount failed: %v", err)
	}
	if count != 0 {
		t.Errorf("auto-delivered count: got %d, want 0", count)
	}

	// Insert a broadcast event after the cursor
	e := Event{
		ID:        "evt-auto-1",
		TS:        "2026-06-15T10:05:00Z",
		Source:    "test",
		Type:      "message",
		Title:     "test event",
		Broadcast: true,
	}
	if err := d.InsertEvent(e, nil, nil); err != nil {
		t.Fatalf("InsertEvent failed: %v", err)
	}

	// Advance only agent cursor (simulating auto ack)
	if err := d.AdvanceCursor("dual-cursor-test", "2026-06-15T10:10:00Z"); err != nil {
		t.Fatalf("AdvanceCursor failed: %v", err)
	}

	// Now auto-delivered count should be 1
	count, err = d.AutoDeliveredCount("dual-cursor-test")
	if err != nil {
		t.Fatalf("AutoDeliveredCount failed: %v", err)
	}
	if count != 1 {
		t.Errorf("auto-delivered count: got %d, want 1", count)
	}

	// CatchUpHumanCursor should make count 0 again
	if err := d.CatchUpHumanCursor("dual-cursor-test"); err != nil {
		t.Fatalf("CatchUpHumanCursor failed: %v", err)
	}
	count, err = d.AutoDeliveredCount("dual-cursor-test")
	if err != nil {
		t.Fatalf("AutoDeliveredCount after catch-up failed: %v", err)
	}
	if count != 0 {
		t.Errorf("auto-delivered count after catch-up: got %d, want 0", count)
	}

	// ClearHumanCursor sets it to NULL
	if err := d.ClearHumanCursor("dual-cursor-test"); err != nil {
		t.Fatalf("ClearHumanCursor failed: %v", err)
	}
	// With NULL human cursor, count should be 0
	count, err = d.AutoDeliveredCount("dual-cursor-test")
	if err != nil {
		t.Fatalf("AutoDeliveredCount after clear failed: %v", err)
	}
	if count != 0 {
		t.Errorf("auto-delivered count after clear: got %d, want 0", count)
	}
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
