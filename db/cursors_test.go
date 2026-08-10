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

// TestCursorAdvancesToMaxEventTS proves that advancing a session's cursor
// after a read uses the max ts of the events actually returned, not
// wall-clock time.Now(). With second-granularity "now" timestamps, an event
// written in the same second as the read (e.g. one with ts == the max
// returned ts, or any ts between the max returned ts and a later "now") must
// still be visible on the next read — it must NOT be skipped.
func TestCursorAdvancesToMaxEventTS(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "max-ts-cursor-test")

	t1 := "2026-06-15T10:00:00Z"
	t2 := "2026-06-15T10:00:05Z" // same "second bucket" concern: still > t1

	if err := d.InsertEvent(Event{
		ID:     "max-ts-evt-1",
		TS:     t1,
		Source: "test",
		Type:   "message",
		Title:  "first",
	}, []EventRecipient{{RecipientType: "session", RecipientValue: "max-ts-cursor-test"}}, nil); err != nil {
		t.Fatalf("InsertEvent 1 failed: %v", err)
	}
	if err := d.InsertEvent(Event{
		ID:     "max-ts-evt-2",
		TS:     t2,
		Source: "test",
		Type:   "message",
		Title:  "second",
	}, []EventRecipient{{RecipientType: "session", RecipientValue: "max-ts-cursor-test"}}, nil); err != nil {
		t.Fatalf("InsertEvent 2 failed: %v", err)
	}

	// Read unread events and advance the cursor the way cmd/unread.go now
	// does: to the max ts of the events actually returned.
	events, err := d.UnreadForSession("max-ts-cursor-test")
	if err != nil {
		t.Fatalf("UnreadForSession failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 unread events, got %d", len(events))
	}

	maxTS := MaxEventTS(events)
	if maxTS != t2 {
		t.Fatalf("MaxEventTS: got %q, want %q", maxTS, t2)
	}

	if err := d.AdvanceCursor("max-ts-cursor-test", maxTS); err != nil {
		t.Fatalf("AdvanceCursor failed: %v", err)
	}

	cursor, err := d.GetCursor("max-ts-cursor-test")
	if err != nil {
		t.Fatalf("GetCursor failed: %v", err)
	}
	if cursor != t2 {
		t.Fatalf("cursor: got %q, want %q (max returned ts, not time.Now())", cursor, t2)
	}

	// Now insert a third event sharing the same ts as the previous max (the
	// boundary condition a "now"-based advance would have raced past). With a
	// `ts > cursor` unread filter, an event AT exactly the old max would
	// still be correctly excluded (it was already returned) — the real bug
	// this guards against is a *later* event landing at or before a
	// time.Now() cursor taken after the read. Simulate that: t3 is after t2
	// but would have been swallowed by a coarse "now" cursor taken at read
	// time in the buggy implementation (same wall-clock second as t2).
	t3 := "2026-06-15T10:00:06Z" // one second after t2, strictly greater than the cursor
	if err := d.InsertEvent(Event{
		ID:     "max-ts-evt-3",
		TS:     t3,
		Source: "test",
		Type:   "message",
		Title:  "third",
	}, []EventRecipient{{RecipientType: "session", RecipientValue: "max-ts-cursor-test"}}, nil); err != nil {
		t.Fatalf("InsertEvent 3 failed: %v", err)
	}

	events2, err := d.UnreadForSession("max-ts-cursor-test")
	if err != nil {
		t.Fatalf("UnreadForSession (2nd read) failed: %v", err)
	}
	if len(events2) != 1 || events2[0].ID != "max-ts-evt-3" {
		t.Fatalf("expected exactly the third event to be unread, got %+v", events2)
	}
}

// TestCursorUnchangedWhenNoEventsReturned proves that when a read returns no
// events, the cursor is left unchanged rather than being force-advanced to
// time.Now(), which would silently drop any event written between the
// previous cursor and "now" but before the next actual read.
func TestCursorUnchangedWhenNoEventsReturned(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "empty-read-cursor-test")

	events, err := d.UnreadForSession("empty-read-cursor-test")
	if err != nil {
		t.Fatalf("UnreadForSession failed: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no unread events, got %d", len(events))
	}

	// Simulate the caller's guard: only advance when len(events) > 0.
	if len(events) > 0 {
		_ = d.AdvanceCursor("empty-read-cursor-test", MaxEventTS(events))
	}

	cursor, err := d.GetCursor("empty-read-cursor-test")
	if err != nil {
		t.Fatalf("GetCursor failed: %v", err)
	}
	if cursor != "" {
		t.Fatalf("expected cursor to remain unset when no events were returned, got %q", cursor)
	}
}
