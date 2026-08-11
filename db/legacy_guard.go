package db

import "database/sql"

// legacyDataTables are the pre-watcher-library handler tables. Their PRESENCE in
// the schema means this database predates the watcher-library migration and must
// be migrated (or discarded) before handler will work correctly. They are
// created only on pre-2c databases; fresh installs never create them (they were
// removed from schema.sql in Phase 2c), and `handler setup --migrate-watcher`
// drops them after copying their data into the library's watcher_* tables. See
// cmd/migrate_watcher.go and docs/watcher-migration-runbook.md.
var legacyDataTables = []string{
	"subscriptions",
	"resource_state",
	"resource_relationships",
	"watcher_status",
}

// HasUnmigratedLegacyData reports whether this database still has any legacy
// watcher table in its schema. Detection is purely structural: if none of the
// legacy tables exist, the database is either a fresh install or already
// migrated. (github/jira rows may still linger in `events` post-migration until
// purged, but that is NOT a "must migrate" signal — table presence is.)
//
// A 2b-migrated database (data copied into watcher_*, but the legacy tables
// retained and the watcher_migrated marker set) still has the legacy tables, so
// this returns true for it. That is intentional: `handler setup
// --migrate-watcher` treats such a DB as "migration half-done" and finishes the
// cleanup (drops the tables) on a re-run, after which detection is correct. The
// migration command — not this detector — consults the marker to decide whether
// to re-copy or just finish; see cmd/migrate_watcher.go.
func (db *DB) HasUnmigratedLegacyData() bool {
	return anyLegacyTableExists(db.conn)
}

// anyLegacyTableExists reports whether any of the legacy watcher tables exists
// in the given database's schema (via sqlite_master).
func anyLegacyTableExists(conn *sql.DB) bool {
	for _, table := range legacyDataTables {
		var name string
		err := conn.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, table,
		).Scan(&name)
		if err == nil {
			return true
		}
	}
	return false
}

// EnsureLegacySubscriptionsColumn brings a pre-2c database's `subscriptions`
// table current before the migration reads it, by adding the `unsubscribed_by`
// column when absent. Historically this ran in runMigrations on every Open;
// Phase 2c moved it here so all legacy-schema knowledge lives alongside the
// migration path. It is a no-op when the legacy `subscriptions` table is absent
// (the pragma returns no rows) or the column already exists.
func EnsureLegacySubscriptionsColumn(conn *sql.DB) error {
	return addColumnIfMissing(conn, "subscriptions", "unsubscribed_by", "TEXT")
}
