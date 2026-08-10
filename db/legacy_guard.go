package db

// legacyDataTables are the pre-watcher-library handler tables whose presence
// with data — while the watcher_migrated marker is unset — means the database
// predates the watcher-library migration and must be migrated (or discarded)
// before handler will work correctly. These are the tables the migration
// command copies into the library's watcher_* tables; see
// cmd/migrate_watcher.go and docs/watcher-migration-runbook.md.
var legacyDataTables = []string{
	"subscriptions",
	"resource_state",
	"resource_relationships",
	"watcher_status",
}

// HasUnmigratedLegacyData reports whether this database holds pre-migration
// legacy watcher data that has NOT yet been migrated into the library's
// watcher_* tables. It returns true only when the watcher_migrated marker is
// unset AND at least one legacy source carries data: any of the legacy handler
// tables (subscriptions, resource_state, resource_relationships, watcher_status)
// has a row, or the events table has a github/jira-sourced event (those are the
// events the migration relocates to watcher_events).
//
// It returns false on a freshly-created database (all legacy tables exist but
// are empty, since db.Open runs CREATE TABLE IF NOT EXISTS) and on an
// already-migrated database (marker set). Any query error is treated as
// "no legacy data" (false) rather than blocking setup on a read hiccup.
func (db *DB) HasUnmigratedLegacyData() bool {
	if db.watcherMigrationDone() {
		return false
	}
	for _, table := range legacyDataTables {
		var one int
		// Table name is from the fixed legacyDataTables allow-list, never user
		// input, so interpolating it into the query is safe.
		err := db.conn.QueryRow(`SELECT 1 FROM ` + table + ` LIMIT 1`).Scan(&one)
		if err == nil {
			return true
		}
	}
	var one int
	err := db.conn.QueryRow(
		`SELECT 1 FROM events WHERE source IN ('github', 'jira') LIMIT 1`,
	).Scan(&one)
	return err == nil
}
