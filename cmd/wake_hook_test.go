package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/mturley/agent-handler/db"
)

func seedLimit(t *testing.T, d *db.DB, sessionID string, pct float64, resets time.Time, updated time.Time) {
	t.Helper()
	if err := d.UpsertRateLimit(sessionID, pct,
		resets.UTC().Format(time.RFC3339), updated.UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("seed rate limit: %v", err)
	}
}

func TestWakeCheckMessageReturnsInstructionOverThreshold(t *testing.T) {
	d := cronTestDB(t)
	now := time.Now()
	seedLimit(t, d, "w-1", 93, now.Add(2*time.Hour), now)

	msg := wakeCheckMessage(d, enabledCfg(), "w-1", now)
	if msg == "" {
		t.Fatal("expected a wake instruction")
	}
	for _, want := range []string{"[agent-handler wake]", "CronCreate", wakeMarker} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected %q in instruction, got:\n%s", want, msg)
		}
	}
}

func TestWakeCheckMessageEmptyBelowThreshold(t *testing.T) {
	d := cronTestDB(t)
	now := time.Now()
	seedLimit(t, d, "w-2", 50, now.Add(2*time.Hour), now)

	if msg := wakeCheckMessage(d, enabledCfg(), "w-2", now); msg != "" {
		t.Errorf("expected no instruction below threshold, got %q", msg)
	}
}

// The hook does the existing-job check itself, so a session that already has a
// wake job is never told to make another.
func TestWakeCheckMessageEmptyWhenWakeJobAlreadyTracked(t *testing.T) {
	d := cronTestDB(t)
	now := time.Now()
	seedLimit(t, d, "w-3", 96, now.Add(2*time.Hour), now)
	if err := d.UpsertSessionCron("w-3", db.SessionCron{
		JobID: "w", Schedule: "1 2 3 4 *", Prompt: wakeMarker + " resume",
	}, now.UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("seed cron: %v", err)
	}

	if msg := wakeCheckMessage(d, enabledCfg(), "w-3", now); msg != "" {
		t.Errorf("expected no instruction when a wake job exists, got %q", msg)
	}
}

func TestWakeCheckMessageEmptyWhenDisabled(t *testing.T) {
	d := cronTestDB(t)
	now := time.Now()
	seedLimit(t, d, "w-4", 99, now.Add(2*time.Hour), now)

	if msg := wakeCheckMessage(d, disabledCfg(), "w-4", now); msg != "" {
		t.Errorf("expected no instruction when disabled, got %q", msg)
	}
}

func TestWakeCheckMessageEmptyWithNoRateLimitRow(t *testing.T) {
	d := cronTestDB(t)
	if msg := wakeCheckMessage(d, enabledCfg(), "w-unknown", time.Now()); msg != "" {
		t.Errorf("expected no instruction without rate limit data, got %q", msg)
	}
}

// --- cleanup ---------------------------------------------------------------

func TestWakeCleanupRemovesOnlyWakeJobs(t *testing.T) {
	d := cronTestDB(t)
	ts := time.Now().UTC().Format(time.RFC3339)
	if err := d.UpsertSessionCron("c-1", db.SessionCron{JobID: "wake", Schedule: "1 2 3 4 *", Prompt: wakeMarker + " resume"}, ts); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := d.UpsertSessionCron("c-1", db.SessionCron{JobID: "other", Schedule: "*/5 * * * *", Prompt: "/inbox --auto"}, ts); err != nil {
		t.Fatalf("seed: %v", err)
	}

	removed := wakeCleanupJobIDs(d, "c-1")
	if len(removed) != 1 || removed[0] != "wake" {
		t.Fatalf("expected only the wake job id, got %v", removed)
	}
}

func TestWakeCleanupReturnsNothingWhenNoWakeJob(t *testing.T) {
	d := cronTestDB(t)
	ts := time.Now().UTC().Format(time.RFC3339)
	if err := d.UpsertSessionCron("c-2", db.SessionCron{JobID: "other", Schedule: "*/5 * * * *", Prompt: "/inbox"}, ts); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if ids := wakeCleanupJobIDs(d, "c-2"); len(ids) != 0 {
		t.Errorf("expected no ids, got %v", ids)
	}
}

