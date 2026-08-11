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

	// Create the legacy tables so schema-based detection trips the guard.
	d, err := db.Open(db.DefaultPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	createLegacyTables(t, d.Conn())
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
