package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mturley/agent-handler/db"
)

// TestHandleResourcesReadsWatcherSubscriptions verifies GET /api/resources
// lists active subscriptions across non-archived sessions, reading from the
// watcher library's watcher_subscriptions table (post-Phase-2c) and
// recovering the session id from the "handler:session:<id>" subscriber
// string, instead of the legacy subscriptions table's session_id column.
func TestHandleResourcesReadsWatcherSubscriptions(t *testing.T) {
	s := newTestServer(t)
	now := time.Now().UTC().Format(time.RFC3339)

	if err := s.DB.UpsertSession(db.Session{
		SessionID: "S1", Harness: "claude-code", Repo: "r", Branch: "main",
		Status: "active", LastActive: now, RegisteredAt: now, JSONLPath: "/tmp/s1.jsonl",
	}); err != nil {
		t.Fatalf("upsert S1: %v", err)
	}
	if err := s.DB.UpsertSession(db.Session{
		SessionID: "S2", Harness: "claude-code", Repo: "r", Branch: "main",
		Status: "archived", LastActive: now, RegisteredAt: now, JSONLPath: "/tmp/s2.jsonl",
	}); err != nil {
		t.Fatalf("upsert S2: %v", err)
	}

	if err := s.DB.Subscribe(db.Subscription{ID: "sub1", SessionID: "S1", ResourceType: "pr", ResourceID: "example/repo#1", CreatedAt: now}); err != nil {
		t.Fatalf("subscribe S1: %v", err)
	}
	// Archived session's subscription must be excluded.
	if err := s.DB.Subscribe(db.Subscription{ID: "sub2", SessionID: "S2", ResourceType: "pr", ResourceID: "example/repo#2", CreatedAt: now}); err != nil {
		t.Fatalf("subscribe S2: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/resources", nil)
	w := httptest.NewRecorder()
	s.handleResources(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp resourcesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response %q: %v", w.Body.String(), err)
	}

	if len(resp.Resources) != 1 {
		t.Fatalf("resources = %+v, want exactly 1 (S1's, active session)", resp.Resources)
	}
	entry := resp.Resources[0]
	if entry.ResourceType != "pr" || entry.ResourceID != "example/repo#1" {
		t.Errorf("resource = %+v, want pr/example/repo#1", entry)
	}
	if len(entry.Sessions) != 1 || entry.Sessions[0].SessionID != "S1" {
		t.Errorf("sessions = %+v, want [S1]", entry.Sessions)
	}
}
