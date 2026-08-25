package db

import (
	"testing"
	"time"
)

func seedReminderSession(t *testing.T, db *DB, id, registeredAt string) {
	t.Helper()
	if err := db.UpsertSession(Session{
		SessionID:    id,
		Harness:      "claude",
		Repo:         "github.com/example/repo",
		Branch:       "main",
		Status:       "active",
		InboxMode:    "manual",
		LastActive:   registeredAt,
		RegisteredAt: registeredAt,
		JSONLPath:    "/tmp/session.jsonl",
	}); err != nil {
		t.Fatalf("UpsertSession failed: %v", err)
	}
}

func TestStatusReminderStateDefaultsToRegisteredAt(t *testing.T) {
	db := testDB(t)
	registered := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	seedReminderSession(t, db, "s1", registered)

	prompts, baseline, err := db.StatusReminderState("s1")
	if err != nil {
		t.Fatalf("StatusReminderState failed: %v", err)
	}
	if prompts != 0 {
		t.Errorf("expected 0 prompts for a fresh session, got %d", prompts)
	}
	if baseline != registered {
		t.Errorf("expected baseline to fall back to registered_at %q, got %q", registered, baseline)
	}
}

func TestBumpPromptsSinceStatusIncrements(t *testing.T) {
	db := testDB(t)
	seedReminderSession(t, db, "s1", time.Now().UTC().Format(time.RFC3339))

	for want := 1; want <= 3; want++ {
		got, err := db.BumpPromptsSinceStatus("s1")
		if err != nil {
			t.Fatalf("BumpPromptsSinceStatus failed: %v", err)
		}
		if got != want {
			t.Errorf("bump %d: expected %d, got %d", want, want, got)
		}
	}

	prompts, _, err := db.StatusReminderState("s1")
	if err != nil {
		t.Fatalf("StatusReminderState failed: %v", err)
	}
	if prompts != 3 {
		t.Errorf("expected persisted count 3, got %d", prompts)
	}
}

func TestResetStatusReminderBaselineClearsCountAndSetsBaseline(t *testing.T) {
	db := testDB(t)
	registered := time.Now().UTC().Add(-5 * time.Hour).Format(time.RFC3339)
	seedReminderSession(t, db, "s1", registered)

	for i := 0; i < 7; i++ {
		if _, err := db.BumpPromptsSinceStatus("s1"); err != nil {
			t.Fatalf("BumpPromptsSinceStatus failed: %v", err)
		}
	}

	reset := time.Now().UTC().Format(time.RFC3339)
	if err := db.ResetStatusReminderBaseline("s1", reset); err != nil {
		t.Fatalf("ResetStatusReminderBaseline failed: %v", err)
	}

	prompts, baseline, err := db.StatusReminderState("s1")
	if err != nil {
		t.Fatalf("StatusReminderState failed: %v", err)
	}
	if prompts != 0 {
		t.Errorf("expected count reset to 0, got %d", prompts)
	}
	if baseline != reset {
		t.Errorf("expected baseline %q, got %q", reset, baseline)
	}
}

func TestStatusReminderStateUnknownSession(t *testing.T) {
	db := testDB(t)
	_, _, err := db.StatusReminderState("nope")
	if err == nil {
		t.Error("expected an error for an unknown session")
	}
}

func TestBumpPromptsSinceStatusSurvivesUpsert(t *testing.T) {
	// The UserPromptSubmit hook re-upserts session metadata on every prompt;
	// that must not wipe the reminder counter.
	db := testDB(t)
	registered := time.Now().UTC().Format(time.RFC3339)
	seedReminderSession(t, db, "s1", registered)

	if _, err := db.BumpPromptsSinceStatus("s1"); err != nil {
		t.Fatalf("BumpPromptsSinceStatus failed: %v", err)
	}
	seedReminderSession(t, db, "s1", registered)

	prompts, _, err := db.StatusReminderState("s1")
	if err != nil {
		t.Fatalf("StatusReminderState failed: %v", err)
	}
	if prompts != 1 {
		t.Errorf("expected the counter to survive re-upsert, got %d", prompts)
	}
}
