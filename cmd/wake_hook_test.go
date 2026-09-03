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

// --- Stop must never interfere with a wake job -----------------------------
//
// Cancelling on Stop was removed after a live failure: Stop fires at the end of
// an assistant TURN, not when the session's work is finished — subagents may
// still be running — and a cancel-then-recreate cycle made the Stop and
// PostToolUse hooks contradict each other every turn.

func TestStopLeavesWakeJobAlone(t *testing.T) {
	d := cronTestDB(t)
	seedStopSession(t, d, "s-1")
	ts := time.Now().UTC().Format(time.RFC3339)
	if err := d.UpsertSessionCron("s-1", db.SessionCron{
		JobID: "wake", Schedule: "1 2 3 4 *", Prompt: wakeMarker + " resume",
	}, ts); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// The snapshot still reports the job, as Claude's would.
	stop := []byte(`{"session_id":"s-1","stop_hook_active":false,"session_crons":[` +
		`{"id":"wake","schedule":"1 2 3 4 *","recurring":false,"prompt":"` + wakeMarker + ` resume"}]}`)
	if err := applyStopHook(d, stop); err != nil {
		t.Fatalf("applyStopHook failed: %v", err)
	}

	crons, err := d.ListSessionCrons("s-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found bool
	for _, c := range crons {
		if isWakeCron(c) {
			found = true
		}
	}
	if !found {
		t.Error("the wake job must survive Stop — a turn ending is not the work finishing")
	}
}

// Stop still does its own job: marking the session idle and reconciling crons.
func TestStopStillReconcilesAlongsideWakeJob(t *testing.T) {
	d := cronTestDB(t)
	seedStopSession(t, d, "s-2")
	ts := time.Now().UTC().Format(time.RFC3339)
	for _, c := range []db.SessionCron{
		{JobID: "wake", Schedule: "1 2 3 4 *", Prompt: wakeMarker + " resume"},
		{JobID: "fired", Schedule: "5 6 7 8 *", Prompt: "one-shot that already fired"},
	} {
		if err := d.UpsertSessionCron("s-2", c, ts); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Snapshot omits "fired" — it auto-deleted when it fired.
	stop := []byte(`{"session_id":"s-2","session_crons":[` +
		`{"id":"wake","schedule":"1 2 3 4 *","recurring":false,"prompt":"` + wakeMarker + ` resume"}]}`)
	if err := applyStopHook(d, stop); err != nil {
		t.Fatalf("applyStopHook failed: %v", err)
	}

	crons, _ := d.ListSessionCrons("s-2")
	if len(crons) != 1 || crons[0].JobID != "wake" {
		t.Errorf("expected only the wake job to remain after reconciliation, got %+v", crons)
	}
	s, _ := d.GetSession("s-2")
	if s.Working {
		t.Error("expected Working false")
	}
}

// The prompt has to cover the case that motivated dropping cancellation: a
// session idle because it asked the user something.
func TestWakePromptHandlesWaitingOnUser(t *testing.T) {
	now := time.Date(2026, 9, 3, 13, 0, 0, 0, time.Local)
	reset := time.Date(2026, 9, 3, 15, 30, 0, 0, time.Local)
	rl := freshLimit(95, reset.UTC().Format(time.RFC3339), now)

	d := decideWake(enabledCfg(), rl, nil, now)
	if !d.Inject {
		t.Fatal("expected injection")
	}
	lower := strings.ToLower(d.Prompt)
	for _, want := range []string{"question", "restate"} {
		if !strings.Contains(lower, want) {
			t.Errorf("expected the wake prompt to mention %q, got:\n%s", want, d.Prompt)
		}
	}
}
