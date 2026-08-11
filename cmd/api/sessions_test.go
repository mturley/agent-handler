package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mturley/agent-handler/db"
)

// TestHandleSessionResourcesCountsWatcherEvents verifies GET
// /api/sessions/{id}/resources' unread-count enrichment counts events linked
// via watcher_event_resources (github/jira events, which post-migration live
// only in watcher_events/watcher_event_resources), not only the legacy
// events/event_resources tables.
func TestHandleSessionResourcesCountsWatcherEvents(t *testing.T) {
	s := newTestServer(t)
	now := time.Now().UTC().Format(time.RFC3339)
	past := "2020-01-01T00:00:00Z"

	if err := s.DB.UpsertSession(db.Session{
		SessionID: "S1", Harness: "claude-code", Repo: "r", Branch: "main",
		Status: "active", LastActive: now, RegisteredAt: now, JSONLPath: "/tmp/s1.jsonl",
	}); err != nil {
		t.Fatalf("upsert session: %v", err)
	}
	if err := s.DB.Subscribe(db.Subscription{ID: "sub1", SessionID: "S1", ResourceType: "pr", ResourceID: "example/repo#1", CreatedAt: now}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	// Cursor set in the past so the event below counts as unread.
	if err := s.DB.AdvanceCursor("S1", past); err != nil {
		t.Fatalf("advance cursor: %v", err)
	}

	watcherEventID := "evt-watcher-unread"
	insertWatcherEvent(t, s, watcherEventID, now, "github", "pr_comment", "commented", "pr", "example/repo#1")

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/S1/resources", nil)
	req.SetPathValue("id", "S1")
	w := httptest.NewRecorder()
	s.handleSessionResources(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resources []sessionResourceInfo
	if err := json.Unmarshal(w.Body.Bytes(), &resources); err != nil {
		t.Fatalf("unmarshal response %q: %v", w.Body.String(), err)
	}

	if len(resources) != 1 {
		t.Fatalf("resources = %+v, want exactly 1", resources)
	}
	if resources[0].UnreadCount != 1 {
		t.Errorf("unread_count = %d, want 1 (watcher event should be counted)", resources[0].UnreadCount)
	}
	if resources[0].UnreadTypes["pr_comment"] != 1 {
		t.Errorf("unread_types = %+v, want pr_comment: 1", resources[0].UnreadTypes)
	}
}
