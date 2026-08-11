package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mturley/agent-handler/db"
)

// openGuardDB opens a fresh temp database and returns both the handle and its
// path, so a test can seed via the handle and then call guardLegacyDatabaseAt
// against the same path. The handle is closed on cleanup.
func openGuardDB(t *testing.T) (*db.DB, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d, dbPath
}

// A freshly-created database (no legacy tables — Phase 2c removed them from
// schema.sql) must NOT trip the guard under schema-based detection.
func TestGuardLegacyDatabaseFreshDBAllowed(t *testing.T) {
	_, dbPath := openGuardDB(t)
	if err := guardLegacyDatabaseAt(dbPath); err != nil {
		t.Fatalf("guardLegacyDatabaseAt on fresh DB = %v, want nil", err)
	}
}

// A database that still carries the legacy tables must trip the guard with
// exactly the legacyDBError message (schema-based: table presence is the
// signal).
func TestGuardLegacyDatabaseRefusesLegacyData(t *testing.T) {
	d, dbPath := openGuardDB(t)
	seedMigrationFixtures(t, d)

	err := guardLegacyDatabaseAt(dbPath)
	if err == nil {
		t.Fatalf("guardLegacyDatabaseAt on legacy DB = nil, want error")
	}
	if err.Error() != legacyDBError {
		t.Errorf("guardLegacyDatabaseAt message = %q, want %q", err.Error(), legacyDBError)
	}
	if !strings.Contains(err.Error(), "handler setup --migrate-watcher") {
		t.Errorf("guard message should tell the user to run the migration, got %q", err.Error())
	}
}

// Once the legacy tables are dropped (as the migration does), the guard must NOT
// fire even though github/jira rows may still linger in `events` (those are
// purged separately in Task 7 and are not a detection signal).
func TestGuardLegacyDatabaseAllowedAfterTablesDropped(t *testing.T) {
	d, dbPath := openGuardDB(t)
	seedMigrationFixtures(t, d)

	// Drop the legacy tables directly (mirrors what the migration does).
	if _, err := d.Conn().Exec(`
		DROP TABLE IF EXISTS subscriptions;
		DROP TABLE IF EXISTS resource_state;
		DROP TABLE IF EXISTS resource_relationships;
		DROP TABLE IF EXISTS watcher_status;
	`); err != nil {
		t.Fatalf("drop legacy tables: %v", err)
	}

	if err := guardLegacyDatabaseAt(dbPath); err != nil {
		t.Fatalf("guardLegacyDatabaseAt after dropping legacy tables = %v, want nil", err)
	}
}

// A github/jira event alone (no legacy tables) does NOT trip the guard under
// schema-based detection: table presence is the only signal now, and a migrated
// DB legitimately still has github/jira rows in `events` until they are purged.
func TestGuardLegacyDatabaseIgnoresEventsWithoutTables(t *testing.T) {
	d, dbPath := openGuardDB(t)
	if _, err := d.Conn().Exec(`
		INSERT INTO events (id, ts, source, type, title)
		VALUES ('e1', '2026-01-01T00:00:00Z', 'github', 'pr_opened', 'test')
	`); err != nil {
		t.Fatalf("seed github event: %v", err)
	}

	if err := guardLegacyDatabaseAt(dbPath); err != nil {
		t.Fatalf("guardLegacyDatabaseAt with a github event but no legacy tables = %v, want nil", err)
	}
}
