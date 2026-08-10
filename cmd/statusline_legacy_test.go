package cmd

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mturley/agent-handler/db"
)

// When the database holds unmigrated legacy data, the statusline must print a
// single migration-warning line and return nil (exit 0) — it must NOT error,
// since erroring would break every Claude Code session's prompt.
func TestRunStatuslineLegacyWarning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HANDLER_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	// Seed a legacy subscription (marker unset) so the guard fires.
	d, err := db.Open(db.DefaultPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := d.Conn().Exec(`
		INSERT INTO sessions (session_id, harness, repo, branch, status, last_active, registered_at, jsonl_path)
		VALUES ('S1', 'claude', 'r', 'main', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '/tmp/s1.jsonl')
	`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := d.Conn().Exec(`
		INSERT INTO subscriptions (id, session_id, resource_type, resource_id, created_at)
		VALUES ('sub1', 'S1', 'pr', 'example/repo#1', '2026-01-01T00:00:00Z')
	`); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	d.Close()

	// Capture stdout.
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	runErr := runStatusline(statuslineCmd, nil)
	w.Close()
	os.Stdout = origStdout

	if runErr != nil {
		t.Fatalf("runStatusline returned error on legacy DB: %v (must be nil to avoid breaking the prompt)", runErr)
	}
	scanner := bufio.NewScanner(r)
	var out string
	if scanner.Scan() {
		out = scanner.Text()
	}
	if !strings.Contains(out, "migrate-watcher") {
		t.Errorf("statusline output = %q, want a line mentioning migrate-watcher", out)
	}
}
