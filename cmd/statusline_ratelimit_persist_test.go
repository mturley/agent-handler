package cmd

import (
	"testing"
	"time"
)

func TestRecordRateLimitPersistsFiveHourWindow(t *testing.T) {
	d := cronTestDB(t)

	resets := time.Date(2026, 9, 2, 21, 0, 0, 0, time.UTC)
	in := &hookInput{SessionID: "sl-1"}
	in.RateLimits = &rateLimits{}
	in.RateLimits.FiveHour.UsedPercentage = 92.5
	in.RateLimits.FiveHour.ResetsAt = float64(resets.Unix())

	recordRateLimit(d, in)

	rl, err := d.GetRateLimit("sl-1")
	if err != nil {
		t.Fatalf("GetRateLimit failed: %v", err)
	}
	if rl == nil {
		t.Fatal("expected a persisted rate limit row")
	}
	if rl.FiveHourPercent != 92.5 {
		t.Errorf("expected 92.5, got %v", rl.FiveHourPercent)
	}
	got, err := time.Parse(time.RFC3339, rl.FiveHourResetsAt)
	if err != nil {
		t.Fatalf("resets_at %q is not RFC3339: %v", rl.FiveHourResetsAt, err)
	}
	if !got.Equal(resets) {
		t.Errorf("expected resets_at %v, got %v", resets, got)
	}
}

// Vertex sessions omit rate_limits entirely. Absence must leave no row at all —
// writing 0% would read as "plenty of budget" and, worse, a row that looks fresh.
func TestRecordRateLimitIgnoresSessionsWithoutLimits(t *testing.T) {
	d := cronTestDB(t)

	in := &hookInput{SessionID: "sl-2"}
	in.RateLimits = nil
	recordRateLimit(d, in)

	rl, err := d.GetRateLimit("sl-2")
	if err != nil {
		t.Fatalf("GetRateLimit failed: %v", err)
	}
	if rl != nil {
		t.Errorf("expected no row for a session without rate limits, got %+v", rl)
	}
}

func TestRecordRateLimitIgnoresMissingSessionID(t *testing.T) {
	d := cronTestDB(t)
	in := &hookInput{SessionID: ""}
	in.RateLimits = &rateLimits{}
	in.RateLimits.FiveHour.UsedPercentage = 50
	recordRateLimit(d, in) // must not panic or write
}

// A zero resets_at is not a real timestamp; store the percentage but leave the
// reset blank so decideWake refuses to build a cron expression from it.
func TestRecordRateLimitWithZeroResetsAtStoresBlankReset(t *testing.T) {
	d := cronTestDB(t)

	in := &hookInput{SessionID: "sl-3"}
	in.RateLimits = &rateLimits{}
	in.RateLimits.FiveHour.UsedPercentage = 77
	in.RateLimits.FiveHour.ResetsAt = 0

	recordRateLimit(d, in)

	rl, _ := d.GetRateLimit("sl-3")
	if rl == nil {
		t.Fatal("expected a row")
	}
	if rl.FiveHourResetsAt != "" {
		t.Errorf("expected blank resets_at for a zero epoch, got %q", rl.FiveHourResetsAt)
	}
}

func TestRecordRateLimitRefreshesUpdatedAt(t *testing.T) {
	d := cronTestDB(t)

	in := &hookInput{SessionID: "sl-4"}
	in.RateLimits = &rateLimits{}
	in.RateLimits.FiveHour.UsedPercentage = 42
	in.RateLimits.FiveHour.ResetsAt = float64(time.Now().Add(time.Hour).Unix())

	recordRateLimit(d, in)

	rl, _ := d.GetRateLimit("sl-4")
	if rl == nil {
		t.Fatal("expected a row")
	}
	if rl.IsStale(time.Now(), time.Minute) {
		t.Errorf("a just-written row must be fresh, updated_at=%q", rl.UpdatedAt)
	}
}

// --- fast-path marker -------------------------------------------------------
//
// The PostToolUse wake hook runs on every tool call. Spawning the Go binary
// each time is wasteful when nothing is near the limit, so the statusline
// maintains a marker file the shell wrapper can test with a single stat.

func TestWakeMarkerCreatedWhenOverThreshold(t *testing.T) {
	d := cronTestDB(t)
	in := &hookInput{SessionID: "mk-1"}
	in.RateLimits = &rateLimits{}
	in.RateLimits.FiveHour.UsedPercentage = 93
	in.RateLimits.FiveHour.ResetsAt = float64(time.Now().Add(time.Hour).Unix())

	recordRateLimit(d, in)

	if !wakeArmed("mk-1") {
		t.Error("expected the marker to exist when usage is over threshold")
	}
}

func TestWakeMarkerAbsentBelowThreshold(t *testing.T) {
	d := cronTestDB(t)
	in := &hookInput{SessionID: "mk-2"}
	in.RateLimits = &rateLimits{}
	in.RateLimits.FiveHour.UsedPercentage = 20
	in.RateLimits.FiveHour.ResetsAt = float64(time.Now().Add(time.Hour).Unix())

	recordRateLimit(d, in)

	if wakeArmed("mk-2") {
		t.Error("expected no marker below threshold")
	}
}

// Usage falling back below the threshold (a reset) must clear the marker, or
// the hot path stays armed forever.
func TestWakeMarkerClearedWhenUsageDrops(t *testing.T) {
	d := cronTestDB(t)
	in := &hookInput{SessionID: "mk-3"}
	in.RateLimits = &rateLimits{}
	in.RateLimits.FiveHour.ResetsAt = float64(time.Now().Add(time.Hour).Unix())

	in.RateLimits.FiveHour.UsedPercentage = 95
	recordRateLimit(d, in)
	if !wakeArmed("mk-3") {
		t.Fatal("expected marker to be armed first")
	}

	in.RateLimits.FiveHour.UsedPercentage = 5
	recordRateLimit(d, in)
	if wakeArmed("mk-3") {
		t.Error("expected the marker to be cleared once usage dropped")
	}
}

func TestWakeMarkerAbsentForSessionsWithoutLimits(t *testing.T) {
	d := cronTestDB(t)
	in := &hookInput{SessionID: "mk-4"}
	in.RateLimits = nil
	recordRateLimit(d, in)

	if wakeArmed("mk-4") {
		t.Error("expected no marker for a session that reports no limits")
	}
}
