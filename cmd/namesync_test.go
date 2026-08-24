package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/worktreeinterop"
	"github.com/mturley/watcher"
	wdb "github.com/mturley/watcher/db"
)

func nsTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func seedHandlerMeta(t *testing.T, d *db.DB, typ, id, name, desc, ts string) {
	t.Helper()
	if err := wdb.SetResourceMetaAt(d.Conn(), watcher.Resource{Type: typ, ID: id}, name, desc, ts); err != nil {
		t.Fatalf("seed handler meta: %v", err)
	}
}

func getHandlerMeta(t *testing.T, d *db.DB, typ, id string) *wdb.ResourceMeta {
	t.Helper()
	m, err := wdb.GetResourceMeta(d.Conn(), typ, id)
	if err != nil {
		t.Fatalf("get handler meta: %v", err)
	}
	return m
}

// captureWorktreeSetName fakes worktreeinterop's exec seam, records the argv of
// any `worktree` call, and returns a pointer to the recorded argv slice.
func captureWorktreeSetName(t *testing.T) *[]string {
	t.Helper()
	got := &[]string{}
	restore := worktreeinterop.SetSeamsForTest(
		func(name string, args ...string) *exec.Cmd {
			*got = append([]string{name}, args...)
			c := exec.Command(os.Args[0], "-test.run=TestNSHelperProcess", "--")
			c.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
			return c
		},
		func(string) (string, error) { return "/usr/bin/worktree", nil },
	)
	t.Cleanup(restore)
	return got
}

func TestNSHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	os.Exit(0)
}

func TestReconcile_WorktreeNewer_UpdatesHandlerDB(t *testing.T) {
	d := nsTestDB(t)
	seedHandlerMeta(t, d, "slack", "C1:1.2", "Old", "", "2030-01-01T00:00:00Z")
	got := captureWorktreeSetName(t)

	w := worktreeinterop.Resource{Type: "slack", ID: "C1:1.2", CustomName: "New", CustomDescription: "d", UpdatedAt: "2030-02-02T00:00:00Z"}
	if err := reconcileSlackName(d.Conn(), "/wt", w); err != nil {
		t.Fatal(err)
	}
	m := getHandlerMeta(t, d, "slack", "C1:1.2")
	if m.CustomName != "New" || m.CustomDescription != "d" || m.UpdatedAt != "2030-02-02T00:00:00Z" {
		t.Fatalf("handler meta not updated to worktree's: %+v", m)
	}
	if len(*got) != 0 {
		t.Errorf("worktree-newer must NOT call set-name, got argv %v", *got)
	}
}

func TestReconcile_HandlerNewer_PushesToWorktree(t *testing.T) {
	d := nsTestDB(t)
	seedHandlerMeta(t, d, "slack", "C1:1.2", "Newer name", "hd", "2030-03-03T00:00:00Z")
	got := captureWorktreeSetName(t)

	w := worktreeinterop.Resource{Type: "slack", ID: "C1:1.2", URL: "u", CustomName: "Old", CustomDescription: "", UpdatedAt: "2030-01-01T00:00:00Z"}
	if err := reconcileSlackName(d.Conn(), "/wt", w); err != nil {
		t.Fatal(err)
	}
	want := []string{"worktree", "resources", "set-name", "slack", "C1:1.2",
		"--name", "Newer name", "--updated-at", "2030-03-03T00:00:00Z",
		"--description", "hd", "--worktree", "/wt"}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("push argv got %v want %v", *got, want)
	}
	// handler's own meta must be untouched (it is the winner).
	m := getHandlerMeta(t, d, "slack", "C1:1.2")
	if m.CustomName != "Newer name" || m.UpdatedAt != "2030-03-03T00:00:00Z" {
		t.Errorf("handler meta should be unchanged: %+v", m)
	}
}

func TestReconcile_EqualNames_NoOp(t *testing.T) {
	d := nsTestDB(t)
	seedHandlerMeta(t, d, "slack", "C1:1.2", "Same", "same", "2030-01-01T00:00:00Z")
	got := captureWorktreeSetName(t)

	w := worktreeinterop.Resource{Type: "slack", ID: "C1:1.2", CustomName: "Same", CustomDescription: "same", UpdatedAt: "2030-09-09T00:00:00Z"}
	if err := reconcileSlackName(d.Conn(), "/wt", w); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 0 {
		t.Errorf("equal names must not push, got %v", *got)
	}
	// handler ts must remain the original (no write happened).
	if m := getHandlerMeta(t, d, "slack", "C1:1.2"); m.UpdatedAt != "2030-01-01T00:00:00Z" {
		t.Errorf("equal names must not rewrite handler meta, got ts %q", m.UpdatedAt)
	}
}

func TestReconcile_Converges(t *testing.T) {
	d := nsTestDB(t)
	seedHandlerMeta(t, d, "slack", "C1:1.2", "Old", "", "2030-01-01T00:00:00Z")
	got := captureWorktreeSetName(t)

	w := worktreeinterop.Resource{Type: "slack", ID: "C1:1.2", CustomName: "New", UpdatedAt: "2030-02-02T00:00:00Z"}
	if err := reconcileSlackName(d.Conn(), "/wt", w); err != nil {
		t.Fatal(err)
	}
	// Second pass with the SAME worktree view: now equal → no writes, no calls.
	*got = nil
	if err := reconcileSlackName(d.Conn(), "/wt", w); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 0 {
		t.Errorf("second pass should be a no-op, got argv %v", *got)
	}
}

func TestReconcile_TieWorktreeWins(t *testing.T) {
	d := nsTestDB(t)
	seedHandlerMeta(t, d, "slack", "C1:1.2", "Handler", "", "2030-05-05T00:00:00Z")
	got := captureWorktreeSetName(t)

	w := worktreeinterop.Resource{Type: "slack", ID: "C1:1.2", CustomName: "Worktree", UpdatedAt: "2030-05-05T00:00:00Z"}
	if err := reconcileSlackName(d.Conn(), "/wt", w); err != nil {
		t.Fatal(err)
	}
	if m := getHandlerMeta(t, d, "slack", "C1:1.2"); m.CustomName != "Worktree" {
		t.Errorf("tie should let worktree win in handler DB, got %q", m.CustomName)
	}
	if len(*got) != 0 {
		t.Errorf("tie should not push to worktree, got %v", *got)
	}
}

func TestSlackNameSyncDue_Throttles(t *testing.T) {
	isolateHomes(t)
	if err := os.MkdirAll(filepath.Dir(slackNameSyncMarkerPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if !slackNameSyncDue() {
		t.Fatal("first call should be due")
	}
	if slackNameSyncDue() {
		t.Fatal("second immediate call should be throttled")
	}
	// Backdate the marker beyond the window → due again.
	old := time.Now().Add(-2 * slackNameSyncInterval)
	if err := os.Chtimes(slackNameSyncMarkerPath(), old, old); err != nil {
		t.Fatal(err)
	}
	if !slackNameSyncDue() {
		t.Fatal("after window elapsed, should be due again")
	}
}
