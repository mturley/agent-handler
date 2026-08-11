package cmd

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	wcfg "github.com/mturley/watcher/config"
)

// testMigrateDB opens a temp database for migration tests. CRITICAL: this
// must NEVER point at db.DefaultPath() / the real ~/.agent-handler/handler.db.
func testMigrateDB(t *testing.T) *db.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// legacyTableDDL holds the pre-2c CREATE TABLE (+ index) statement for each of
// the four legacy watcher tables, exactly as the pre-2c db/schema.sql defined
// them. Phase 2c removed these blocks from schema.sql (fresh installs never
// create them), so migration/guard tests that need legacy data must create the
// tables themselves before seeding. This is the single source of the legacy
// DDL for tests.
var legacyTableDDL = map[string]string{
	"subscriptions": `
		CREATE TABLE IF NOT EXISTS subscriptions (
		    id TEXT PRIMARY KEY,
		    session_id TEXT NOT NULL REFERENCES sessions(session_id),
		    resource_type TEXT NOT NULL,
		    resource_id TEXT NOT NULL,
		    resource_url TEXT,
		    created_at TEXT NOT NULL,
		    deleted_at TEXT,
		    unsubscribed_by TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_subscriptions_resource ON subscriptions(resource_type, resource_id, deleted_at);
	`,
	"watcher_status": `
		CREATE TABLE IF NOT EXISTS watcher_status (
		    name TEXT PRIMARY KEY,
		    last_success TEXT,
		    last_error TEXT,
		    last_error_message TEXT
		);
	`,
	"resource_relationships": `
		CREATE TABLE IF NOT EXISTS resource_relationships (
		    id TEXT PRIMARY KEY,
		    child_type TEXT NOT NULL,
		    child_id TEXT NOT NULL,
		    child_url TEXT,
		    parent_type TEXT NOT NULL,
		    parent_id TEXT NOT NULL,
		    parent_url TEXT,
		    relationship TEXT NOT NULL,
		    source TEXT NOT NULL,
		    created_at TEXT NOT NULL
		);
	`,
	"resource_state": `
		CREATE TABLE IF NOT EXISTS resource_state (
		    resource_type TEXT NOT NULL,
		    resource_id TEXT NOT NULL,
		    state_json TEXT NOT NULL,
		    resource_updated_at TEXT NOT NULL,
		    watcher_updated_at TEXT NOT NULL,
		    PRIMARY KEY (resource_type, resource_id)
		);
	`,
}

// createLegacyTables recreates all four legacy watcher tables and their
// indexes. See legacyTableDDL for the DDL source.
func createLegacyTables(t *testing.T, conn interface {
	Exec(string, ...interface{}) (sql.Result, error)
}) {
	t.Helper()
	createLegacyTablesSubset(t, conn, "subscriptions", "watcher_status", "resource_relationships", "resource_state")
}

// createLegacyTablesSubset creates only the named legacy watcher tables
// (each must be a key of legacyTableDDL), for tests exercising a
// partially-migrated / hand-corrupted database where only some of the four
// legacy tables are present (see #8).
func createLegacyTablesSubset(t *testing.T, conn interface {
	Exec(string, ...interface{}) (sql.Result, error)
}, tables ...string) {
	t.Helper()
	for _, name := range tables {
		ddl, ok := legacyTableDDL[name]
		if !ok {
			t.Fatalf("createLegacyTablesSubset: unknown legacy table %q", name)
		}
		if _, err := conn.Exec(ddl); err != nil {
			t.Fatalf("createLegacyTablesSubset(%s): %v", name, err)
		}
	}
}

