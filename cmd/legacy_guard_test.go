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

// A freshly-created database (all legacy tables exist but are empty, marker
// unset) must NOT trip the guard.
func TestGuardLegacyDatabaseFreshDBAllowed(t *testing.T) {
	_, dbPath := openGuardDB(t)
	if err := guardLegacyDatabaseAt(dbPath); err != nil {
		t.Fatalf("guardLegacyDatabaseAt on fresh DB = %v, want nil", err)
	}
}

// A database carrying legacy data with the marker unset must trip the guard,
// with exactly the legacyDBError message.
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

// Once the migration marker is set, the guard must NOT fire even though the
// legacy tables still hold their (now-backup) rows.
func TestGuardLegacyDatabaseMigratedAllowed(t *testing.T) {
	d, dbPath := openGuardDB(t)
	seedMigrationFixtures(t, d)
	if err := d.SetWatcherMigrated(); err != nil {
		t.Fatalf("SetWatcherMigrated: %v", err)
	}

	if err := guardLegacyDatabaseAt(dbPath); err != nil {
		t.Fatalf("guardLegacyDatabaseAt on migrated DB = %v, want nil (marker set)", err)
	}
}

// The events table alone (a github/jira event, no legacy subscription/state
// rows) is enough to trip the guard, since those events are relocated by the
// migration.
func TestGuardLegacyDatabaseFiresOnEventsOnly(t *testing.T) {
	d, dbPath := openGuardDB(t)
	if _, err := d.Conn().Exec(`
		INSERT INTO events (id, ts, source, type, title)
		VALUES ('e1', '2026-01-01T00:00:00Z', 'github', 'pr_opened', 'test')
	`); err != nil {
		t.Fatalf("seed github event: %v", err)
	}

	if err := guardLegacyDatabaseAt(dbPath); err == nil {
		t.Fatalf("guardLegacyDatabaseAt with a github event = nil, want error")
	}
}

// An agent-sourced event (not relocated by the migration) does NOT by itself
// trip the guard — only github/jira events count.
func TestGuardLegacyDatabaseIgnoresAgentEvents(t *testing.T) {
	d, dbPath := openGuardDB(t)
	if _, err := d.Conn().Exec(`
		INSERT INTO events (id, ts, source, type, title)
		VALUES ('e1', '2026-01-01T00:00:00Z', 'agent', 'status', 'test')
	`); err != nil {
		t.Fatalf("seed agent event: %v", err)
	}

	if err := guardLegacyDatabaseAt(dbPath); err != nil {
		t.Fatalf("guardLegacyDatabaseAt with only an agent event = %v, want nil", err)
	}
}
