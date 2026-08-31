package db

import (
	"testing"
	"time"
)

func nowTS() string { return time.Now().UTC().Format(time.RFC3339) }

func TestUpsertAndListSessionCrons(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "cron-upsert")

	if err := d.UpsertSessionCron("cron-upsert", SessionCron{
		JobID:     "abc123",
		Schedule:  "0 4 * * *",
		Recurring: true,
		Prompt:    "control job",
	}, nowTS()); err != nil {
		t.Fatalf("UpsertSessionCron failed: %v", err)
	}

	got, err := d.ListSessionCrons("cron-upsert")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 cron, got %d", len(got))
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

func TestUpsertSessionCronIsIdempotent(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "cron-idem")

	c := SessionCron{JobID: "dup", Schedule: "*/5 * * * *", Recurring: true, Prompt: "first"}
	if err := d.UpsertSessionCron("cron-idem", c, nowTS()); err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}
	c.Prompt = "second"
	if err := d.UpsertSessionCron("cron-idem", c, nowTS()); err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	got, err := d.ListSessionCrons("cron-idem")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 cron after duplicate upsert, got %d", len(got))
	}
	if got[0].Prompt != "second" {
		t.Errorf("expected prompt updated to %q, got %q", "second", got[0].Prompt)
	}
}

func TestSessionCronsAreScopedPerSession(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "cron-a")
	seedSession(t, d, "cron-b")

	if err := d.UpsertSessionCron("cron-a", SessionCron{JobID: "job-a", Schedule: "* * * * *"}, nowTS()); err != nil {
		t.Fatalf("upsert a failed: %v", err)
	}
	if err := d.UpsertSessionCron("cron-b", SessionCron{JobID: "job-b", Schedule: "* * * * *"}, nowTS()); err != nil {
		t.Fatalf("upsert b failed: %v", err)
	}

	got, err := d.ListSessionCrons("cron-a")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 1 || got[0].JobID != "job-a" {
		t.Fatalf("expected only job-a for session cron-a, got %+v", got)
	}
}

func TestDeleteSessionCron(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "cron-del")

	if err := d.UpsertSessionCron("cron-del", SessionCron{JobID: "gone", Schedule: "* * * * *"}, nowTS()); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if err := d.DeleteSessionCron("cron-del", "gone"); err != nil {
		t.Fatalf("DeleteSessionCron failed: %v", err)
	}

	got, err := d.ListSessionCrons("cron-del")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 crons after delete, got %d", len(got))
	}
}

func TestDeleteSessionCronMissingIsNotAnError(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "cron-del-missing")

	if err := d.DeleteSessionCron("cron-del-missing", "never-existed"); err != nil {
		t.Fatalf("deleting a missing cron should not error, got: %v", err)
	}
}

// SyncSessionCrons is the Stop-hook reconciliation: the snapshot is
// authoritative, so it must drop rows the snapshot omits (jobs that fired and
// auto-deleted) and insert rows it contains but the DB lacks (a missed
// CronCreate hook).
func TestSyncSessionCronsRemovesFiredJobs(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "cron-sync-remove")

	if err := d.UpsertSessionCron("cron-sync-remove", SessionCron{JobID: "control", Schedule: "0 4 * * *", Recurring: true}, nowTS()); err != nil {
		t.Fatalf("upsert control failed: %v", err)
	}
	if err := d.UpsertSessionCron("cron-sync-remove", SessionCron{JobID: "oneshot", Schedule: "45 11 31 8 *"}, nowTS()); err != nil {
		t.Fatalf("upsert oneshot failed: %v", err)
	}

	// Snapshot after the one-shot fired: only the control remains.
	snapshot := []SessionCron{{JobID: "control", Schedule: "0 4 * * *", Recurring: true}}
	if err := d.SyncSessionCrons("cron-sync-remove", snapshot, nowTS()); err != nil {
		t.Fatalf("SyncSessionCrons failed: %v", err)
	}

	got, err := d.ListSessionCrons("cron-sync-remove")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 cron after sync, got %d: %+v", len(got), got)
	}
	if got[0].JobID != "control" {
		t.Errorf("expected surviving job %q, got %q", "control", got[0].JobID)
	}
}

