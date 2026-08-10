package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mturley/agent-handler/db"
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

// seedMigrationFixtures populates the legacy handler tables with a small,
// deterministic dataset covering every mapping the migration performs:
// sessions (active + archived), github/jira/agent events (+resources),
// resource_state, subscriptions (active/normal, user-unsubscribed+deleted),
// resource_relationships, and the legacy watcher_status table.
func seedMigrationFixtures(t *testing.T, d *db.DB) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	conn := d.Conn()

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

	report, err := migrateWatcherData(d)
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

	if !d.WatcherMigrationDone() {
		t.Errorf("d.WatcherMigrationDone() = false, want true after migration")
	}
}

func TestMigrateWatcherDataRefusesDoubleRun(t *testing.T) {
	d := testMigrateDB(t)
	seedMigrationFixtures(t, d)

	if _, err := migrateWatcherData(d); err != nil {
		t.Fatalf("first migrateWatcherData failed: %v", err)
	}

	before := countRows(t, d, "watcher_events")

	if _, err := migrateWatcherData(d); err == nil {
		t.Fatalf("second migrateWatcherData succeeded, want error (already migrated)")
	}

	after := countRows(t, d, "watcher_events")
	if before != after {
		t.Errorf("watcher_events count changed on refused re-run: before=%d after=%d", before, after)
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

	// Second run should detect the marker and return nil (nothing to do),
	// not attempt to re-migrate.
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
