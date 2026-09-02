package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
)

func enabledCfg() *config.Config { return &config.Config{} }

func disabledCfg() *config.Config {
	no := false
	return &config.Config{AutoWake: &config.AutoWakeConfig{Enabled: &no}}
}

func freshLimit(pct float64, resetsAt string, now time.Time) *db.RateLimitState {
	return &db.RateLimitState{
		FiveHourPercent:  pct,
		FiveHourResetsAt: resetsAt,
		UpdatedAt:        now.UTC().Format(time.RFC3339),
	}
}

func TestWakeDecisionFiresAboveThreshold(t *testing.T) {
	now := time.Date(2026, 9, 2, 13, 0, 0, 0, time.Local)
	rl := freshLimit(92, now.Add(2*time.Hour).UTC().Format(time.RFC3339), now)

	d := decideWake(enabledCfg(), rl, nil, now)
	if !d.Inject {
		t.Fatalf("expected injection, got %+v", d)
	}
	if !strings.Contains(d.Prompt, wakeMarker) {
		t.Errorf("expected wake marker in prompt, got %q", d.Prompt)
	}
	if d.Cron == "" {
		t.Error("expected a cron expression")
	}
}

func TestWakeDecisionSilentBelowThreshold(t *testing.T) {
	now := time.Date(2026, 9, 2, 13, 0, 0, 0, time.Local)
	rl := freshLimit(89.9, now.Add(2*time.Hour).UTC().Format(time.RFC3339), now)

	if d := decideWake(enabledCfg(), rl, nil, now); d.Inject {
		t.Errorf("expected no injection below threshold, got %+v", d)
	}
}

func TestWakeDecisionDisabledByConfig(t *testing.T) {
	now := time.Date(2026, 9, 2, 13, 0, 0, 0, time.Local)
	rl := freshLimit(99, now.Add(2*time.Hour).UTC().Format(time.RFC3339), now)

	if d := decideWake(disabledCfg(), rl, nil, now); d.Inject {
		t.Error("expected config to disable injection entirely")
	}
}

func TestWakeDecisionSilentWithoutRateLimitData(t *testing.T) {
	now := time.Date(2026, 9, 2, 13, 0, 0, 0, time.Local)
	if d := decideWake(enabledCfg(), nil, nil, now); d.Inject {
		t.Error("absent rate limit data means unknown, not over threshold")
	}
}

// Statusline is the only writer; if it stopped, the row must not be trusted.
func TestWakeDecisionSilentOnStaleData(t *testing.T) {
	now := time.Date(2026, 9, 2, 13, 0, 0, 0, time.Local)
	rl := &db.RateLimitState{
		FiveHourPercent:  99,
		FiveHourResetsAt: now.Add(2 * time.Hour).UTC().Format(time.RFC3339),
		UpdatedAt:        now.Add(-45 * time.Minute).UTC().Format(time.RFC3339),
	}
	if d := decideWake(enabledCfg(), rl, nil, now); d.Inject {
		t.Error("expected stale rate limit data to suppress injection")
	}
}

// This is the hook-side guard the user asked for: never instruct Claude to
// check for itself, and never inject when a wake job already exists.
func TestWakeDecisionSilentWhenWakeJobExists(t *testing.T) {
	now := time.Date(2026, 9, 2, 13, 0, 0, 0, time.Local)
	rl := freshLimit(95, now.Add(2*time.Hour).UTC().Format(time.RFC3339), now)
	existing := []db.SessionCron{
		{JobID: "w1", Schedule: "1 15 2 9 *", Prompt: wakeMarker + " resume your work"},
	}
	if d := decideWake(enabledCfg(), rl, existing, now); d.Inject {
		t.Error("expected no injection when a wake job is already tracked")
	}
}

