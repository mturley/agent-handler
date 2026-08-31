package cmd

import (
	"strings"
	"testing"

	"github.com/mturley/agent-handler/db"
)

func TestRenderCronsEmpty(t *testing.T) {
	out := renderCrons(nil, false)
	if !strings.Contains(out, "No cron jobs") {
		t.Errorf("expected an empty-state message, got %q", out)
	}
}

func TestRenderCronsShowsScheduleAndRecurrence(t *testing.T) {
	crons := []db.SessionCron{
		{SessionID: "s1", JobID: "abc123", Schedule: "0 4 * * *", Recurring: true, Prompt: "control job"},
		{SessionID: "s1", JobID: "def456", Schedule: "45 11 31 8 *", Recurring: false, Prompt: "one-shot"},
	}
	out := renderCrons(crons, false)

	for _, want := range []string{"abc123", "0 4 * * *", "recurring", "def456", "45 11 31 8 *", "one-shot"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestRenderCronsJSONIncludesAllFields(t *testing.T) {
	crons := []db.SessionCron{
		{SessionID: "s1", JobID: "abc123", Schedule: "0 4 * * *", Recurring: true, Prompt: "p", CreatedAt: "2026-08-31T00:00:00Z", LastSeen: "2026-08-31T01:00:00Z"},
	}
	out := renderCrons(crons, true)

	for _, want := range []string{`"session_id"`, `"job_id"`, `"schedule"`, `"recurring"`, `"prompt"`, `"created_at"`, `"last_seen_at"`} {
		if !strings.Contains(out, want) {
			t.Errorf("expected JSON to contain %q, got:\n%s", want, out)
		}
	}
}

func TestRenderCronsJSONEmptyIsEmptyArray(t *testing.T) {
	out := strings.TrimSpace(renderCrons(nil, true))
	if out != "[]" {
		t.Errorf("expected empty JSON array, got %q", out)
	}
}

// A one-shot job's prompt is the only thing distinguishing it in a list, so a
// long prompt must be truncated rather than blowing up the table.
func TestRenderCronsTruncatesLongPrompts(t *testing.T) {
	long := strings.Repeat("x", 300)
	out := renderCrons([]db.SessionCron{{JobID: "j", Schedule: "* * * * *", Prompt: long}}, false)

	for _, line := range strings.Split(out, "\n") {
		if len(line) > 200 {
			t.Errorf("expected prompt truncation, got a %d-char line", len(line))
		}
	}
}