func TestSyncSessionCronsAddsMissingJobs(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "cron-sync-add")

	snapshot := []SessionCron{
		{JobID: "missed", Schedule: "*/5 * * * *", Recurring: true, Prompt: "hook was missed"},
	}
	if err := d.SyncSessionCrons("cron-sync-add", snapshot, nowTS()); err != nil {
		t.Fatalf("SyncSessionCrons failed: %v", err)
	}

	got, err := d.ListSessionCrons("cron-sync-add")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 1 || got[0].JobID != "missed" {
		t.Fatalf("expected sync to insert the missing job, got %+v", got)
	}
	if got[0].Prompt != "hook was missed" {
		t.Errorf("expected prompt %q, got %q", "hook was missed", got[0].Prompt)
	}
}

func TestSyncSessionCronsWithEmptySnapshotClearsSession(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "cron-sync-empty")

	if err := d.UpsertSessionCron("cron-sync-empty", SessionCron{JobID: "stale", Schedule: "* * * * *"}, nowTS()); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if err := d.SyncSessionCrons("cron-sync-empty", []SessionCron{}, nowTS()); err != nil {
		t.Fatalf("SyncSessionCrons failed: %v", err)
	}

	got, err := d.ListSessionCrons("cron-sync-empty")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty snapshot to clear the session, got %d rows", len(got))
	}
}

func TestSyncSessionCronsDoesNotTouchOtherSessions(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "cron-sync-mine")
	seedSession(t, d, "cron-sync-theirs")

	if err := d.UpsertSessionCron("cron-sync-theirs", SessionCron{JobID: "theirs", Schedule: "* * * * *"}, nowTS()); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if err := d.SyncSessionCrons("cron-sync-mine", []SessionCron{}, nowTS()); err != nil {
		t.Fatalf("SyncSessionCrons failed: %v", err)
	}

	got, err := d.ListSessionCrons("cron-sync-theirs")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("sync on one session must not clear another; got %d rows", len(got))
	}
}

func TestSyncSessionCronsUpdatesChangedFields(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "cron-sync-update")

	if err := d.UpsertSessionCron("cron-sync-update", SessionCron{JobID: "j1", Schedule: "0 4 * * *", Recurring: true, Prompt: "old"}, nowTS()); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	snapshot := []SessionCron{{JobID: "j1", Schedule: "0 5 * * *", Recurring: true, Prompt: "new"}}
	if err := d.SyncSessionCrons("cron-sync-update", snapshot, nowTS()); err != nil {
		t.Fatalf("SyncSessionCrons failed: %v", err)
	}

	got, err := d.ListSessionCrons("cron-sync-update")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 cron, got %d", len(got))
	}
	if got[0].Schedule != "0 5 * * *" || got[0].Prompt != "new" {
		t.Errorf("expected snapshot fields to win, got schedule=%q prompt=%q", got[0].Schedule, got[0].Prompt)
	}
}

func TestDeleteSessionCronsForSessions(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "cron-bulk-1")
	seedSession(t, d, "cron-bulk-2")
	seedSession(t, d, "cron-bulk-keep")

	for _, id := range []string{"cron-bulk-1", "cron-bulk-2", "cron-bulk-keep"} {
		if err := d.UpsertSessionCron(id, SessionCron{JobID: "j", Schedule: "* * * * *"}, nowTS()); err != nil {
			t.Fatalf("upsert for %s failed: %v", id, err)
		}
	}

	if err := d.DeleteSessionCronsForSessions([]string{"cron-bulk-1", "cron-bulk-2"}); err != nil {
		t.Fatalf("DeleteSessionCronsForSessions failed: %v", err)
	}

	for _, id := range []string{"cron-bulk-1", "cron-bulk-2"} {
		got, err := d.ListSessionCrons(id)
		if err != nil {
			t.Fatalf("ListSessionCrons(%s) failed: %v", id, err)
		}
		if len(got) != 0 {
			t.Errorf("expected %s to be cleared, got %d rows", id, len(got))
		}
	}
	kept, err := d.ListSessionCrons("cron-bulk-keep")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(kept) != 1 {
		t.Errorf("expected untargeted session to keep its cron, got %d rows", len(kept))
	}
}

func TestDeleteSessionCronsForSessionsWithEmptyListIsNoop(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "cron-bulk-noop")

	if err := d.UpsertSessionCron("cron-bulk-noop", SessionCron{JobID: "j", Schedule: "* * * * *"}, nowTS()); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if err := d.DeleteSessionCronsForSessions(nil); err != nil {
		t.Fatalf("expected nil list to be a no-op, got: %v", err)
	}

	got, err := d.ListSessionCrons("cron-bulk-noop")
	if err != nil {
		t.Fatalf("ListSessionCrons failed: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("empty delete list must not clear anything, got %d rows", len(got))
	}
}
