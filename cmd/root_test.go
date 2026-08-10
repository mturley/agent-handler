package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mturley/agent-handler/db"
)

// The legacy-database guard must block ordinary commands (matched by full
// command path) but exempt the recovery/help/statusline/uninstall set. `ui` is
// guarded (the dashboard reads legacy tables); `handler watcher uninstall` is
// exempt but `handler watcher status` is not.
func TestCommandGuardedForLegacyDB(t *testing.T) {
	guarded := []string{
		"handler status", "handler log", "handler triage", "handler watching",
		"handler health", "handler query", "handler cleanup", "handler ui",
		"handler watcher run", "handler watcher status", "handler watcher list",
	}
	for _, path := range guarded {
		if !commandGuardedForLegacyDB(path) {
			t.Errorf("command %q should be guarded against an unmigrated legacy DB", path)
		}
	}

	exempt := []string{
		"handler setup", "handler help", "handler completion", "handler claude",
		"handler statusline", "handler uninstall", "handler watcher uninstall",
	}
	for _, path := range exempt {
		if commandGuardedForLegacyDB(path) {
			t.Errorf("command %q should be EXEMPT from the legacy-DB guard", path)
		}
	}
}

// legacyUnmigrated reflects the database state: false for a fresh DB, true once
// legacy data is present with the marker unset, false again after the marker is
// set.
func TestLegacyUnmigrated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HANDLER_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	// Fresh DB: no legacy data.
	d, err := db.Open(db.DefaultPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if legacyUnmigrated() {
		t.Fatal("legacyUnmigrated() = true on a fresh DB, want false")
	}

	// Seed a legacy subscription -> unmigrated.
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
	if !legacyUnmigrated() {
		t.Fatal("legacyUnmigrated() = false with legacy data + marker unset, want true")
	}

	// Set the marker -> migrated, guard clears.
	if err := d.SetWatcherMigrated(); err != nil {
		t.Fatalf("SetWatcherMigrated: %v", err)
	}
	if legacyUnmigrated() {
		t.Fatal("legacyUnmigrated() = true after marker set, want false")
	}
	d.Close()
}
