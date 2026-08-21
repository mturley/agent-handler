package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/worktreeinterop"
)

// fakeExecJSON returns an execCommand-compatible func whose resulting
// *exec.Cmd, when run, prints the given JSON to stdout and exits 0.
func fakeExecJSON(stdout string) func(string, ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", name}
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			"HELPER_STDOUT="+stdout,
			"HELPER_EXIT=0",
		)
		return cmd
	}
}

func lookPathFound(string) (string, error) {
	return "/usr/local/bin/worktree", nil
}

func lookPathNotFound(string) (string, error) {
	return "", exec.ErrNotFound
}

func TestAutoSubscribe_SubscribesPrimaries(t *testing.T) {
	isolateHomes(t)

	restore := worktreeinterop.SetSeamsForTest(
		fakeExecJSON(`[{"type":"pr","id":"o/r#1","url":"u1","primary":true}]`),
		lookPathFound,
	)
	defer restore()

	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	sid := "sess-1"
	now := time.Now().UTC().Format(time.RFC3339)
	autoSubscribeWorktreePrimaries(d, sid, "/some/dir", now)

	subs, err := d.ListSubscriptions(sid, false)
	if err != nil {
		t.Fatalf("ListSubscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d: %+v", len(subs), subs)
	}
	if subs[0].ResourceType != "pr" || subs[0].ResourceID != "o/r#1" {
		t.Errorf("unexpected subscription: %+v", subs[0])
	}
	if subs[0].ResourceURL == nil || *subs[0].ResourceURL != "u1" {
		t.Errorf("expected ResourceURL u1, got %+v", subs[0].ResourceURL)
	}
}

func TestAutoSubscribe_RespectsTombstone(t *testing.T) {
	isolateHomes(t)

	restore := worktreeinterop.SetSeamsForTest(
		fakeExecJSON(`[{"type":"pr","id":"o/r#1","url":"u1","primary":true}]`),
		lookPathFound,
	)
	defer restore()

	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	sid := "sess-2"
	now := time.Now().UTC().Format(time.RFC3339)

	if err := d.Subscribe(db.Subscription{
		ID:           "sub-1",
		SessionID:    sid,
		ResourceType: "pr",
		ResourceID:   "o/r#1",
		CreatedAt:    now,
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := d.Unsubscribe(sid, "pr", "o/r#1"); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	autoSubscribeWorktreePrimaries(d, sid, "/some/dir", now)

	subs, err := d.ListSubscriptions(sid, false)
	if err != nil {
		t.Fatalf("ListSubscriptions: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("expected tombstone to be respected (0 active subs), got %d: %+v", len(subs), subs)
	}
}

func TestAutoSubscribe_NotAvailable(t *testing.T) {
	isolateHomes(t)

	restore := worktreeinterop.SetSeamsForTest(nil, lookPathNotFound)
	defer restore()

	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	sid := "sess-3"
	now := time.Now().UTC().Format(time.RFC3339)

	autoSubscribeWorktreePrimaries(d, sid, "/some/dir", now)

	subs, err := d.ListSubscriptions(sid, false)
	if err != nil {
		t.Fatalf("ListSubscriptions: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("expected 0 subs when worktree CLI unavailable, got %d", len(subs))
	}
}

// TestHelperProcess is not a real test — it's the fake `worktree` subprocess
// used by fakeExecJSON above.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	os.Stdout.WriteString(os.Getenv("HELPER_STDOUT"))
	os.Exit(0)
}