// An unrelated cron job must not be mistaken for a wake job.
func TestWakeDecisionFiresWhenOnlyUnrelatedCronsExist(t *testing.T) {
	now := time.Date(2026, 9, 2, 13, 0, 0, 0, time.Local)
	rl := freshLimit(95, now.Add(2*time.Hour).UTC().Format(time.RFC3339), now)
	other := []db.SessionCron{{JobID: "o1", Schedule: "*/5 * * * *", Prompt: "/inbox --auto"}}

	if d := decideWake(enabledCfg(), rl, other, now); !d.Inject {
		t.Error("an unrelated cron job must not suppress the wake job")
	}
}

func TestWakeDecisionSilentWhenResetTimeMissingOrUnparseable(t *testing.T) {
	now := time.Date(2026, 9, 2, 13, 0, 0, 0, time.Local)
	for _, bad := range []string{"", "not a time"} {
		rl := freshLimit(95, bad, now)
		if d := decideWake(enabledCfg(), rl, nil, now); d.Inject {
			t.Errorf("resets_at %q: expected no injection", bad)
		}
	}
}

// A reset already in the past means the limit has recovered; nothing to wake for.
func TestWakeDecisionSilentWhenResetAlreadyPassed(t *testing.T) {
	now := time.Date(2026, 9, 2, 13, 0, 0, 0, time.Local)
	rl := freshLimit(95, now.Add(-10*time.Minute).UTC().Format(time.RFC3339), now)
	if d := decideWake(enabledCfg(), rl, nil, now); d.Inject {
		t.Error("expected no injection when the reset time has already passed")
	}
}

// The job must fire one minute AFTER the reset, pinned to that exact date so it
// is a true one-shot.
func TestWakeCronExpressionIsOneMinuteAfterResetInLocalTime(t *testing.T) {
	now := time.Date(2026, 9, 2, 13, 0, 0, 0, time.Local)
	reset := time.Date(2026, 9, 2, 15, 30, 0, 0, time.Local)
	rl := freshLimit(95, reset.UTC().Format(time.RFC3339), now)

	d := decideWake(enabledCfg(), rl, nil, now)
	if !d.Inject {
		t.Fatal("expected injection")
	}
	want := "31 15 2 9 *" // 15:31 local on Sep 2
	if d.Cron != want {
		t.Errorf("expected cron %q, got %q", want, d.Cron)
	}
}

func TestWakeCronExpressionRollsOverAcrossMidnight(t *testing.T) {
	now := time.Date(2026, 9, 2, 23, 0, 0, 0, time.Local)
	reset := time.Date(2026, 9, 2, 23, 59, 0, 0, time.Local)
	rl := freshLimit(95, reset.UTC().Format(time.RFC3339), now)

	d := decideWake(enabledCfg(), rl, nil, now)
	if !d.Inject {
		t.Fatal("expected injection")
	}
	want := "0 0 3 9 *" // 00:00 local on Sep 3
	if d.Cron != want {
		t.Errorf("expected cron %q, got %q", want, d.Cron)
	}
}

// The injected text must be a complete, executable directive: the hook has
// already done the checking, so Claude is given literal arguments.
func TestWakeInstructionCarriesLiteralArguments(t *testing.T) {
	now := time.Date(2026, 9, 2, 13, 0, 0, 0, time.Local)
	reset := time.Date(2026, 9, 2, 15, 30, 0, 0, time.Local)
	rl := freshLimit(95, reset.UTC().Format(time.RFC3339), now)

	d := decideWake(enabledCfg(), rl, nil, now)
	msg := wakeInstruction(d)

	for _, want := range []string{"[agent-handler wake]", "CronCreate", "31 15 2 9 *", wakeMarker} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected instruction to contain %q, got:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "CronList") {
		t.Error("instruction must not ask Claude to check for existing jobs itself")
	}
}

func TestIsWakeCron(t *testing.T) {
	if !isWakeCron(db.SessionCron{Prompt: wakeMarker + " resume"}) {
		t.Error("expected a marked prompt to be recognised as a wake job")
	}
	if isWakeCron(db.SessionCron{Prompt: "/inbox --auto"}) {
		t.Error("expected an unrelated job not to be recognised")
	}
	if isWakeCron(db.SessionCron{Prompt: ""}) {
		t.Error("expected an empty prompt not to be recognised")
	}
}
