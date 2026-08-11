package api

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mturley/agent-handler/db"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return &Server{DB: d, Logger: log.New(os.Stderr, "", 0)}
}

// TestHandleEventsSessionFilterUsesWatcherSubscriptions verifies the
// `?session=` filter on GET /api/events finds events whose resource is
// subscribed to by that session, reading from the watcher library's
// watcher_subscriptions table (post-Phase-2c) instead of the legacy
// subscriptions table.
func TestHandleEventsSessionFilterUsesWatcherSubscriptions(t *testing.T) {
	s := newTestServer(t)
	now := time.Now().UTC().Format(time.RFC3339)

	if err := s.DB.UpsertSession(db.Session{
		SessionID: "S1", Harness: "claude-code", Repo: "r", Branch: "main",
		Status: "active", LastActive: now, RegisteredAt: now, JSONLPath: "/tmp/s1.jsonl",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}
	if err := s.DB.Subscribe(db.Subscription{ID: "sub1", SessionID: "S1", ResourceType: "pr", ResourceID: "example/repo#1", CreatedAt: now}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Event on the subscribed resource, not authored by S1 directly.
	subscribedEventID := "evt-subscribed"
	if err := s.DB.InsertEvent(db.Event{
		ID: subscribedEventID, TS: now, Source: "github", Type: "pr_comment", Title: "commented",
	}, nil, []db.EventResource{{ResourceType: "pr", ResourceID: "example/repo#1"}}); err != nil {
		t.Fatalf("insert subscribed event: %v", err)
	}
	// Event on an unrelated resource S1 doesn't subscribe to.
	unrelatedEventID := "evt-unrelated"
	if err := s.DB.InsertEvent(db.Event{
		ID: unrelatedEventID, TS: now, Source: "github", Type: "pr_comment", Title: "other",
	}, nil, []db.EventResource{{ResourceType: "pr", ResourceID: "example/repo#2"}}); err != nil {
		t.Fatalf("insert unrelated event: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/events?session=S1", nil)
	w := httptest.NewRecorder()
	s.handleEvents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp eventsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response %q: %v", w.Body.String(), err)
	}

	ids := map[string]bool{}
	for _, e := range resp.Events {
		ids[e.ID] = true
	}
	if !ids[subscribedEventID] {
		t.Errorf("events = %v, want subscribed event %q present", resp.Events, subscribedEventID)
	}
	if ids[unrelatedEventID] {
		t.Errorf("events = %v, want unrelated event %q absent", resp.Events, unrelatedEventID)
	}
}
