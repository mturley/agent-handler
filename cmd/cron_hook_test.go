package cmd

import (
	"testing"

	"github.com/mturley/agent-handler/db"
)

func cronTestDB(t *testing.T) *db.DB {
	t.Helper()
	t.Setenv("HANDLER_HOME", t.TempDir())
	d, err := db.Open(db.DefaultPath())
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestApplyCronToolHookRecordsCreatedJob(t *testing.T) {
	d := cronTestDB(t)

	payload := []byte(`{
	  "session_id": "s1",
	  "hook_event_name": "PostToolUse",
	  "tool_name": "CronCreate",
	  "tool_input": {"cron": "0 4 * * *", "prompt": "control job", "recurring": true},
	  "tool_response": {"id": "abc123", "humanSchedule": "0 4 * * *", "recurring": true, "durable": false}
	}`)

	if err := applyCronToolHook(d, payload); err != nil {
		t.Fatalf("applyCronToolHook failed: %v", err)
	}

	got, err := d.ListSessionCrons("s1")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 recorded cron, got %d", len(got))
	}
	if got[0].JobID != "abc123" {
		t.Errorf("expected job id %q, got %q", "abc123", got[0].JobID)
	}
	if got[0].Schedule != "0 4 * * *" {
		t.Errorf("expected schedule %q, got %q", "0 4 * * *", got[0].Schedule)
	}
	if !got[0].Recurring {
		t.Error("expected Recurring true")
	}
	if got[0].Prompt != "control job" {
		t.Errorf("expected prompt %q, got %q", "control job", got[0].Prompt)
	}
}

func TestApplyCronToolHookRemovesDeletedJob(t *testing.T) {
	d := cronTestDB(t)

	create := []byte(`{
	  "session_id": "s2", "tool_name": "CronCreate",
	  "tool_input": {"cron": "* * * * *", "prompt": "p", "recurring": false},
	  "tool_response": {"id": "doomed"}
	}`)
	if err := applyCronToolHook(d, create); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	del := []byte(`{
	  "session_id": "s2", "tool_name": "CronDelete",
	  "tool_input": {"id": "doomed"},
	  "tool_response": {"id": "doomed"}
	}`)
	if err := applyCronToolHook(d, del); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	got, err := d.ListSessionCrons("s2")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected job removed, got %d rows", len(got))
	}
}

func TestApplyCronToolHookIgnoresOtherTools(t *testing.T) {
	d := cronTestDB(t)

	payload := []byte(`{
	  "session_id": "s3", "tool_name": "Edit",
	  "tool_input": {"file_path": "/tmp/x"},
	  "tool_response": {"id": "not-a-cron"}
	}`)
	if err := applyCronToolHook(d, payload); err != nil {
		t.Fatalf("applyCronToolHook failed: %v", err)
	}

	got, err := d.ListSessionCrons("s3")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a non-cron tool must not record a job, got %d rows", len(got))
	}
}

func TestApplyCronToolHookIgnoresMalformedPayloads(t *testing.T) {
	d := cronTestDB(t)

	for name, payload := range map[string]string{
		"not json":       `{{{`,
		"no session":     `{"tool_name":"CronCreate","tool_response":{"id":"x"}}`,
		"no job id":      `{"session_id":"s4","tool_name":"CronCreate","tool_response":{}}`,
		"delete no id":   `{"session_id":"s4","tool_name":"CronDelete","tool_input":{}}`,
		"empty envelope": `{}`,
	} {
		if err := applyCronToolHook(d, []byte(payload)); err != nil {
			t.Errorf("%s: hook must not error, got: %v", name, err)
		}
	}

	got, err := d.ListSessionCrons("s4")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("malformed payloads must record nothing, got %d rows", len(got))
	}
}

// CronCreate's tool_response carries humanSchedule, which for a plain cron
// expression matches tool_input.cron. tool_input.cron is the raw expression and
// is preferred; humanSchedule is the fallback when input is unavailable.
func TestApplyCronToolHookFallsBackToHumanSchedule(t *testing.T) {
	d := cronTestDB(t)

	payload := []byte(`{
	  "session_id": "s5", "tool_name": "CronCreate",
	  "tool_response": {"id": "j5", "humanSchedule": "30 9 * * 1-5", "recurring": true}
	}`)
	if err := applyCronToolHook(d, payload); err != nil {
		t.Fatalf("applyCronToolHook failed: %v", err)
	}

	got, err := d.ListSessionCrons("s5")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 1 || got[0].Schedule != "30 9 * * 1-5" {
		t.Fatalf("expected humanSchedule fallback, got %+v", got)
	}
	if !got[0].Recurring {
		t.Error("expected recurring from tool_response")
	}
}