// --- PreToolUse allow guard ------------------------------------------------

func TestCronCreateAllowedForWakeJobWhileOverThreshold(t *testing.T) {
	d := cronTestDB(t)
	now := time.Now()
	seedLimit(t, d, "p-1", 95, now.Add(2*time.Hour), now)

	if !shouldAllowCronCreate(d, enabledCfg(), "p-1", wakeMarker+" resume your work", false, now) {
		t.Error("expected a marked one-shot to be allowed while over threshold")
	}
}

func TestCronCreateNotAllowedWithoutMarker(t *testing.T) {
	d := cronTestDB(t)
	now := time.Now()
	seedLimit(t, d, "p-2", 95, now.Add(2*time.Hour), now)

	if shouldAllowCronCreate(d, enabledCfg(), "p-2", "some other job", false, now) {
		t.Error("an unmarked job must not be auto-approved")
	}
}

// Auto-approval is for a one-shot wake job only; a recurring job carrying the
// marker must still go through normal permission handling.
func TestCronCreateNotAllowedWhenRecurring(t *testing.T) {
	d := cronTestDB(t)
	now := time.Now()
	seedLimit(t, d, "p-3", 95, now.Add(2*time.Hour), now)

	if shouldAllowCronCreate(d, enabledCfg(), "p-3", wakeMarker+" resume", true, now) {
		t.Error("a recurring job must not be auto-approved")
	}
}

// The grant exists only while handler is actually asking for a wake job.
func TestCronCreateNotAllowedBelowThreshold(t *testing.T) {
	d := cronTestDB(t)
	now := time.Now()
	seedLimit(t, d, "p-4", 40, now.Add(2*time.Hour), now)

	if shouldAllowCronCreate(d, enabledCfg(), "p-4", wakeMarker+" resume", false, now) {
		t.Error("must not auto-approve when usage is below threshold")
	}
}

func TestCronCreateNotAllowedWhenDisabled(t *testing.T) {
	d := cronTestDB(t)
	now := time.Now()
	seedLimit(t, d, "p-5", 99, now.Add(2*time.Hour), now)

	if shouldAllowCronCreate(d, disabledCfg(), "p-5", wakeMarker+" resume", false, now) {
		t.Error("must not auto-approve when the feature is disabled")
	}
}

func TestCronCreateNotAllowedOnStaleData(t *testing.T) {
	d := cronTestDB(t)
	now := time.Now()
	seedLimit(t, d, "p-6", 99, now.Add(2*time.Hour), now.Add(-45*time.Minute))

	if shouldAllowCronCreate(d, enabledCfg(), "p-6", wakeMarker+" resume", false, now) {
		t.Error("must not auto-approve on stale rate limit data")
	}
}

// --- Stop cleanup with the StopFailure guard --------------------------------

func TestStopCleanupRemovesWakeJobAfterNormalTurn(t *testing.T) {
	d := cronTestDB(t)
	now := time.Now()
	ts := now.UTC().Format(time.RFC3339)
	if err := d.UpsertSessionCron("s-1", db.SessionCron{JobID: "wake", Schedule: "1 2 3 4 *", Prompt: wakeMarker + " resume"}, ts); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ids := wakeStopCleanupIDs(d, "s-1", now)
	if len(ids) != 1 || ids[0] != "wake" {
		t.Fatalf("expected the wake job to be cleaned up, got %v", ids)
	}
}

// If the turn died on a rate limit, the wake job is exactly what is needed —
// cleaning it up then would silently defeat the whole feature. This makes the
// unknown Stop-vs-StopFailure ordering irrelevant.
func TestStopCleanupSkippedAfterRateLimitFailure(t *testing.T) {
	d := cronTestDB(t)
	now := time.Now()
	ts := now.UTC().Format(time.RFC3339)
	if err := d.UpsertSessionCron("s-2", db.SessionCron{JobID: "wake", Schedule: "1 2 3 4 *", Prompt: wakeMarker + " resume"}, ts); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := d.RecordRateLimitError("s-2", ts); err != nil {
		t.Fatalf("record error: %v", err)
	}

	if ids := wakeStopCleanupIDs(d, "s-2", now); len(ids) != 0 {
		t.Errorf("expected cleanup to be skipped after a rate-limit failure, got %v", ids)
	}
}

