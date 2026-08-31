package cmd

import (
	"testing"
	"time"

	"github.com/mturley/agent-handler/db"
)

func seedStopSession(t *testing.T, d *db.DB, id string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := d.UpsertSession(db.Session{
		SessionID:    id,
		Harness:      "claude",
		Repo:         "github.com/example/repo",
		Branch:       "main",
		Status:       "active",
		InboxMode:    "manual",
		Working:      true,
		LastActive:   now,
		RegisteredAt: now,
		JSONLPath:    "/path/to/session.jsonl",
	}); err != nil {
		t.Fatalf("failed to seed session %q: %v", id, err)
	}
}

func TestApplyStopHookMarksSessionNotWorking(t *testing.T) {
	d := cronTestDB(t)
	seedStopSession(t, d, "stop-1")

	if err := applyStopHook(d, []byte(`{"session_id": "stop-1"}`)); err != nil {
		t.Fatalf("applyStopHook failed: %v", err)
	}

	s, err := d.GetSession("stop-1")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if s.Working {
		t.Error("expected Working false after Stop hook")
	}
}

// The Stop hook must do both jobs: mark the session idle AND reconcile crons.
func TestApplyStopHookReconcilesCrons(t *testing.T) {
	d := cronTestDB(t)
	seedStopSession(t, d, "stop-2")

	create := []byte(`{
	  "session_id": "stop-2", "tool_name": "CronCreate",
	  "tool_input": {"cron": "45 11 31 8 *", "prompt": "one-shot"},
	  "tool_response": {"id": "fired"}
	}`)
	if err := applyCronToolHook(d, create); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	stop := []byte(`{
	  "session_id": "stop-2",
	  "session_crons": [{"id": "control", "schedule": "0 4 * * *", "recurring": true}]
	}`)
	if err := applyStopHook(d, stop); err != nil {
		t.Fatalf("applyStopHook failed: %v", err)
	}

	got, err := d.ListSessionCrons("stop-2")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 1 || got[0].JobID != "control" {
		t.Fatalf("expected Stop hook to reconcile crons, got %+v", got)
	}

	s, err := d.GetSession("stop-2")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if s.Working {
		t.Error("expected Working false alongside cron reconciliation")
	}
}

// A cron sync failure must not prevent the session being marked idle — the
// working flag drives the statusline and is the more important of the two.
func TestApplyStopHookMarksNotWorkingEvenWithoutCronData(t *testing.T) {
	d := cronTestDB(t)
	seedStopSession(t, d, "stop-3")

	if err := applyStopHook(d, []byte(`{"session_id": "stop-3", "session_crons": null}`)); err != nil {
		t.Fatalf("applyStopHook failed: %v", err)
	}

	s, err := d.GetSession("stop-3")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if s.Working {
		t.Error("expected Working false")
	}
}

func TestApplyStopHookIgnoresMalformedPayload(t *testing.T) {
	d := cronTestDB(t)
	if err := applyStopHook(d, []byte(`{{{`)); err != nil {
		t.Errorf("malformed payload must not error, got: %v", err)
	}
	if err := applyStopHook(d, []byte(`{}`)); err != nil {
		t.Errorf("empty payload must not error, got: %v", err)
	}
}