func TestApplyStopHookCronsReconcilesSnapshot(t *testing.T) {
	d := cronTestDB(t)

	create := []byte(`{
	  "session_id": "s6", "tool_name": "CronCreate",
	  "tool_input": {"cron": "45 11 31 8 *", "prompt": "one-shot", "recurring": false},
	  "tool_response": {"id": "oneshot"}
	}`)
	if err := applyCronToolHook(d, create); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Stop snapshot after the one-shot fired: it is gone, a control remains.
	stop := []byte(`{
	  "session_id": "s6",
	  "hook_event_name": "Stop",
	  "session_crons": [
	    {"id": "control", "schedule": "0 4 * * *", "recurring": true, "prompt": "control job"}
	  ]
	}`)
	if err := applyStopHookCrons(d, stop); err != nil {
		t.Fatalf("applyStopHookCrons failed: %v", err)
	}

	got, err := d.ListSessionCrons("s6")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected reconciliation to leave 1 row, got %d: %+v", len(got), got)
	}
	if got[0].JobID != "control" {
		t.Errorf("expected fired job dropped and control inserted, got %q", got[0].JobID)
	}
}

func TestApplyStopHookCronsWithEmptyArrayClearsSession(t *testing.T) {
	d := cronTestDB(t)

	create := []byte(`{
	  "session_id": "s7", "tool_name": "CronCreate",
	  "tool_input": {"cron": "* * * * *"},
	  "tool_response": {"id": "stale"}
	}`)
	if err := applyCronToolHook(d, create); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	stop := []byte(`{"session_id": "s7", "session_crons": []}`)
	if err := applyStopHookCrons(d, stop); err != nil {
		t.Fatalf("applyStopHookCrons failed: %v", err)
	}

	got, err := d.ListSessionCrons("s7")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("an explicitly empty snapshot means no jobs; got %d rows", len(got))
	}
}

// The critical degenerate case: a Stop payload with NO session_crons key at all
// (older Claude Code, or a payload shape change) means "unknown", not "no
// jobs". Wiping on absence would silently destroy correct state.
func TestApplyStopHookCronsWithAbsentFieldDoesNotWipe(t *testing.T) {
	d := cronTestDB(t)

	create := []byte(`{
	  "session_id": "s8", "tool_name": "CronCreate",
	  "tool_input": {"cron": "0 4 * * *", "recurring": true},
	  "tool_response": {"id": "keepme"}
	}`)
	if err := applyCronToolHook(d, create); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	stop := []byte(`{"session_id": "s8", "hook_event_name": "Stop"}`)
	if err := applyStopHookCrons(d, stop); err != nil {
		t.Fatalf("applyStopHookCrons failed: %v", err)
	}

	got, err := d.ListSessionCrons("s8")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("absent session_crons must not clear rows, got %d", len(got))
	}
}

func TestApplyStopHookCronsIgnoresMalformedPayloads(t *testing.T) {
	d := cronTestDB(t)

	for name, payload := range map[string]string{
		"not json":   `{{{`,
		"no session": `{"session_crons": []}`,
	} {
		if err := applyStopHookCrons(d, []byte(payload)); err != nil {
			t.Errorf("%s: hook must not error, got: %v", name, err)
		}
	}
}

// A snapshot entry with no id is unusable; it must be skipped rather than
// written as an empty-id row, and must not prune the rest of the snapshot.
func TestApplyStopHookCronsSkipsEntriesWithoutID(t *testing.T) {
	d := cronTestDB(t)

	stop := []byte(`{
	  "session_id": "s9",
	  "session_crons": [
	    {"id": "", "schedule": "* * * * *"},
	    {"id": "good", "schedule": "0 4 * * *", "recurring": true}
	  ]
	}`)
	if err := applyStopHookCrons(d, stop); err != nil {
		t.Fatalf("applyStopHookCrons failed: %v", err)
	}

	got, err := d.ListSessionCrons("s9")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 1 || got[0].JobID != "good" {
		t.Fatalf("expected only the id-bearing entry, got %+v", got)
	}
}
