package db

import (
	"testing"
	"time"
)

func TestUpsertAndGetRateLimit(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "rl-1")

	now := time.Now().UTC().Format(time.RFC3339)
	resets := "2026-09-02T21:00:00Z"
	if err := d.UpsertRateLimit("rl-1", 92.5, resets, now); err != nil {
		t.Fatalf("UpsertRateLimit failed: %v", err)
	}

	rl, err := d.GetRateLimit("rl-1")
	if err != nil {
		t.Fatalf("GetRateLimit failed: %v", err)
	}
	if rl == nil {
		t.Fatal("expected a rate limit row")
	}
	if rl.FiveHourPercent != 92.5 {
		t.Errorf("expected 92.5, got %v", rl.FiveHourPercent)
	}
	if rl.FiveHourResetsAt != resets {
		t.Errorf("expected resets_at %q, got %q", resets, rl.FiveHourResetsAt)
	}
}

func TestGetRateLimitMissingReturnsNil(t *testing.T) {
	d := testDB(t)
	rl, err := d.GetRateLimit("never-seen")
	if err != nil {
		t.Fatalf("GetRateLimit failed: %v", err)
	}
	if rl != nil {
		t.Errorf("expected nil for unknown session, got %+v", rl)
	}
}

func TestUpsertRateLimitOverwrites(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "rl-2")

	if err := d.UpsertRateLimit("rl-2", 10, "2026-09-02T20:00:00Z", "2026-09-02T15:00:00Z"); err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}
	if err := d.UpsertRateLimit("rl-2", 91, "2026-09-02T21:00:00Z", "2026-09-02T16:00:00Z"); err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	rl, err := d.GetRateLimit("rl-2")
	if err != nil {
		t.Fatalf("GetRateLimit failed: %v", err)
	}
	if rl.FiveHourPercent != 91 || rl.FiveHourResetsAt != "2026-09-02T21:00:00Z" {
		t.Errorf("expected the newer values, got %+v", rl)
	}
}

// Staleness matters: the statusline is the only source of this data, so a row
// that stopped updating must not be trusted to trigger a wake job.
func TestRateLimitIsStaleWhenOld(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "rl-3")

	old := time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339)
	if err := d.UpsertRateLimit("rl-3", 95, "2026-09-02T21:00:00Z", old); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	rl, err := d.GetRateLimit("rl-3")
	if err != nil {
		t.Fatalf("GetRateLimit failed: %v", err)
	}
	if !rl.IsStale(time.Now(), 10*time.Minute) {
		t.Error("expected a 30-minute-old row to be stale against a 10-minute window")
	}
	if rl.IsStale(time.Now(), time.Hour) {
		t.Error("expected a 30-minute-old row to be fresh against a 1-hour window")
	}
}

func TestRateLimitFreshRowIsNotStale(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "rl-4")

	now := time.Now().UTC().Format(time.RFC3339)
	if err := d.UpsertRateLimit("rl-4", 95, "2026-09-02T21:00:00Z", now); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	rl, _ := d.GetRateLimit("rl-4")
	if rl.IsStale(time.Now(), 10*time.Minute) {
		t.Error("expected a just-written row to be fresh")
	}
}

// An unparseable updated_at must read as stale rather than as fresh — failing
// closed means a bad timestamp suppresses wake jobs instead of spamming them.
func TestRateLimitUnparseableTimestampIsStale(t *testing.T) {
	rl := &RateLimitState{UpdatedAt: "not a timestamp"}
	if !rl.IsStale(time.Now(), time.Hour) {
		t.Error("expected an unparseable updated_at to count as stale")
	}
}

func TestDeleteRateLimitsForSessions(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "rl-del-1")
	seedSession(t, d, "rl-keep")

	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range []string{"rl-del-1", "rl-keep"} {
		if err := d.UpsertRateLimit(id, 50, "2026-09-02T21:00:00Z", now); err != nil {
			t.Fatalf("upsert %s failed: %v", id, err)
		}
	}
	if err := d.DeleteRateLimitsForSessions([]string{"rl-del-1"}); err != nil {
		t.Fatalf("DeleteRateLimitsForSessions failed: %v", err)
	}

	if rl, _ := d.GetRateLimit("rl-del-1"); rl != nil {
		t.Error("expected targeted session's row to be deleted")
	}
	if rl, _ := d.GetRateLimit("rl-keep"); rl == nil {
		t.Error("expected untargeted session's row to survive")
	}
}

func TestDeleteRateLimitsForSessionsEmptyIsNoop(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "rl-noop")
	now := time.Now().UTC().Format(time.RFC3339)
	if err := d.UpsertRateLimit("rl-noop", 50, "2026-09-02T21:00:00Z", now); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if err := d.DeleteRateLimitsForSessions(nil); err != nil {
		t.Fatalf("expected nil list to be a no-op, got %v", err)
	}
	if rl, _ := d.GetRateLimit("rl-noop"); rl == nil {
		t.Error("empty delete list must not remove anything")
	}
}

func TestRecordAndReadRateLimitError(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "rle-1")

	now := time.Now().UTC()
	if err := d.RecordRateLimitError("rle-1", now.Format(time.RFC3339)); err != nil {
		t.Fatalf("RecordRateLimitError failed: %v", err)
	}

	if !d.HadRecentRateLimitError("rle-1", now, time.Minute) {
		t.Error("expected a just-recorded error to count as recent")
	}
	if d.HadRecentRateLimitError("rle-1", now.Add(10*time.Minute), time.Minute) {
		t.Error("expected the error to age out of a 1-minute window")
	}
}

// The error must be recordable even when no statusline row exists yet — a
// session can hit a rate limit before the statusline has written anything.
func TestRecordRateLimitErrorWithoutExistingRow(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "rle-2")

	now := time.Now().UTC()
	if err := d.RecordRateLimitError("rle-2", now.Format(time.RFC3339)); err != nil {
		t.Fatalf("RecordRateLimitError failed: %v", err)
	}
	if !d.HadRecentRateLimitError("rle-2", now, time.Minute) {
		t.Error("expected the error to be recorded without a prior rate limit row")
	}
}

// Recording an error must not clobber a real usage reading.
func TestRecordRateLimitErrorPreservesUsage(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "rle-3")

	now := time.Now().UTC()
	if err := d.UpsertRateLimit("rle-3", 97, "2026-09-02T21:00:00Z", now.Format(time.RFC3339)); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if err := d.RecordRateLimitError("rle-3", now.Format(time.RFC3339)); err != nil {
		t.Fatalf("RecordRateLimitError failed: %v", err)
	}

	rl, _ := d.GetRateLimit("rle-3")
	if rl == nil || rl.FiveHourPercent != 97 {
		t.Errorf("expected usage preserved, got %+v", rl)
	}
}

func TestHadRecentRateLimitErrorFalseWhenNoneRecorded(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "rle-4")
	if d.HadRecentRateLimitError("rle-4", time.Now().UTC(), time.Minute) {
		t.Error("expected false when no error was ever recorded")
	}
}
