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

// insertWatcherEvent seeds a row directly into the watcher library's
// watcher_events (+ optionally watcher_event_resources) tables, bypassing
// handler's own db.InsertEvent (which only writes the legacy `events` table).
// Post-migration, github/jira events live exclusively in these tables, so
// tests exercising the combined-read behavior must seed here.
func insertWatcherEvent(t *testing.T, s *Server, id, ts, source, evtType, title string, resourceType, resourceID string) {
	t.Helper()
	if _, err := s.DB.Exec(
		`INSERT INTO watcher_events (id, ts, source, type, title) VALUES (?, ?, ?, ?, ?)`,
		id, ts, source, evtType, title,
	); err != nil {
		t.Fatalf("insert watcher_events row %q: %v", id, err)
	}
	if resourceType != "" {
		if _, err := s.DB.Exec(
			`INSERT INTO watcher_event_resources (event_id, resource_type, resource_id) VALUES (?, ?, ?)`,
			id, resourceType, resourceID,
		); err != nil {
			t.Fatalf("insert watcher_event_resources row for %q: %v", id, err)
		}
	}
}

// TestHandleEventsIncludesWatcherEvents verifies GET /api/events returns
// events from BOTH the legacy `events` table (agent/handler/web events) and
// the watcher library's `watcher_events` table (github/jira events, which
// after `handler setup --migrate-watcher` live ONLY there — the legacy rows
// are purged). Before the fix, the query read only `events`, so the global
// timeline showed no PR/Jira activity post-migration.
func TestHandleEventsIncludesWatcherEvents(t *testing.T) {
	s := newTestServer(t)
	now := time.Now().UTC().Format(time.RFC3339)

	agentEventID := "evt-agent"
	if err := s.DB.InsertEvent(db.Event{
		ID: agentEventID, TS: now, Source: "handler", Type: "session_started", Title: "session started",
	}, nil, nil); err != nil {
		t.Fatalf("insert agent event: %v", err)
	}

	watcherEventID := "evt-watcher-gh"
	insertWatcherEvent(t, s, watcherEventID, now, "github", "pr_opened", "opened PR", "pr", "example/repo#42")

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
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
	if !ids[agentEventID] {
		t.Errorf("events = %v, want agent event %q present", resp.Events, agentEventID)
	}
	if !ids[watcherEventID] {
		t.Errorf("events = %v, want watcher event %q present", resp.Events, watcherEventID)
	}
}

// TestHandleEventsResourceFilterFindsWatcherEvents verifies the
// `?resource=type:id` filter matches events whose resource link lives in
// watcher_event_resources (github/jira events post-migration), not only the
// legacy event_resources table.
func TestHandleEventsResourceFilterFindsWatcherEvents(t *testing.T) {
	s := newTestServer(t)
	now := time.Now().UTC().Format(time.RFC3339)

	watcherEventID := "evt-watcher-jira"
	insertWatcherEvent(t, s, watcherEventID, now, "jira", "jira_comment", "commented", "jira", "PROJ-1")

	otherEventID := "evt-watcher-other"
	insertWatcherEvent(t, s, otherEventID, now, "jira", "jira_comment", "unrelated", "jira", "PROJ-2")

	req := httptest.NewRequest(http.MethodGet, "/api/events?resource=jira:PROJ-1", nil)
	w := httptest.NewRecorder()
	s.handleEvents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp eventsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response %q: %v", w.Body.String(), err)
	}

	if len(resp.Events) != 1 || resp.Events[0].ID != watcherEventID {
		t.Fatalf("events = %+v, want exactly [%q]", resp.Events, watcherEventID)
	}
}

// TestHandleEventsSessionFilterFindsWatcherEvents verifies the `?session=`
// filter's subscription-routed arm matches watcher events (resources linked
// via watcher_event_resources), not only legacy event_resources rows.
func TestHandleEventsSessionFilterFindsWatcherEvents(t *testing.T) {
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

	subscribedEventID := "evt-watcher-subscribed"
	insertWatcherEvent(t, s, subscribedEventID, now, "github", "pr_comment", "commented", "pr", "example/repo#1")

	unrelatedEventID := "evt-watcher-unrelated"
	insertWatcherEvent(t, s, unrelatedEventID, now, "github", "pr_comment", "other", "pr", "example/repo#2")

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
		t.Errorf("events = %v, want subscribed watcher event %q present", resp.Events, subscribedEventID)
	}
	if ids[unrelatedEventID] {
		t.Errorf("events = %v, want unrelated watcher event %q absent", resp.Events, unrelatedEventID)
	}
}

// TestHandleEventsResourcesIncludesWatcherEventResources verifies the
// per-event resource enrichment (fetchEventResources) finds resources linked
// via watcher_event_resources, not only the legacy event_resources table.
func TestHandleEventsResourcesIncludesWatcherEventResources(t *testing.T) {
	s := newTestServer(t)
	now := time.Now().UTC().Format(time.RFC3339)

	watcherEventID := "evt-watcher-res"
	insertWatcherEvent(t, s, watcherEventID, now, "github", "pr_opened", "opened", "pr", "example/repo#7")

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	w := httptest.NewRecorder()
	s.handleEvents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp eventsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response %q: %v", w.Body.String(), err)
	}

	var found *timelineEvent
	for i := range resp.Events {
		if resp.Events[i].ID == watcherEventID {
			found = &resp.Events[i]
		}
	}
	if found == nil {
		t.Fatalf("events = %+v, want watcher event %q present", resp.Events, watcherEventID)
	}
	if len(found.Resources) != 1 || found.Resources[0].ResourceType != "pr" || found.Resources[0].ResourceID != "example/repo#7" {
		t.Errorf("resources = %+v, want exactly [pr/example/repo#7]", found.Resources)
	}
}

func TestExtractResourceMetadata_Slack(t *testing.T) {
	stateJSON := `{"title":"Deploy question","channel_name":"eng-releases","author":"Ada","reply_count":4,"created_ts":"1787257539.775119"}`
	meta := extractResourceMetadata("slack", stateJSON)
	if meta == nil {
		t.Fatal("expected non-nil metadata for slack state")
	}
	for k, want := range map[string]string{"title": "Deploy question", "channel_name": "eng-releases", "author": "Ada"} {
		if meta[k] != want {
			t.Errorf("meta[%q] = %q, want %q", k, meta[k], want)
		}
	}
}
