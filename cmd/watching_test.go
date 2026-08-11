package cmd

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mturley/agent-handler/db"
)

// TestRunWatchingGlobal verifies `handler watching --global` lists watched
// resources read from the watcher library's watcher_subscriptions table,
// scoped to active sessions, with the session id recovered from the
// "handler:session:<id>" subscriber string.
func TestRunWatchingGlobal(t *testing.T) {
	isolateHomes(t)

	d, err := db.Open(db.DefaultPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := d.UpsertSession(db.Session{
		SessionID: "S1", Harness: "claude-code", Repo: "r", Branch: "main",
		Status: "active", LastActive: now, RegisteredAt: now, JSONLPath: "/tmp/s1.jsonl",
	}); err != nil {
		t.Fatalf("upsert S1: %v", err)
	}
	// Archived session — its subscriptions must NOT appear in the global view.
	if err := d.UpsertSession(db.Session{
		SessionID: "S2", Harness: "claude-code", Repo: "r", Branch: "main",
		Status: "archived", LastActive: now, RegisteredAt: now, JSONLPath: "/tmp/s2.jsonl",
	}); err != nil {
		t.Fatalf("upsert S2: %v", err)
	}

	if err := d.Subscribe(db.Subscription{ID: "sub1", SessionID: "S1", ResourceType: "pr", ResourceID: "example/repo#1", CreatedAt: now}); err != nil {
		t.Fatalf("subscribe S1: %v", err)
	}
	if err := d.Subscribe(db.Subscription{ID: "sub2", SessionID: "S2", ResourceType: "pr", ResourceID: "example/repo#2", CreatedAt: now}); err != nil {
		t.Fatalf("subscribe S2: %v", err)
	}

	origJSON := jsonOutput
	jsonOutput = true
	t.Cleanup(func() { jsonOutput = origJSON })

	stdout := captureStdout(t, func() {
		if err := runWatchingGlobal(d); err != nil {
			t.Fatalf("runWatchingGlobal: %v", err)
		}
	})
	d.Close()

	var parsed struct {
		Subscriptions []struct {
			ResourceType string `json:"resource_type"`
			ResourceID   string `json:"resource_id"`
			Sessions     []struct {
				SessionID string `json:"session_id"`
			} `json:"sessions"`
		} `json:"subscriptions"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("unmarshal watching output %q: %v", stdout, err)
	}
	if len(parsed.Subscriptions) != 1 {
		t.Fatalf("subscriptions = %v, want exactly 1 (only S1's, active session)", parsed.Subscriptions)
	}
	sub := parsed.Subscriptions[0]
	if sub.ResourceType != "pr" || sub.ResourceID != "example/repo#1" {
		t.Errorf("subscription = %+v, want pr/example/repo#1", sub)
	}
	if len(sub.Sessions) != 1 || sub.Sessions[0].SessionID != "S1" {
		t.Errorf("sessions = %+v, want [S1]", sub.Sessions)
	}
}
