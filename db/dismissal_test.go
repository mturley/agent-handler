package db

import (
	"testing"
	"time"
)

func TestDismissEventExcludesFromUnread(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "sess-dismiss")

	// Cursor at epoch so all events are "after" it.
	if err := d.AdvanceCursor("sess-dismiss", "1970-01-01T00:00:00Z"); err != nil {
		t.Fatalf("AdvanceCursor: %v", err)
	}

	// Two broadcast events targeting everyone.
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range []string{"evt-keep", "evt-drop"} {
		e := Event{
			ID: id, TS: now, Source: "test", Type: "status",
			Title: "t", Broadcast: true,
		}
		if err := d.InsertEvent(e, nil, nil); err != nil {
			t.Fatalf("InsertEvent %s: %v", id, err)
		}
	}

	total, _, err := d.UnreadCountForSession("sess-dismiss")
	if err != nil {
		t.Fatalf("UnreadCountForSession: %v", err)
	}
	if total != 2 {
		t.Fatalf("before dismiss: got %d unread, want 2", total)
	}

	if err := d.DismissEvent("sess-dismiss", "evt-drop"); err != nil {
		t.Fatalf("DismissEvent: %v", err)
	}

	total, _, err = d.UnreadCountForSession("sess-dismiss")
	if err != nil {
		t.Fatalf("UnreadCountForSession after dismiss: %v", err)
	}
	if total != 1 {
		t.Fatalf("after dismiss: got %d unread, want 1", total)
	}

	unread, err := d.UnreadForSession("sess-dismiss")
	if err != nil {
		t.Fatalf("UnreadForSession: %v", err)
	}
	if len(unread) != 1 || unread[0].ID != "evt-keep" {
		t.Fatalf("expected only evt-keep, got %+v", unread)
	}
}

func TestDismissEventIsPerSession(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "sess-a")
	seedSession(t, d, "sess-b")
	d.AdvanceCursor("sess-a", "1970-01-01T00:00:00Z")
	d.AdvanceCursor("sess-b", "1970-01-01T00:00:00Z")

	now := time.Now().UTC().Format(time.RFC3339)
	e := Event{ID: "shared", TS: now, Source: "test", Type: "status", Title: "t", Broadcast: true}
	if err := d.InsertEvent(e, nil, nil); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	if err := d.DismissEvent("sess-a", "shared"); err != nil {
		t.Fatalf("DismissEvent: %v", err)
	}

	aCount, _, _ := d.UnreadCountForSession("sess-a")
	bCount, _, _ := d.UnreadCountForSession("sess-b")
	if aCount != 0 {
		t.Errorf("sess-a: got %d, want 0", aCount)
	}
	if bCount != 1 {
		t.Errorf("sess-b: got %d, want 1", bCount)
	}
}

func TestPruneDismissedBehindCursor(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "sess-prune")
	d.AdvanceCursor("sess-prune", "1970-01-01T00:00:00Z")

	e := Event{ID: "evt-old", TS: "2026-01-01T00:00:00Z", Source: "test", Type: "status", Title: "t", Broadcast: true}
	if err := d.InsertEvent(e, nil, nil); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
	if err := d.DismissEvent("sess-prune", "evt-old"); err != nil {
		t.Fatalf("DismissEvent: %v", err)
	}

	// Row exists before advancing.
	var before int
	d.conn.QueryRow(`SELECT COUNT(*) FROM dismissed_events WHERE session_id = ?`, "sess-prune").Scan(&before)
	if before != 1 {
		t.Fatalf("before advance: got %d rows, want 1", before)
	}

	// Advance cursor past the event.
	if err := d.AdvanceBothCursors("sess-prune", "2026-06-01T00:00:00Z"); err != nil {
		t.Fatalf("AdvanceBothCursors: %v", err)
	}

	var after int
	d.conn.QueryRow(`SELECT COUNT(*) FROM dismissed_events WHERE session_id = ?`, "sess-prune").Scan(&after)
	if after != 0 {
		t.Fatalf("after advance: got %d rows, want 0 (pruned)", after)
	}
}

func TestDismissEventExcludesFromDirectCount(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "sess-direct")
	d.AdvanceCursor("sess-direct", "1970-01-01T00:00:00Z")

	now := time.Now().UTC().Format(time.RFC3339)
	e := Event{ID: "direct-msg", TS: now, Source: "test", Type: "message", Title: "t"}
	recipients := []EventRecipient{{RecipientType: "session", RecipientValue: "sess-direct"}}
	if err := d.InsertEvent(e, recipients, nil); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	before, err := d.DirectCountForSession("sess-direct")
	if err != nil {
		t.Fatalf("DirectCountForSession: %v", err)
	}
	if before != 1 {
		t.Fatalf("before dismiss: got %d direct, want 1", before)
	}

	if err := d.DismissEvent("sess-direct", "direct-msg"); err != nil {
		t.Fatalf("DismissEvent: %v", err)
	}

	after, err := d.DirectCountForSession("sess-direct")
	if err != nil {
		t.Fatalf("DirectCountForSession after dismiss: %v", err)
	}
	if after != 0 {
		t.Fatalf("after dismiss: got %d direct, want 0", after)
	}
}

func TestUnreadEventsOfTypeRespectsDismissal(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "sess-typed")
	d.AdvanceCursor("sess-typed", "1970-01-01T00:00:00Z")

	now := time.Now().UTC().Format(time.RFC3339)
	recip := []EventRecipient{{RecipientType: "session", RecipientValue: "sess-typed"}}
	for _, id := range []string{"rem-1", "rem-2"} {
		e := Event{ID: id, TS: now, Source: "test", Type: "reminder", Title: id}
		if err := d.InsertEvent(e, recip, nil); err != nil {
			t.Fatalf("InsertEvent %s: %v", id, err)
		}
	}
	// A non-reminder event should not appear in the reminder listing.
	other := Event{ID: "status-1", TS: now, Source: "test", Type: "status", Title: "s", Broadcast: true}
	d.InsertEvent(other, nil, nil)

	got, err := d.UnreadEventsOfType("sess-typed", "reminder")
	if err != nil {
		t.Fatalf("UnreadEventsOfType: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("before dismiss: got %d reminders, want 2", len(got))
	}

	if err := d.DismissEvent("sess-typed", "rem-1"); err != nil {
		t.Fatalf("DismissEvent: %v", err)
	}
	got, err = d.UnreadEventsOfType("sess-typed", "reminder")
	if err != nil {
		t.Fatalf("UnreadEventsOfType after dismiss: %v", err)
	}
	if len(got) != 1 || got[0].ID != "rem-2" {
		t.Fatalf("after dismiss: expected only rem-2, got %+v", got)
	}
}

func TestPruneKeepsRowsAheadOfCursor(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "sess-keep")
	d.AdvanceCursor("sess-keep", "1970-01-01T00:00:00Z")

	e := Event{ID: "evt-future", TS: "2027-01-01T00:00:00Z", Source: "test", Type: "status", Title: "t", Broadcast: true}
	d.InsertEvent(e, nil, nil)
	d.DismissEvent("sess-keep", "evt-future")

	// Advance to a time BEFORE the event.
	if err := d.AdvanceBothCursors("sess-keep", "2026-06-01T00:00:00Z"); err != nil {
		t.Fatalf("AdvanceBothCursors: %v", err)
	}

	var after int
	d.conn.QueryRow(`SELECT COUNT(*) FROM dismissed_events WHERE session_id = ?`, "sess-keep").Scan(&after)
	if after != 1 {
		t.Fatalf("row for still-future event should be kept, got %d", after)
	}
}
