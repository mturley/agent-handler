package db

import (
	"testing"
	"time"
)

func TestUpsertAndGetPeekState(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "peek-test")

	now := time.Now().UTC().Format(time.RFC3339)
	err := d.UpsertPeekState("peek-test", "terminal content here", true, "awaiting approval", now)
	if err != nil {
		t.Fatalf("UpsertPeekState failed: %v", err)
	}

	ps, err := d.GetPeekState("peek-test")
	if err != nil {
		t.Fatalf("GetPeekState failed: %v", err)
	}
	if ps == nil {
		t.Fatal("expected non-nil PeekState")
	}
	if ps.Content != "terminal content here" {
		t.Errorf("expected content %q, got %q", "terminal content here", ps.Content)
	}
	if !ps.NeedsInput {
		t.Error("expected NeedsInput true")
	}
	if ps.Reason != "awaiting approval" {
		t.Errorf("expected reason %q, got %q", "awaiting approval", ps.Reason)
	}
}

func TestUpsertPeekStateOverwrites(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "peek-overwrite")

	now := time.Now().UTC().Format(time.RFC3339)
	d.UpsertPeekState("peek-overwrite", "old content", true, "awaiting approval", now)
	d.UpsertPeekState("peek-overwrite", "new content", false, "", now)

	ps, err := d.GetPeekState("peek-overwrite")
	if err != nil {
		t.Fatalf("GetPeekState failed: %v", err)
	}
	if ps.Content != "new content" {
		t.Errorf("expected updated content, got %q", ps.Content)
	}
	if ps.NeedsInput {
		t.Error("expected NeedsInput false after update")
	}
}

func TestGetPeekStateNotFound(t *testing.T) {
	d := testDB(t)

	ps, err := d.GetPeekState("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ps != nil {
		t.Error("expected nil for nonexistent session")
	}
}

func TestListPeekStates(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "peek-list-1")
	seedSession(t, d, "peek-list-2")

	now := time.Now().UTC().Format(time.RFC3339)
	d.UpsertPeekState("peek-list-1", "content 1", true, "awaiting approval", now)
	d.UpsertPeekState("peek-list-2", "content 2", false, "", now)

	states, err := d.ListPeekStates()
	if err != nil {
		t.Fatalf("ListPeekStates failed: %v", err)
	}
	if len(states) != 2 {
		t.Errorf("expected 2 states, got %d", len(states))
	}
}

func TestPeekStatesAge(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "peek-age")

	// With no rows, age should be very large
	age, err := d.PeekStatesAge()
	if err != nil {
		t.Fatalf("PeekStatesAge failed: %v", err)
	}
	if age < time.Hour {
		t.Errorf("expected large age for empty table, got %v", age)
	}

	// Insert a row with current timestamp
	now := time.Now().UTC().Format(time.RFC3339)
	d.UpsertPeekState("peek-age", "content", false, "", now)

	age, err = d.PeekStatesAge()
	if err != nil {
		t.Fatalf("PeekStatesAge failed: %v", err)
	}
	if age > 2*time.Second {
		t.Errorf("expected small age for fresh data, got %v", age)
	}
}

func TestDeletePeekStatesForSessions(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "peek-del-1")
	seedSession(t, d, "peek-del-2")

	now := time.Now().UTC().Format(time.RFC3339)
	d.UpsertPeekState("peek-del-1", "c1", false, "", now)
	d.UpsertPeekState("peek-del-2", "c2", false, "", now)

	err := d.DeletePeekStatesForSessions([]string{"peek-del-1"})
	if err != nil {
		t.Fatalf("DeletePeekStatesForSessions failed: %v", err)
	}

	ps, _ := d.GetPeekState("peek-del-1")
	if ps != nil {
		t.Error("expected nil after delete")
	}
	ps, _ = d.GetPeekState("peek-del-2")
	if ps == nil {
		t.Error("expected non-nil for non-deleted session")
	}
}