// An old failure must not suppress cleanup forever.
func TestStopCleanupResumesAfterOldRateLimitFailure(t *testing.T) {
	d := cronTestDB(t)
	now := time.Now()
	ts := now.UTC().Format(time.RFC3339)
	if err := d.UpsertSessionCron("s-3", db.SessionCron{JobID: "wake", Schedule: "1 2 3 4 *", Prompt: wakeMarker + " resume"}, ts); err != nil {
		t.Fatalf("seed: %v", err)
	}
	old := now.Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	if err := d.RecordRateLimitError("s-3", old); err != nil {
		t.Fatalf("record error: %v", err)
	}

	if ids := wakeStopCleanupIDs(d, "s-3", now); len(ids) != 1 {
		t.Errorf("expected cleanup to resume after an old failure, got %v", ids)
	}
}

// --- forced cleanup via Stop exit 2 ----------------------------------------

func TestStopForcesDeleteWhenWakeJobPresent(t *testing.T) {
	d := cronTestDB(t)
	now := time.Now()
	ts := now.UTC().Format(time.RFC3339)
	if err := d.UpsertSessionCron("f-1", db.SessionCron{JobID: "wake", Schedule: "1 2 3 4 *", Prompt: wakeMarker + " resume"}, ts); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ids, force := wakeStopDecision(d, "f-1", now, false)
	if !force {
		t.Fatal("expected Stop to force a continuation to delete the wake job")
	}
	if len(ids) != 1 || ids[0] != "wake" {
		t.Errorf("expected the wake job id, got %v", ids)
	}
}

// THE loop guard: once a stop hook is already holding the session open, never
// force again, or the session can never stop.
func TestStopDoesNotForceWhenStopHookAlreadyActive(t *testing.T) {
	d := cronTestDB(t)
	now := time.Now()
	ts := now.UTC().Format(time.RFC3339)
	if err := d.UpsertSessionCron("f-2", db.SessionCron{JobID: "wake", Schedule: "1 2 3 4 *", Prompt: wakeMarker + " resume"}, ts); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, force := wakeStopDecision(d, "f-2", now, true); force {
		t.Error("must not force a continuation when stop_hook_active is set")
	}
}

func TestStopDoesNotForceWithoutWakeJob(t *testing.T) {
	d := cronTestDB(t)
	now := time.Now()
	ts := now.UTC().Format(time.RFC3339)
	if err := d.UpsertSessionCron("f-3", db.SessionCron{JobID: "other", Schedule: "*/5 * * * *", Prompt: "/inbox"}, ts); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, force := wakeStopDecision(d, "f-3", now, false); force {
		t.Error("must not force a continuation when there is no wake job")
	}
}

func TestStopDoesNotForceAfterRateLimitFailure(t *testing.T) {
	d := cronTestDB(t)
	now := time.Now()
	ts := now.UTC().Format(time.RFC3339)
	if err := d.UpsertSessionCron("f-4", db.SessionCron{JobID: "wake", Schedule: "1 2 3 4 *", Prompt: wakeMarker + " resume"}, ts); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := d.RecordRateLimitError("f-4", ts); err != nil {
		t.Fatalf("record error: %v", err)
	}
	// Rate-limited: the job is needed, and a forced continuation would fail anyway.
	if _, force := wakeStopDecision(d, "f-4", now, false); force {
		t.Error("must not force a continuation right after a rate-limit failure")
	}
}

func TestWakeDeleteInstructionNamesTheJobIDs(t *testing.T) {
	msg := wakeDeleteInstruction([]string{"abc123"})
	for _, want := range []string{"CronDelete", "abc123"} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected %q in %q", want, msg)
		}
	}
}
