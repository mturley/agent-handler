package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mturley/agent-handler/db"
)

func seedCronSession(t *testing.T, s *Server, id string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.DB.UpsertSession(db.Session{
		SessionID: id, Harness: "claude-code", Repo: "r", Branch: "main",
		Status: "active", LastActive: now, RegisteredAt: now, JSONLPath: "/tmp/x.jsonl",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}
}

func getSessionCrons(t *testing.T, s *Server, id string) []db.SessionCron {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+id+"/crons", nil)
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.handleSessionCrons(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var crons []db.SessionCron
	if err := json.Unmarshal(w.Body.Bytes(), &crons); err != nil {
		t.Fatalf("unmarshal response %q: %v", w.Body.String(), err)
	}
	return crons
}

func TestHandleSessionCronsReturnsJobs(t *testing.T) {
	s := newTestServer(t)
	seedCronSession(t, s, "S1")
	now := time.Now().UTC().Format(time.RFC3339)

	if err := s.DB.UpsertSessionCron("S1", db.SessionCron{
		JobID: "abc123", Schedule: "0 4 * * *", Recurring: true, Prompt: "control job",
	}, now); err != nil {
		t.Fatalf("upsert cron: %v", err)
	}

	crons := getSessionCrons(t, s, "S1")
	if len(crons) != 1 {
		t.Fatalf("crons = %+v, want exactly 1", crons)
	}
	c := crons[0]
	if c.JobID != "abc123" || c.Schedule != "0 4 * * *" || !c.Recurring || c.Prompt != "control job" {
		t.Errorf("unexpected cron payload: %+v", c)
	}
	if c.CreatedAt == "" || c.LastSeen == "" {
		t.Errorf("expected timestamps to be populated, got created_at=%q last_seen_at=%q", c.CreatedAt, c.LastSeen)
	}
}

func TestHandleSessionCronsIsScopedToSession(t *testing.T) {
	s := newTestServer(t)
	seedCronSession(t, s, "S1")
	seedCronSession(t, s, "S2")
	now := time.Now().UTC().Format(time.RFC3339)

	if err := s.DB.UpsertSessionCron("S1", db.SessionCron{JobID: "mine", Schedule: "* * * * *"}, now); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s.DB.UpsertSessionCron("S2", db.SessionCron{JobID: "theirs", Schedule: "* * * * *"}, now); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	crons := getSessionCrons(t, s, "S1")
	if len(crons) != 1 || crons[0].JobID != "mine" {
		t.Fatalf("expected only S1's job, got %+v", crons)
	}
}

// The UI renders this directly, so an empty result must be [] and not null —
// otherwise `.length` on the client blows up.
func TestHandleSessionCronsReturnsEmptyArrayNotNull(t *testing.T) {
	s := newTestServer(t)
	seedCronSession(t, s, "S1")

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/S1/crons", nil)
	req.SetPathValue("id", "S1")
	w := httptest.NewRecorder()
	s.handleSessionCrons(w, req)

	if got := w.Body.String(); got != "[]\n" && got != "[]" {
		t.Errorf("expected empty JSON array, got %q", got)
	}
}

func TestHandleSessionCronsRequiresSessionID(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions//crons", nil)
	req.SetPathValue("id", "")
	w := httptest.NewRecorder()
	s.handleSessionCrons(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing session id, got %d", w.Code)
	}
}

func TestHandleSessionCronsOrdersByCreatedAt(t *testing.T) {
	s := newTestServer(t)
	seedCronSession(t, s, "S1")

	if err := s.DB.UpsertSessionCron("S1", db.SessionCron{JobID: "second", Schedule: "* * * * *"}, "2026-08-31T02:00:00Z"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s.DB.UpsertSessionCron("S1", db.SessionCron{JobID: "first", Schedule: "* * * * *"}, "2026-08-31T01:00:00Z"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	crons := getSessionCrons(t, s, "S1")
	if len(crons) != 2 {
		t.Fatalf("expected 2 crons, got %d", len(crons))
	}
	if crons[0].JobID != "first" || crons[1].JobID != "second" {
		t.Errorf("expected creation order, got %q then %q", crons[0].JobID, crons[1].JobID)
	}
}

// --- next fire time ---------------------------------------------------------

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("bad test time %q: %v", s, err)
	}
	return ts
}

func TestNextFireAtRecurringDaily(t *testing.T) {
	from := mustParseTime(t, "2026-08-31T13:20:00Z")
	got := nextFireAt("37 4 * * *", from)
	want := "2026-09-01T04:37:00Z"
	if got != want {
		t.Errorf("nextFireAt = %q, want %q", got, want)
	}
}

func TestNextFireAtRecurringLaterToday(t *testing.T) {
	from := mustParseTime(t, "2026-08-31T13:20:00Z")
	got := nextFireAt("32 13 * * *", from)
	want := "2026-08-31T13:32:00Z"
	if got != want {
		t.Errorf("nextFireAt = %q, want %q", got, want)
	}
}

func TestNextFireAtStepExpression(t *testing.T) {
	from := mustParseTime(t, "2026-08-31T13:20:30Z")
	got := nextFireAt("*/5 * * * *", from)
	want := "2026-08-31T13:25:00Z"
	if got != want {
		t.Errorf("nextFireAt = %q, want %q", got, want)
	}
}

func TestNextFireAtPinnedOneShot(t *testing.T) {
	from := mustParseTime(t, "2026-08-31T13:20:00Z")
	got := nextFireAt("53 3 28 12 *", from)
	want := "2026-12-28T03:53:00Z"
	if got != want {
		t.Errorf("nextFireAt = %q, want %q", got, want)
	}
}

// A pinned one-shot whose time has passed has no next occurrence this year.
// robfig rolls to the next matching year; the point is it must not report a
// time in the past.
func TestNextFireAtNeverReturnsPastTime(t *testing.T) {
	from := mustParseTime(t, "2026-12-29T00:00:00Z")
	got := nextFireAt("53 3 28 12 *", from)
	if got == "" {
		t.Fatal("expected a next occurrence, got empty")
	}
	next := mustParseTime(t, got)
	if !next.After(from) {
		t.Errorf("next fire %q is not after %q", got, from.Format(time.RFC3339))
	}
}

func TestNextFireAtInvalidExpressionIsEmpty(t *testing.T) {
	from := mustParseTime(t, "2026-08-31T13:20:00Z")
	for _, expr := range []string{"", "not a cron", "* * *", "99 99 * * *"} {
		if got := nextFireAt(expr, from); got != "" {
			t.Errorf("nextFireAt(%q) = %q, want empty", expr, got)
		}
	}
}

// The handler must enrich each job with next_fire_at without dropping jobs
// whose expression cannot be parsed.
func TestHandleSessionCronsIncludesNextFireAt(t *testing.T) {
	s := newTestServer(t)
	seedCronSession(t, s, "S1")
	now := time.Now().UTC().Format(time.RFC3339)

	if err := s.DB.UpsertSessionCron("S1", db.SessionCron{
		JobID: "good", Schedule: "*/5 * * * *", Recurring: true,
	}, now); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s.DB.UpsertSessionCron("S1", db.SessionCron{
		JobID: "bad", Schedule: "not a cron", Recurring: false,
	}, now); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/S1/crons", nil)
	req.SetPathValue("id", "S1")
	w := httptest.NewRecorder()
	s.handleSessionCrons(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var crons []sessionCronInfo
	if err := json.Unmarshal(w.Body.Bytes(), &crons); err != nil {
		t.Fatalf("unmarshal %q: %v", w.Body.String(), err)
	}
	if len(crons) != 2 {
		t.Fatalf("expected both jobs listed, got %d", len(crons))
	}

	byID := map[string]sessionCronInfo{}
	for _, c := range crons {
		byID[c.JobID] = c
	}
	if byID["good"].NextFireAt == "" {
		t.Error("expected next_fire_at for a valid expression")
	}
	if byID["bad"].NextFireAt != "" {
		t.Errorf("expected empty next_fire_at for an unparseable expression, got %q", byID["bad"].NextFireAt)
	}
	if byID["good"].Schedule != "*/5 * * * *" {
		t.Errorf("expected embedded cron fields preserved, got %+v", byID["good"])
	}
}

// --- SSE change detection ---------------------------------------------------
//
// The stream emits crons_changed only when the tracked cron set actually
// differs, so an idle page does not refetch every heartbeat.

func TestCronsFingerprintChangesWhenJobAdded(t *testing.T) {
	s := newTestServer(t)
	seedCronSession(t, s, "S1")
	now := time.Now().UTC().Format(time.RFC3339)

	before := cronsFingerprint(s.DB)
	if err := s.DB.UpsertSessionCron("S1", db.SessionCron{JobID: "j1", Schedule: "* * * * *"}, now); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	after := cronsFingerprint(s.DB)

	if before == after {
		t.Errorf("expected the fingerprint to change when a job is added (both %q)", before)
	}
}

func TestCronsFingerprintChangesWhenJobRemoved(t *testing.T) {
	s := newTestServer(t)
	seedCronSession(t, s, "S1")
	now := time.Now().UTC().Format(time.RFC3339)

	if err := s.DB.UpsertSessionCron("S1", db.SessionCron{JobID: "j1", Schedule: "* * * * *"}, now); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	before := cronsFingerprint(s.DB)
	if err := s.DB.DeleteSessionCron("S1", "j1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	after := cronsFingerprint(s.DB)

	if before == after {
		t.Errorf("expected the fingerprint to change when a job is removed (both %q)", before)
	}
}

// A schedule edit must be visible too — reconciliation can rewrite fields in
// place without changing the job count.
func TestCronsFingerprintChangesWhenScheduleChanges(t *testing.T) {
	s := newTestServer(t)
	seedCronSession(t, s, "S1")
	now := time.Now().UTC().Format(time.RFC3339)

	if err := s.DB.UpsertSessionCron("S1", db.SessionCron{JobID: "j1", Schedule: "1 2 3 4 *"}, now); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	before := cronsFingerprint(s.DB)
	if err := s.DB.UpsertSessionCron("S1", db.SessionCron{JobID: "j1", Schedule: "5 6 7 8 *"}, now); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	after := cronsFingerprint(s.DB)

	if before == after {
		t.Errorf("expected the fingerprint to change when a schedule changes (both %q)", before)
	}
}

// Steady state must be stable, or the page would refetch on every heartbeat.
func TestCronsFingerprintStableWhenNothingChanges(t *testing.T) {
	s := newTestServer(t)
	seedCronSession(t, s, "S1")
	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.DB.UpsertSessionCron("S1", db.SessionCron{JobID: "j1", Schedule: "* * * * *"}, now); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if a, b := cronsFingerprint(s.DB), cronsFingerprint(s.DB); a != b {
		t.Errorf("expected a stable fingerprint, got %q then %q", a, b)
	}
}

func TestCronsFingerprintEmptyDatabase(t *testing.T) {
	s := newTestServer(t)
	if got := cronsFingerprint(s.DB); got != "" {
		t.Errorf("expected an empty fingerprint with no jobs, got %q", got)
	}
}

// --- resources SSE change detection -----------------------------------------
//
// The Resources tab had the same gap as Cron jobs: ["session-resources", id]
// was invalidated only by its own subscribe/unsubscribe mutations, so a
// resource added by a watcher never appeared until remount.

func TestResourcesFingerprintChangesOnSubscribe(t *testing.T) {
	s := newTestServer(t)
	seedCronSession(t, s, "S1")
	now := time.Now().UTC().Format(time.RFC3339)

	before := resourcesFingerprint(s.DB)
	if err := s.DB.Subscribe(db.Subscription{
		ID: "sub1", SessionID: "S1", ResourceType: "pr", ResourceID: "example/repo#1", CreatedAt: now,
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if after := resourcesFingerprint(s.DB); after == before {
		t.Errorf("expected the fingerprint to change on subscribe (both %q)", before)
	}
}

func TestResourcesFingerprintChangesOnUnsubscribe(t *testing.T) {
	s := newTestServer(t)
	seedCronSession(t, s, "S1")
	now := time.Now().UTC().Format(time.RFC3339)

	if err := s.DB.Subscribe(db.Subscription{
		ID: "sub1", SessionID: "S1", ResourceType: "pr", ResourceID: "example/repo#1", CreatedAt: now,
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	before := resourcesFingerprint(s.DB)
	if _, err := s.DB.SoftDeleteSubscriptionsForSession("S1"); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	if after := resourcesFingerprint(s.DB); after == before {
		t.Errorf("expected the fingerprint to change on unsubscribe (both %q)", before)
	}
}

func TestResourcesFingerprintStableWhenNothingChanges(t *testing.T) {
	s := newTestServer(t)
	seedCronSession(t, s, "S1")
	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.DB.Subscribe(db.Subscription{
		ID: "sub1", SessionID: "S1", ResourceType: "pr", ResourceID: "example/repo#1", CreatedAt: now,
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if a, b := resourcesFingerprint(s.DB), resourcesFingerprint(s.DB); a != b {
		t.Errorf("expected a stable fingerprint, got %q then %q", a, b)
	}
}

func TestResourcesFingerprintEmptyDatabase(t *testing.T) {
	s := newTestServer(t)
	if got := resourcesFingerprint(s.DB); got != "" {
		t.Errorf("expected an empty fingerprint with no subscriptions, got %q", got)
	}
}
