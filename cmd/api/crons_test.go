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