// legacyTablesExist reports whether all four legacy tables currently exist in
// the database's schema (via sqlite_master). Used by tests to assert the
// migration dropped them.
func legacyTablesExist(t *testing.T, d *db.DB) bool {
	t.Helper()
	var count int
	if err := d.Conn().QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type='table'
		AND name IN ('subscriptions','resource_state','resource_relationships','watcher_status')
	`).Scan(&count); err != nil {
		t.Fatalf("legacyTablesExist query: %v", err)
	}
	return count > 0
}

// seedMigrationFixtures creates the legacy tables (removed from schema.sql in
// Phase 2c) and populates them with a small, deterministic dataset covering
// every mapping the migration performs: sessions (active + archived),
// github/jira/agent events (+resources), resource_state, subscriptions
// (active/normal, user-unsubscribed+deleted), resource_relationships, and the
// legacy watcher_status table.
func seedMigrationFixtures(t *testing.T, d *db.DB) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	conn := d.Conn()

	createLegacyTables(t, conn)

	// Sessions: S1 active, S2 archived.
	if _, err := conn.Exec(`
		INSERT INTO sessions (session_id, harness, repo, branch, status, last_active, registered_at, jsonl_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "S1", "claude", "github.com/example/repo", "main", "active", now, now, "/tmp/s1.jsonl"); err != nil {
		t.Fatalf("seed session S1: %v", err)
	}
	if _, err := conn.Exec(`
		INSERT INTO sessions (session_id, harness, repo, branch, status, last_active, registered_at, jsonl_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "S2", "claude", "github.com/example/repo", "main", "archived", now, now, "/tmp/s2.jsonl"); err != nil {
		t.Fatalf("seed session S2: %v", err)
	}

	// Events: 2 to be migrated (github, jira), 1 to be excluded (agent).
	events := []struct {
		id, source string
	}{
		{"evt-gh-1", "github"},
		{"evt-jira-1", "jira"},
		{"evt-agent-1", "agent"},
	}
	for _, e := range events {
		if _, err := conn.Exec(`
			INSERT INTO events (id, ts, source, type, title)
			VALUES (?, ?, ?, ?, ?)
		`, e.id, now, e.source, "some_event", "Title for "+e.id); err != nil {
			t.Fatalf("seed event %s: %v", e.id, err)
		}
	}
	// event_resources for the two migrated events only.
	for _, id := range []string{"evt-gh-1", "evt-jira-1"} {
		if _, err := conn.Exec(`
			INSERT INTO event_resources (event_id, resource_type, resource_id, resource_url)
			VALUES (?, ?, ?, ?)
		`, id, "pr", "example/repo#1", "https://github.com/example/repo/pull/1"); err != nil {
			t.Fatalf("seed event_resources for %s: %v", id, err)
		}
	}

	// resource_state: one row.
	if _, err := conn.Exec(`
		INSERT INTO resource_state (resource_type, resource_id, state_json, resource_updated_at, watcher_updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, "pr", "example/repo#1", `{"state":"open"}`, now, now); err != nil {
		t.Fatalf("seed resource_state: %v", err)
	}

	// subscriptions:
	//   sub-1: S1 (active), not unsubscribed -> should end up live (future expires_at).
	//   sub-2: S2 (archived), user-unsubscribed + deleted -> a tombstone; proves
	//          nothing about the dangerous orphaned-live case below.
	//   sub-3: S1 (active), not unsubscribed (second active row).
	//   sub-4: S2 (archived), deleted_at NULL, unsubscribed_by NULL -- an
	//          ORPHANED-LIVE subscription (never soft-deleted, session just
	//          went archived without cleanup ever revoking it). This is the
	//          dangerous case H1 covers: it must age out (expires_at in the
	//          past), NOT get a permanent NULL lease.
	if _, err := conn.Exec(`
		INSERT INTO subscriptions (id, session_id, resource_type, resource_id, resource_url, created_at, deleted_at, unsubscribed_by)
		VALUES (?, ?, ?, ?, ?, ?, NULL, NULL)
	`, "sub-1", "S1", "pr", "example/repo#1", "https://github.com/example/repo/pull/1", now); err != nil {
		t.Fatalf("seed subscription sub-1: %v", err)
	}
	if _, err := conn.Exec(`
		INSERT INTO subscriptions (id, session_id, resource_type, resource_id, resource_url, created_at, deleted_at, unsubscribed_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "sub-2", "S2", "jira", "PROJ-1", "", now, now, "user"); err != nil {
		t.Fatalf("seed subscription sub-2: %v", err)
	}
	if _, err := conn.Exec(`
		INSERT INTO subscriptions (id, session_id, resource_type, resource_id, resource_url, created_at, deleted_at, unsubscribed_by)
		VALUES (?, ?, ?, ?, ?, ?, NULL, NULL)
	`, "sub-3", "S1", "jira", "PROJ-2", "", now); err != nil {
		t.Fatalf("seed subscription sub-3: %v", err)
	}
	if _, err := conn.Exec(`
		INSERT INTO subscriptions (id, session_id, resource_type, resource_id, resource_url, created_at, deleted_at, unsubscribed_by)
		VALUES (?, ?, ?, ?, ?, ?, NULL, NULL)
	`, "sub-4", "S2", "pr", "example/repo#2", "https://github.com/example/repo/pull/2", now); err != nil {
		t.Fatalf("seed subscription sub-4: %v", err)
	}

	// resource_relationships: one row.
	if _, err := conn.Exec(`
		INSERT INTO resource_relationships (id, child_type, child_id, child_url, parent_type, parent_id, parent_url, relationship, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "rel-1", "pr", "example/repo#1", "https://github.com/example/repo/pull/1", "jira", "PROJ-1", "", "linked", "github", now); err != nil {
		t.Fatalf("seed resource_relationships: %v", err)
	}

	// legacy watcher_status: one row.
	if _, err := conn.Exec(`
		INSERT INTO watcher_status (name, last_success, last_error, last_error_message)
		VALUES (?, ?, NULL, NULL)
	`, "github", now); err != nil {
		t.Fatalf("seed watcher_status: %v", err)
	}
}

func TestMigrateWatcherData(t *testing.T) {
	d := testMigrateDB(t)
	seedMigrationFixtures(t, d)

	report, err := migrateWatcherData(d, false)
	if err != nil {
		t.Fatalf("migrateWatcherData failed: %v", err)
	}

	conn := d.Conn()

	var eventCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM watcher_events`).Scan(&eventCount); err != nil {
		t.Fatalf("count watcher_events: %v", err)
	}
	if eventCount != 2 {
		t.Errorf("watcher_events count = %d, want 2 (agent event must not be copied)", eventCount)
	}
	if report.Events != 2 {
		t.Errorf("report.Events = %d, want 2", report.Events)
	}

	var eventResourceCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM watcher_event_resources`).Scan(&eventResourceCount); err != nil {
		t.Fatalf("count watcher_event_resources: %v", err)
	}
	if eventResourceCount != 2 {
		t.Errorf("watcher_event_resources count = %d, want 2", eventResourceCount)
	}
	if report.EventResources != 2 {
		t.Errorf("report.EventResources = %d, want 2", report.EventResources)
	}

	var resourceStateCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM watcher_resource_state`).Scan(&resourceStateCount); err != nil {
		t.Fatalf("count watcher_resource_state: %v", err)
	}
	if resourceStateCount != 1 {
		t.Errorf("watcher_resource_state count = %d, want 1", resourceStateCount)
	}
	if report.ResourceState != 1 {
		t.Errorf("report.ResourceState = %d, want 1", report.ResourceState)
	}

	var subCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM watcher_subscriptions`).Scan(&subCount); err != nil {
		t.Fatalf("count watcher_subscriptions: %v", err)
	}
	if subCount != 4 {
		t.Errorf("watcher_subscriptions count = %d, want 4", subCount)
	}
	if report.Subscriptions != 4 {
		t.Errorf("report.Subscriptions = %d, want 4", report.Subscriptions)
	}

	// sub-1 (S1, active session, not unsubscribed): subscriber prefix, no
	// deleted_at, non-null expires_at in the future.
	var subscriber1 string
	var deletedAt1 *string
	var expiresAt1 *string
	if err := conn.QueryRow(`SELECT subscriber, deleted_at, expires_at FROM watcher_subscriptions WHERE id = ?`, "sub-1").
		Scan(&subscriber1, &deletedAt1, &expiresAt1); err != nil {
		t.Fatalf("query sub-1: %v", err)
	}
	if subscriber1 != "handler:session:S1" {
		t.Errorf("sub-1 subscriber = %q, want %q", subscriber1, "handler:session:S1")
	}
	if deletedAt1 != nil {
		t.Errorf("sub-1 deleted_at = %v, want nil", *deletedAt1)
	}
	if expiresAt1 == nil {
		t.Fatalf("sub-1 expires_at = nil, want non-null (S1 is active)")
	}
	parsedExpiry, err := time.Parse(time.RFC3339, *expiresAt1)
	if err != nil {
		t.Fatalf("sub-1 expires_at %q not RFC3339: %v", *expiresAt1, err)
	}
	if !parsedExpiry.After(time.Now().UTC()) {
		t.Errorf("sub-1 expires_at %q not in the future", *expiresAt1)
	}

	// sub-3 (S1, active session, not unsubscribed, second active row):
	// non-null expires_at in the future, same as sub-1.
	var expiresAt3 *string
	if err := conn.QueryRow(`SELECT expires_at FROM watcher_subscriptions WHERE id = ?`, "sub-3").
		Scan(&expiresAt3); err != nil {
		t.Fatalf("query sub-3: %v", err)
	}
	if expiresAt3 == nil {
		t.Fatalf("sub-3 expires_at = nil, want non-null (S1 is active)")
	}
	parsedExpiry3, err := time.Parse(time.RFC3339, *expiresAt3)
	if err != nil {
		t.Fatalf("sub-3 expires_at %q not RFC3339: %v", *expiresAt3, err)
	}
	if !parsedExpiry3.After(time.Now().UTC()) {
		t.Errorf("sub-3 expires_at %q not in the future", *expiresAt3)
	}

	// sub-2 (S2, archived session, user-unsubscribed+deleted): subscriber
	// prefix, unsubscribed_by_user=1, non-null deleted_at. S2 is archived
	// (non-active), so per H1's fix expires_at must be a non-null,
	// already-elapsed timestamp (aged out), NOT nil (nil would mean a
	// PERMANENT lease in the watcher library's live-subscription predicate).
	// This row is also a deleted tombstone, so it stays excluded from live
	// queries via deleted_at regardless — but expires_at must still reflect
	// "not active" correctly, on principle and in case deleted_at is ever
	// cleared/reinstated.
	var subscriber2 string
	var deletedAt2 *string
	var expiresAt2 *string
	var unsubscribedByUser2 int
	if err := conn.QueryRow(`SELECT subscriber, deleted_at, expires_at, unsubscribed_by_user FROM watcher_subscriptions WHERE id = ?`, "sub-2").
		Scan(&subscriber2, &deletedAt2, &expiresAt2, &unsubscribedByUser2); err != nil {
		t.Fatalf("query sub-2: %v", err)
	}
	if subscriber2 != "handler:session:S2" {
		t.Errorf("sub-2 subscriber = %q, want %q", subscriber2, "handler:session:S2")
	}
	if unsubscribedByUser2 != 1 {
		t.Errorf("sub-2 unsubscribed_by_user = %d, want 1", unsubscribedByUser2)
	}
	if deletedAt2 == nil {
		t.Errorf("sub-2 deleted_at = nil, want non-null")
	}
	if expiresAt2 == nil {
		t.Fatalf("sub-2 expires_at = nil, want non-null aged-out timestamp (S2 is archived; NULL would mean a PERMANENT lease)")
	}
	parsedExpiry2, err := time.Parse(time.RFC3339, *expiresAt2)
	if err != nil {
		t.Fatalf("sub-2 expires_at %q not RFC3339: %v", *expiresAt2, err)
	}
	if parsedExpiry2.After(time.Now().UTC()) {
		t.Errorf("sub-2 expires_at %q is in the future, want already elapsed (S2 is archived)", *expiresAt2)
	}

	// sub-4 (S2, archived session, ORPHANED-LIVE: deleted_at NULL,
	// unsubscribed_by NULL): this is the dangerous case H1 covers. It must
	// age out via a non-null, already-elapsed expires_at — a NULL here
	// would make the watcher library treat it as permanently live, so a
	// long-dead session's subscription would poll GitHub/Jira forever.
	var subscriber4 string
	var deletedAt4 *string
	var expiresAt4 *string
	if err := conn.QueryRow(`SELECT subscriber, deleted_at, expires_at FROM watcher_subscriptions WHERE id = ?`, "sub-4").
		Scan(&subscriber4, &deletedAt4, &expiresAt4); err != nil {
		t.Fatalf("query sub-4: %v", err)
	}
	if subscriber4 != "handler:session:S2" {
		t.Errorf("sub-4 subscriber = %q, want %q", subscriber4, "handler:session:S2")
	}
	if deletedAt4 != nil {
		t.Errorf("sub-4 deleted_at = %v, want nil (not soft-deleted in source)", *deletedAt4)
	}
	if expiresAt4 == nil {
		t.Fatalf("sub-4 expires_at = nil, want non-null aged-out timestamp (S2 is archived; NULL would leave this orphan permanently live)")
	}
	parsedExpiry4, err := time.Parse(time.RFC3339, *expiresAt4)
	if err != nil {
		t.Fatalf("sub-4 expires_at %q not RFC3339: %v", *expiresAt4, err)
	}
	if parsedExpiry4.After(time.Now().UTC()) {
		t.Errorf("sub-4 expires_at %q is in the future, want already elapsed (orphaned archived session must age out)", *expiresAt4)
	}

	var relCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM watcher_resource_relationships`).Scan(&relCount); err != nil {
		t.Fatalf("count watcher_resource_relationships: %v", err)
	}
	if relCount != 1 {
		t.Errorf("watcher_resource_relationships count = %d, want 1", relCount)
	}
	if report.Relationships != 1 {
		t.Errorf("report.Relationships = %d, want 1", report.Relationships)
	}

	var pollerStatusCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM watcher_poller_status`).Scan(&pollerStatusCount); err != nil {
		t.Fatalf("count watcher_poller_status: %v", err)
	}
	if pollerStatusCount != 1 {
		t.Errorf("watcher_poller_status count = %d, want 1", pollerStatusCount)
	}
	if report.PollerStatus != 1 {
		t.Errorf("report.PollerStatus = %d, want 1", report.PollerStatus)
	}

	// After a full migration the legacy tables are DROPPED (schema-based
	// detection now signals "migrated" by their absence, not by a marker).
	if legacyTablesExist(t, d) {
		t.Errorf("legacy tables still exist after full migration, want dropped")
	}
	if d.HasUnmigratedLegacyData() {
		t.Errorf("HasUnmigratedLegacyData() = true after full migration, want false")
	}
}

