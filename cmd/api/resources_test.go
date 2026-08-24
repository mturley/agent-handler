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
	// With no custom name and no cached title, display_title falls back to id.
	if entry.DisplayTitle != "example/repo#1" {
		t.Errorf("display_title = %q, want the resource id fallback", entry.DisplayTitle)
	}
	// The Slack watcher must appear in the status map (Phase 6 made it
	// first-class; Phase 7 wired it into this endpoint via KnownWatchers).
	if _, ok := resp.Watchers["slack"]; !ok {
		t.Errorf("watchers map missing slack: %+v", resp.Watchers)
	}
}

func TestResolveDisplayTitle(t *testing.T) {
	cases := []struct {
		name   string
		custom string
		state  map[string]interface{}
		id     string
		want   string
	}{
		{"custom name wins", "My Name", map[string]interface{}{"title": "Cached"}, "id", "My Name"},
		{"cached title when no custom", "", map[string]interface{}{"title": "Cached"}, "id", "Cached"},
		{"id when neither", "", nil, "slack:C1:1.2", "slack:C1:1.2"},
		{"id when title empty", "", map[string]interface{}{"title": ""}, "id", "id"},
		{"id when title not a string", "", map[string]interface{}{"title": 42}, "id", "id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveDisplayTitle(c.custom, c.state, c.id); got != c.want {
				t.Errorf("resolveDisplayTitle(%q, %v, %q) = %q, want %q", c.custom, c.state, c.id, got, c.want)
			}
		})
	}
}
