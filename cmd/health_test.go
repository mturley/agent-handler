package cmd

import (
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/mturley/agent-handler/db"
)

// TestRunHealthSubscriptionCounts verifies that `handler health` reads its
// subscription counts from the watcher library's watcher_subscriptions
// table (post-Phase-2c) rather than the legacy subscriptions table: total
// counts every row (active + soft-deleted + lease-expired), while active
// counts only rows that are not soft-deleted and not lease-expired.
func TestRunHealthSubscriptionCounts(t *testing.T) {
	isolateHomes(t)

	d, err := db.Open(db.DefaultPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := d.Subscribe(db.Subscription{ID: "s1", SessionID: "S1", ResourceType: "pr", ResourceID: "1", CreatedAt: now}); err != nil {
		t.Fatalf("subscribe s1: %v", err)
	}
	if err := d.Subscribe(db.Subscription{ID: "s2", SessionID: "S1", ResourceType: "pr", ResourceID: "2", CreatedAt: now}); err != nil {
		t.Fatalf("subscribe s2: %v", err)
	}
	// Soft-deleted — counts toward total but not active.
	if err := d.Unsubscribe("S1", "pr", "2"); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	// Lease-expired (not soft-deleted) — counts toward total but not active.
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	if _, err := d.Conn().Exec(
		`INSERT INTO watcher_subscriptions (id, subscriber, resource_type, resource_id, created_at, expires_at, deleted_at) VALUES (?, ?, ?, ?, ?, ?, NULL)`,
		"s3", "handler:session:S1", "pr", "3", now, past,
	); err != nil {
		t.Fatalf("seed expired subscription: %v", err)
	}
	d.Close()

	origJSON := jsonOutput
	jsonOutput = true
	t.Cleanup(func() { jsonOutput = origJSON })

	stdout := captureStdout(t, func() {
		if err := runHealth(healthCmd, nil); err != nil {
			t.Fatalf("runHealth: %v", err)
		}
	})

	var parsed struct {
		TotalSubscriptions  int `json:"total_subscriptions"`
		ActiveSubscriptions int `json:"active_subscriptions"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("unmarshal health output %q: %v", stdout, err)
	}
	if parsed.TotalSubscriptions != 3 {
		t.Errorf("total_subscriptions = %d, want 3", parsed.TotalSubscriptions)
	}
	if parsed.ActiveSubscriptions != 1 {
		t.Errorf("active_subscriptions = %d, want 1", parsed.ActiveSubscriptions)
	}
}

// captureStdout runs fn with os.Stdout redirected, returning everything fn wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = orig
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return string(out)
}
