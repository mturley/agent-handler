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

// legacyUnmigrated reflects the database state under schema-based detection:
// false for a fresh DB (no legacy tables), true once a legacy table is present,
// false again after the legacy tables are dropped (as the migration does).
func TestLegacyUnmigrated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HANDLER_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "data"), 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	// Fresh DB: no legacy tables.
	d, err := db.Open(db.DefaultPath())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if legacyUnmigrated() {
		t.Fatal("legacyUnmigrated() = true on a fresh DB, want false")
	}

	// Create a legacy table -> unmigrated (table presence is the signal).
	createLegacyTables(t, d.Conn())
	if !legacyUnmigrated() {
		t.Fatal("legacyUnmigrated() = false with legacy tables present, want true")
	}

	// Drop the legacy tables (what the migration does) -> migrated, guard clears.
	if _, err := d.Conn().Exec(`
		DROP TABLE IF EXISTS subscriptions;
		DROP TABLE IF EXISTS resource_state;
		DROP TABLE IF EXISTS resource_relationships;
		DROP TABLE IF EXISTS watcher_status;
	`); err != nil {
		t.Fatalf("drop legacy tables: %v", err)
	}
	if legacyUnmigrated() {
		t.Fatal("legacyUnmigrated() = true after legacy tables dropped, want false")
	}
	d.Close()
}