// TestMigrateWatcherDataPartialLegacyTables covers GitHub issue #8:
// HasUnmigratedLegacyData fires on ANY ONE of the four legacy tables
// existing, but a hand-corrupted or partially-migrated database may have only
// a subset of them. The copy and source-count paths must tolerate the
// individually-absent tables (skip + zero-count) rather than aborting
// mid-command with "no such table".
//
// Only `subscriptions` and `resource_state` are created here;
// `resource_relationships` and `watcher_status` are deliberately absent.
func TestMigrateWatcherDataPartialLegacyTables(t *testing.T) {
	d := testMigrateDB(t)
	conn := d.Conn()
	now := time.Now().UTC().Format(time.RFC3339)

	createLegacyTablesSubset(t, conn, "subscriptions", "resource_state")

	if _, err := conn.Exec(`
		INSERT INTO sessions (session_id, harness, repo, branch, status, last_active, registered_at, jsonl_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, "S1", "claude", "github.com/example/repo", "main", "active", now, now, "/tmp/s1.jsonl"); err != nil {
		t.Fatalf("seed session S1: %v", err)
	}

	if _, err := conn.Exec(`
		INSERT INTO subscriptions (id, session_id, resource_type, resource_id, resource_url, created_at, deleted_at, unsubscribed_by)
		VALUES (?, ?, ?, ?, ?, ?, NULL, NULL)
	`, "sub-1", "S1", "pr", "example/repo#1", "https://github.com/example/repo/pull/1", now); err != nil {
		t.Fatalf("seed subscription sub-1: %v", err)
	}
	if _, err := conn.Exec(`
		INSERT INTO resource_state (resource_type, resource_id, state_json, resource_updated_at, watcher_updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, "pr", "example/repo#1", `{"state":"open"}`, now, now); err != nil {
		t.Fatalf("seed resource_state: %v", err)
	}

	if !d.HasUnmigratedLegacyData() {
		t.Fatalf("HasUnmigratedLegacyData() = false before migration, want true (subscriptions/resource_state present)")
	}

	preCounts, err := sourceRowCounts(d)
	if err != nil {
		t.Fatalf("sourceRowCounts failed: %v", err)
	}
	if preCounts.Subscriptions != 1 {
		t.Errorf("preCounts.Subscriptions = %d, want 1", preCounts.Subscriptions)
	}
	if preCounts.ResourceState != 1 {
		t.Errorf("preCounts.ResourceState = %d, want 1", preCounts.ResourceState)
	}
	if preCounts.Relationships != 0 {
		t.Errorf("preCounts.Relationships = %d, want 0 (table absent)", preCounts.Relationships)
	}
	if preCounts.PollerStatus != 0 {
		t.Errorf("preCounts.PollerStatus = %d, want 0 (table absent)", preCounts.PollerStatus)
	}

	report, err := migrateWatcherData(d, false)
	if err != nil {
		t.Fatalf("migrateWatcherData failed: %v (must not error on individually-absent legacy tables)", err)
	}

	if report.Subscriptions != 1 {
		t.Errorf("report.Subscriptions = %d, want 1", report.Subscriptions)
	}
	if report.ResourceState != 1 {
		t.Errorf("report.ResourceState = %d, want 1", report.ResourceState)
	}
	if report.Relationships != 0 {
		t.Errorf("report.Relationships = %d, want 0 (table was absent)", report.Relationships)
	}
	if report.PollerStatus != 0 {
		t.Errorf("report.PollerStatus = %d, want 0 (table was absent)", report.PollerStatus)
	}

	if got := countRows(t, d, "watcher_subscriptions"); got != 1 {
		t.Errorf("watcher_subscriptions = %d, want 1", got)
	}
	if got := countRows(t, d, "watcher_resource_state"); got != 1 {
		t.Errorf("watcher_resource_state = %d, want 1", got)
	}

	if legacyTablesExist(t, d) {
		t.Errorf("legacy tables still exist after migration, want dropped (including the previously-absent ones, via DROP TABLE IF EXISTS)")
	}
	if d.HasUnmigratedLegacyData() {
		t.Errorf("HasUnmigratedLegacyData() = true after migration, want false")
	}
}

// TestMigrateWatcherDataFinishPath covers the Option-B "finish a prior 2b
// migration" state: legacy tables present but their data already lives in
// watcher_* (as if 2b copied it). With finishOnly=true the migration must NOT
// re-copy (no double-insert) and MUST drop the legacy tables.
func TestMigrateWatcherDataFinishPath(t *testing.T) {
	d := testMigrateDB(t)
	seedMigrationFixtures(t, d)

	// Simulate a 2b-migrated DB: run the full copy once so watcher_* holds the
	// data, then recreate the legacy tables (2b retained them) so the finish
	// path has tables to drop without re-copying.
	if _, err := migrateWatcherData(d, false); err != nil {
		t.Fatalf("initial full migrate failed: %v", err)
	}
	createLegacyTables(t, d.Conn())
	if !legacyTablesExist(t, d) {
		t.Fatalf("legacy tables not recreated for finish-path setup")
	}

	before := countRows(t, d, "watcher_events")
	beforeSubs := countRows(t, d, "watcher_subscriptions")

	if _, err := migrateWatcherData(d, true); err != nil {
		t.Fatalf("finish-path migrateWatcherData failed: %v", err)
	}

	// No double-copy.
	if after := countRows(t, d, "watcher_events"); after != before {
		t.Errorf("watcher_events changed on finish path: before=%d after=%d (double-copy)", before, after)
	}
	if after := countRows(t, d, "watcher_subscriptions"); after != beforeSubs {
		t.Errorf("watcher_subscriptions changed on finish path: before=%d after=%d (double-copy)", beforeSubs, after)
	}
	// Tables dropped.
	if legacyTablesExist(t, d) {
		t.Errorf("legacy tables still exist after finish path, want dropped")
	}
	if d.HasUnmigratedLegacyData() {
		t.Errorf("HasUnmigratedLegacyData() = true after finish path, want false")
	}
}

func countRows(t *testing.T, d *db.DB, table string) int {
	t.Helper()
	var n int
	if err := d.Conn().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// isolateHomes points HANDLER_HOME and WATCHER_HOME at fresh temp
// directories for the duration of a test, so runMigrateWatcherAt's
// best-effort credential migration (which reads/writes config.DefaultPath()
// and wcfg.DefaultPath()) can NEVER touch the real
// ~/.agent-handler/config.yaml or ~/.config/watcher/auth.yaml files.
func isolateHomes(t *testing.T) {
	t.Helper()
	t.Setenv("HANDLER_HOME", filepath.Join(t.TempDir(), "handler-home"))
	t.Setenv("WATCHER_HOME", filepath.Join(t.TempDir(), "watcher-home"))
}

// TestMigrateBehaviorConfigSeedsEmptyConfig verifies that, given a handler
// config with Jira custom_fields and bot_usernames and no existing
// watcher config.yaml, migrateBehaviorConfig writes both into config.yaml
// (the behavior file), NOT auth.yaml (credentials).
func TestMigrateBehaviorConfigSeedsEmptyConfig(t *testing.T) {
	isolateHomes(t)

	hcfg := &config.Config{
		Services: config.Services{
			Jira: &config.JiraConfig{
				URL:          "https://example.atlassian.net",
				Email:        "user@example.com",
				Token:        "tok",
				CustomFields: map[string]string{"epic_key": "customfield_10014"},
				BotUsernames: []string{"bot1", "bot2"},
			},
		},
	}

	migrateBehaviorConfig(hcfg)

	bcfg, err := wcfg.LoadConfig(wcfg.ConfigDefaultPath())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cf := bcfg.JiraCustomFields()
	if len(cf) != 1 || cf["epic_key"] != "customfield_10014" {
		t.Errorf("JiraCustomFields() = %v, want map[epic_key:customfield_10014]", cf)
	}
	bots := bcfg.JiraBotUsernames()
	if len(bots) != 2 || bots[0] != "bot1" || bots[1] != "bot2" {
		t.Errorf("JiraBotUsernames() = %v, want [bot1 bot2]", bots)
	}
}

// TestMigrateBehaviorConfigDoesNotOverwriteExisting verifies that
// migrateBehaviorConfig never clobbers values already present in an
// existing config.yaml, even when the handler config has different values.
func TestMigrateBehaviorConfigDoesNotOverwriteExisting(t *testing.T) {
	isolateHomes(t)

	existing := &wcfg.ConfigFile{
		Jira: &wcfg.JiraBehavior{
			CustomFields: map[string]string{"epic_key": "customfield_99999"},
			BotUsernames: []string{"existing-bot"},
		},
	}
	if err := writeWatcherBehaviorConfig(existing); err != nil {
		t.Fatalf("seed existing config.yaml: %v", err)
	}

	hcfg := &config.Config{
		Services: config.Services{
			Jira: &config.JiraConfig{
				CustomFields: map[string]string{"epic_key": "customfield_10014"},
				BotUsernames: []string{"bot1", "bot2"},
			},
		},
	}

	migrateBehaviorConfig(hcfg)

	bcfg, err := wcfg.LoadConfig(wcfg.ConfigDefaultPath())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cf := bcfg.JiraCustomFields()
	if len(cf) != 1 || cf["epic_key"] != "customfield_99999" {
		t.Errorf("JiraCustomFields() = %v, want unchanged map[epic_key:customfield_99999]", cf)
	}
	bots := bcfg.JiraBotUsernames()
	if len(bots) != 1 || bots[0] != "existing-bot" {
		t.Errorf("JiraBotUsernames() = %v, want unchanged [existing-bot]", bots)
	}
}

// TestMigrateCredentialsDoesNotWriteCustomFieldsToAuth verifies that, after
// Step 3's change, migrateCredentials no longer writes Jira custom_fields
// into auth.yaml — that's now migrateBehaviorConfig's job, targeting
// config.yaml instead.
func TestMigrateCredentialsDoesNotWriteCustomFieldsToAuth(t *testing.T) {
	isolateHomes(t)

	hcfg := &config.Config{
		Services: config.Services{
			Jira: &config.JiraConfig{
				URL:          "https://example.atlassian.net",
				Email:        "user@example.com",
				Token:        "tok",
				CustomFields: map[string]string{"epic_key": "customfield_10014"},
			},
		},
	}
	if err := config.Write(config.DefaultPath(), hcfg); err != nil {
		t.Fatalf("write handler config: %v", err)
	}

	migrateCredentials()

	acfg, err := wcfg.Load(wcfg.DefaultPath())
	if err != nil {
		t.Fatalf("Load auth.yaml: %v", err)
	}
	if len(acfg.JiraCustomFields) != 0 {
		t.Errorf("acfg.JiraCustomFields = %v, want empty (custom_fields no longer written to auth.yaml)", acfg.JiraCustomFields)
	}
	if acfg.Services.Jira == nil || acfg.Services.Jira.Token != "tok" {
		t.Errorf("Jira credentials not copied to auth.yaml as expected: %+v", acfg.Services.Jira)
	}
}

// countEventsBySource returns the number of rows in the legacy `events` table
// matching the given source.
func countEventsBySource(t *testing.T, d *db.DB, source string) int {
	t.Helper()
	var n int
	if err := d.Conn().QueryRow(`SELECT COUNT(*) FROM events WHERE source = ?`, source).Scan(&n); err != nil {
		t.Fatalf("count events source=%s: %v", source, err)
	}
	return n
}

// TestRunMigrateWatcherAtPurgesEvents covers the full migration path: after
// running the command against a legacy DB (marker unset), the github/jira rows
// in the legacy `events` table must be purged (they now live in
// watcher_events), while the agent-sourced event remains untouched.
func TestRunMigrateWatcherAtPurgesEvents(t *testing.T) {
	isolateHomes(t)
	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	seedMigrationFixtures(t, d)
	d.Close()

	if err := runMigrateWatcherAt(dbPath, true); err != nil {
		t.Fatalf("runMigrateWatcherAt failed: %v", err)
	}

	d2, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer d2.Close()

	if got := countEventsBySource(t, d2, "github"); got != 0 {
		t.Errorf("events(source=github) = %d after migration, want 0 (purged)", got)
	}
	if got := countEventsBySource(t, d2, "jira"); got != 0 {
		t.Errorf("events(source=jira) = %d after migration, want 0 (purged)", got)
	}
	if got := countEventsBySource(t, d2, "agent"); got != 1 {
		t.Errorf("events(source=agent) = %d after migration, want 1 (agent events must NOT be purged)", got)
	}
	if got := countRows(t, d2, "watcher_events"); got != 2 {
		t.Errorf("watcher_events = %d after migration, want 2 (copied rows retained)", got)
	}

	// event_resources for the purged events must also be gone.
	var orphanResources int
	if err := d2.Conn().QueryRow(`
		SELECT COUNT(*) FROM event_resources
		WHERE event_id IN ('evt-gh-1', 'evt-jira-1')
	`).Scan(&orphanResources); err != nil {
		t.Fatalf("count orphan event_resources: %v", err)
	}
	if orphanResources != 0 {
		t.Errorf("event_resources for purged events = %d, want 0", orphanResources)
	}
}

func TestRunMigrateWatcherAtDoubleRunRefuses(t *testing.T) {
	isolateHomes(t)
	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	seedMigrationFixtures(t, d)
	d.Close()

	if err := runMigrateWatcherAt(dbPath, true); err != nil {
		t.Fatalf("first runMigrateWatcherAt failed: %v", err)
	}

	// After the first run the legacy tables are dropped, so schema-based
	// detection reports "already migrated" and the second run returns nil
	// (nothing to do) without re-migrating.
	if err := runMigrateWatcherAt(dbPath, true); err != nil {
		t.Fatalf("second runMigrateWatcherAt returned error, want nil (already migrated): %v", err)
	}

	d2, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer d2.Close()
	count := countRows(t, d2, "watcher_events")
	if count != 2 {
		t.Errorf("watcher_events count after double command-run = %d, want 2 (no double-insert)", count)
	}
	// The first run must have dropped the legacy tables.
	if legacyTablesExist(t, d2) {
		t.Errorf("legacy tables still exist after command run, want dropped")
	}
}

// TestRunMigrateWatcherAtFinishesPrior2bMigration exercises the full command
// path for the Option-B finish case: a DB whose data was already copied into
// watcher_* AND whose watcher_migrated marker is set (a 2b-migrated DB with the
// legacy tables retained). Running the command must drop the legacy tables
// WITHOUT re-copying, resolving the upgrade lockout.
func TestRunMigrateWatcherAtFinishesPrior2bMigration(t *testing.T) {
	isolateHomes(t)
	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	seedMigrationFixtures(t, d)

	// Simulate 2b: copy data into watcher_*, set the marker, but RETAIN the
	// legacy tables (recreate them after the copy dropped them).
	if _, err := migrateWatcherData(d, false); err != nil {
		t.Fatalf("simulate 2b copy: %v", err)
	}
	createLegacyTables(t, d.Conn())
	if _, err := d.Conn().Exec(`
		CREATE TABLE IF NOT EXISTS handler_meta (key TEXT PRIMARY KEY, value TEXT);
		INSERT INTO handler_meta (key, value) VALUES ('watcher_migrated', '1')
		ON CONFLICT(key) DO UPDATE SET value = excluded.value;
	`); err != nil {
		t.Fatalf("set 2b marker: %v", err)
	}
	before := countRows(t, d, "watcher_events")
	beforeSubs := countRows(t, d, "watcher_subscriptions")

	// The simulated 2b run (migrateWatcherData, called directly above) copied
	// data into watcher_* but never purged the legacy events table — that's
	// exactly the state a real 2b-migrated DB is in. Confirm the github/jira
	// duplicate rows are still present in `events` going into the finish run.
	if got := countEventsBySource(t, d, "github"); got != 1 {
		t.Fatalf("precondition: events(source=github) = %d before finish run, want 1 (unpurged 2b duplicate)", got)
	}
	if got := countEventsBySource(t, d, "jira"); got != 1 {
		t.Fatalf("precondition: events(source=jira) = %d before finish run, want 1 (unpurged 2b duplicate)", got)
	}
	d.Close()

	// Before the finish run, the retained tables would lock the user out.
	if err := runMigrateWatcherAt(dbPath, true); err != nil {
		t.Fatalf("finish-run runMigrateWatcherAt failed: %v", err)
	}

	d2, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer d2.Close()
	if got := countRows(t, d2, "watcher_events"); got != before {
		t.Errorf("watcher_events = %d after finish run, want %d (no re-copy)", got, before)
	}
	if got := countRows(t, d2, "watcher_subscriptions"); got != beforeSubs {
		t.Errorf("watcher_subscriptions = %d after finish run, want %d (no re-copy)", got, beforeSubs)
	}
	if legacyTablesExist(t, d2) {
		t.Errorf("legacy tables still exist after finish run, want dropped")
	}
	if d2.HasUnmigratedLegacyData() {
		t.Errorf("HasUnmigratedLegacyData() = true after finish run, want false (lockout resolved)")
	}

	// CRITICAL: a 2b-migrated DB (finish path) still has the duplicate
	// github/jira rows in the legacy events table, and the finish run must
	// purge them too — otherwise they linger forever since reads now use
	// watcher_events exclusively.
	if got := countEventsBySource(t, d2, "github"); got != 0 {
		t.Errorf("events(source=github) = %d after finish run, want 0 (purged)", got)
	}
	if got := countEventsBySource(t, d2, "jira"); got != 0 {
		t.Errorf("events(source=jira) = %d after finish run, want 0 (purged)", got)
	}
	if got := countEventsBySource(t, d2, "agent"); got != 1 {
		t.Errorf("events(source=agent) = %d after finish run, want 1 (agent events must NOT be purged)", got)
	}
	// No double-insert into watcher_events from the purge/finish run.
	if got := countRows(t, d2, "watcher_events"); got != before {
		t.Errorf("watcher_events = %d after finish-run purge, want %d (untouched)", got, before)
	}
}

// The pre-migration backup must be a consistent snapshot even when committed
// data still lives in the WAL sidecar (not yet checkpointed into the main .db).
// backupDBFile copies only the main .db file, so runMigrateWatcherAt must
// checkpoint the WAL first. This test keeps a connection open (so the WAL is not
// auto-checkpointed on close) through the migration, then opens the BACKUP file
// fresh and asserts the seeded legacy rows are present in it.
func TestRunMigrateWatcherAtBackupCapturesWALData(t *testing.T) {
	isolateHomes(t)
	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	seedMigrationFixtures(t, d)
	// Deliberately do NOT close d before migrating: on a WAL database, Close
	// would checkpoint and mask the bug. Keep it open so the seeded rows are
	// (potentially) still WAL-resident when backupDBFile copies the main .db.
	t.Cleanup(func() { d.Close() })

	subsBefore := countRows(t, d, "subscriptions")
	if subsBefore == 0 {
		t.Fatal("fixture seeded 0 subscriptions; test cannot detect WAL loss")
	}

	if err := runMigrateWatcherAt(dbPath, true); err != nil {
		t.Fatalf("runMigrateWatcherAt failed: %v", err)
	}

	matches, err := filepath.Glob(dbPath + ".backup-*")
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected exactly 1 backup file, got %v (err %v)", matches, err)
	}

	// Open the backup file directly (read-only, its own connection) and confirm
	// the seeded legacy subscriptions made it into the copied main .db.
	bconn, err := sql.Open("sqlite", matches[0])
	if err != nil {
		t.Fatalf("open backup db: %v", err)
	}
	defer bconn.Close()
	var subsInBackup int
	if err := bconn.QueryRow(`SELECT COUNT(*) FROM subscriptions`).Scan(&subsInBackup); err != nil {
		t.Fatalf("count subscriptions in backup: %v", err)
	}
	if subsInBackup != subsBefore {
		t.Fatalf("backup has %d subscriptions, want %d — WAL data was not captured in the backup", subsInBackup, subsBefore)
	}
}

func TestRunMigrateWatcherAtCreatesBackup(t *testing.T) {
	isolateHomes(t)
	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	seedMigrationFixtures(t, d)
	d.Close()

	if err := runMigrateWatcherAt(dbPath, true); err != nil {
		t.Fatalf("runMigrateWatcherAt failed: %v", err)
	}

	matches, err := filepath.Glob(dbPath + ".backup-*")
	if err != nil {
		t.Fatalf("glob backups: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("found %d backup files, want 1: %v", len(matches), matches)
	}
	info, err := os.Stat(matches[0])
	if err != nil {
		t.Fatalf("stat backup file: %v", err)
	}
	if info.Size() == 0 {
		t.Errorf("backup file %s is empty", matches[0])
	}
}
