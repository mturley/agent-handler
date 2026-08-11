package cmd

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	watcherPkg "github.com/mturley/agent-handler/watcher"
	wcfg "github.com/mturley/watcher/config"
	wdb "github.com/mturley/watcher/db"
	"gopkg.in/yaml.v3"
)

// migrateSkipRunningCheck bypasses the "is a watcher scheduler running"
// guard in runMigrateWatcher. It exists ONLY so tests can exercise the
// command path without depending on launchd/cron state; it is not meant to
// be used by humans running the real migration, so the flag is hidden.
var migrateSkipRunningCheck bool

// MigrationReport holds the before/after row counts for the data migration,
// one field per target watcher_* table populated by migrateWatcherData.
type MigrationReport struct {
	Events         int
	EventResources int
	ResourceState  int
	Subscriptions  int
	Relationships  int
	PollerStatus   int
}

// dropLegacyTablesSQL drops the four legacy watcher tables. DROP TABLE also
// removes each table's associated indexes in SQLite, so no separate DROP INDEX
// is needed. Ordering is irrelevant (no FKs between them).
const dropLegacyTablesSQL = `
	DROP TABLE IF EXISTS subscriptions;
	DROP TABLE IF EXISTS resource_state;
	DROP TABLE IF EXISTS resource_relationships;
	DROP TABLE IF EXISTS watcher_status;
`

// migrateWatcherData performs the structural legacy-table migration inside a
// single transaction, implementing the three-state lifecycle (see
// runMigrateWatcherAt for the caller-side detection):
//
//   - full migration (legacy tables present, watcher_migrated marker UNSET):
//     copy handler's legacy events/subscriptions/resource data into the
//     watcher library's watcher_* tables (preserving ids/timestamps), then DROP
//     the four legacy tables.
//   - finish a prior 2b migration (legacy tables present, marker SET): the data
//     was already copied into watcher_* by Phase 2b, so re-copying would
//     double-insert. SKIP the copy and only DROP the legacy tables to complete
//     the structural cleanup.
//
// The DROP runs in the same transaction as the copy so a DB is never left with
// data copied but tables un-dropped (or vice versa). It does not touch config,
// backups, or the running-watcher guard — those live in runMigrateWatcherAt.
//
// `finishOnly` selects the finish path (marker was set); the caller determines
// it. A report with zero copy counts is returned on the finish path.
func migrateWatcherData(d *db.DB, finishOnly bool) (MigrationReport, error) {
	var report MigrationReport

	conn := d.Conn()

	// Ensure target tables exist before the copy — idempotent, and cheap
	// even though db.Open already calls this on every open.
	if err := wdb.Migrate(conn); err != nil {
		return report, fmt.Errorf("failed to ensure watcher tables exist: %w", err)
	}

	// Bring a pre-2c subscriptions table current (adds unsubscribed_by if
	// missing) before the copy reads that column. No-op on the finish path or
	// when the column already exists. Run outside the write transaction because
	// ALTER TABLE issues its own schema change.
	if err := db.EnsureLegacySubscriptionsColumn(conn); err != nil {
		return report, fmt.Errorf("failed to ensure legacy subscriptions column: %w", err)
	}

	tx, err := conn.Begin()
	if err != nil {
		return report, fmt.Errorf("failed to start migration transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful Commit

	if !finishOnly {
		if err := copyLegacyDataTx(tx, &report); err != nil {
			return report, err
		}
	}

	// Structural cleanup: drop the legacy tables now that their data lives in
	// the watcher_* tables. On the finish path this is the only work; on the
	// full path it commits atomically with the copy above.
	if _, err := tx.Exec(dropLegacyTablesSQL); err != nil {
		return report, fmt.Errorf("failed to drop legacy tables: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return report, fmt.Errorf("failed to commit migration transaction: %w", err)
	}

	return report, nil
}

// copyLegacyDataTx copies the legacy handler tables into the watcher library's
// watcher_* tables within the given transaction, filling in `report`'s per-table
// counts. Extracted from migrateWatcherData so the finish path can skip it.
func copyLegacyDataTx(tx *sql.Tx, report *MigrationReport) error {
	// 1. events -> watcher_events (github/jira sourced rows only; agent and
	// handler-own events stay in the legacy table).
	res, err := tx.Exec(`
		INSERT INTO watcher_events (id, ts, external_ts, source, type, title, body, author, author_type, tags)
		SELECT id, ts, external_ts, source, type, title, body, author, author_type, tags
		FROM events WHERE source IN ('github', 'jira')
	`)
	if err != nil {
		return fmt.Errorf("failed to copy events: %w", err)
	}
	report.Events = int(mustRowsAffected(res))

	// 2. event_resources -> watcher_event_resources, scoped to the events
	// just migrated.
	res, err = tx.Exec(`
		INSERT INTO watcher_event_resources (event_id, resource_type, resource_id, resource_url)
		SELECT er.event_id, er.resource_type, er.resource_id, er.resource_url
		FROM event_resources er
		JOIN events e ON er.event_id = e.id
		WHERE e.source IN ('github', 'jira')
	`)
	if err != nil {
		return fmt.Errorf("failed to copy event_resources: %w", err)
	}
	report.EventResources = int(mustRowsAffected(res))

	// 3. resource_state -> watcher_resource_state (direct copy, identical columns).
	res, err = tx.Exec(`
		INSERT INTO watcher_resource_state (resource_type, resource_id, state_json, resource_updated_at, watcher_updated_at)
		SELECT resource_type, resource_id, state_json, resource_updated_at, watcher_updated_at
		FROM resource_state
	`)
	if err != nil {
		return fmt.Errorf("failed to copy resource_state: %w", err)
	}
	report.ResourceState = int(mustRowsAffected(res))

	// 4. resource_relationships -> watcher_resource_relationships (direct copy).
	res, err = tx.Exec(`
		INSERT INTO watcher_resource_relationships (id, child_type, child_id, child_url, parent_type, parent_id, parent_url, relationship, source, created_at)
		SELECT id, child_type, child_id, child_url, parent_type, parent_id, parent_url, relationship, source, created_at
		FROM resource_relationships
	`)
	if err != nil {
		return fmt.Errorf("failed to copy resource_relationships: %w", err)
	}
	report.Relationships = int(mustRowsAffected(res))

	// 5. handler's legacy watcher_status -> watcher_poller_status (direct copy).
	res, err = tx.Exec(`
		INSERT INTO watcher_poller_status (name, last_success, last_error, last_error_message)
		SELECT name, last_success, last_error, last_error_message
		FROM watcher_status
	`)
	if err != nil {
		return fmt.Errorf("failed to copy watcher_status: %w", err)
	}
	report.PollerStatus = int(mustRowsAffected(res))

	// 6. subscriptions -> watcher_subscriptions. The subscriber prefix
	// literal below MUST match handlerSubscriber's subscriberPrefix in
	// db/watcher_bridge.go — that unexported helper is the source of truth
	// for the prefix; this SQL can't call it directly, so keep them in sync
	// by hand if that prefix ever changes.
	//
	// expires_at: the watcher library treats a NULL expires_at as a
	// PERMANENT lease (see db/subscriptions.go's live-subscription
	// predicates: `expires_at IS NULL OR expires_at > ?`), not as expired.
	// So an active session's subscriptions get a fresh now+5d lease, while a
	// non-active session's subscriptions must be stamped with an
	// already-elapsed timestamp (now) so they immediately read as expired —
	// writing NULL there would do the opposite of the intent and leave
	// long-dead sessions' subscriptions polling forever.
	now := time.Now().UTC()
	expiresActive := now.Add(sessionLeaseTTL).Format(time.RFC3339)
	nowExpired := now.Format(time.RFC3339)
	res, err = tx.Exec(`
		INSERT INTO watcher_subscriptions (id, subscriber, resource_type, resource_id, resource_url, created_at, expires_at, backfill, deleted_at, unsubscribed_by_user)
		SELECT
			s.id,
			'handler:session:' || s.session_id,
			s.resource_type,
			s.resource_id,
			s.resource_url,
			s.created_at,
			CASE WHEN sess.status = 'active' THEN ? ELSE ? END,
			0,
			s.deleted_at,
			CASE WHEN s.unsubscribed_by = 'user' THEN 1 ELSE 0 END
		FROM subscriptions s
		LEFT JOIN sessions sess ON sess.session_id = s.session_id
	`, expiresActive, nowExpired)
	if err != nil {
		return fmt.Errorf("failed to copy subscriptions: %w", err)
	}
	report.Subscriptions = int(mustRowsAffected(res))

	return nil
}

// purgeMigratedEvents deletes the github/jira-sourced rows from the legacy
// `events` table (and their event_resources) now that the same data lives in
// watcher_events/watcher_event_resources (copied by copyLegacyDataTx on a full
// migration, or already copied by a prior Phase 2b run on the finish path).
// Agent/handler-sourced events are untouched — only source IN ('github',
// 'jira') rows are removed. `events` itself is a CURRENT table (shared with
// agent/handler events), not one of the legacy tables dropped by
// migrateWatcherData, so this purge is independent of and runs AFTER that
// transaction commits: a copy failure aborts before this ever runs, so the
// pre-migration backup is never the only way back, and a purge failure here
// can't undo an already-successful copy.
func purgeMigratedEvents(conn *sql.DB) (int64, error) {
	if _, err := conn.Exec(`
		DELETE FROM event_resources
		WHERE event_id IN (SELECT id FROM events WHERE source IN ('github', 'jira'))
	`); err != nil {
		return 0, fmt.Errorf("failed to purge migrated event_resources: %w", err)
	}
	res, err := conn.Exec(`DELETE FROM events WHERE source IN ('github', 'jira')`)
	if err != nil {
		return 0, fmt.Errorf("failed to purge migrated events: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, nil //nolint:nilerr // best-effort row count; the deletes above already succeeded
	}
	return n, nil
}

// purgePostMigration calls purgeMigratedEvents best-effort, on both the full
// and finish migration paths: the copy has already succeeded (or, on the
// finish path, was already done by a prior Phase 2b run), so a purge failure
// here should WARN rather than fail the whole migration — the leftover
// github/jira rows in `events` are harmless duplicates (reads use
// watcher_events now), and the pre-migration backup already covers rollback.
func purgePostMigration(d *db.DB) {
	n, err := purgeMigratedEvents(d.Conn())
	if err != nil {
		fmt.Printf("\nwarning: failed to purge migrated events from the legacy events table: %v\n", err)
		fmt.Println("  (harmless duplicates; safe to ignore, or purge manually later)")
		return
	}
	fmt.Printf("\nPurged %d migrated github/jira rows from the legacy events table.\n", n)
}

// mustRowsAffected returns res.RowsAffected(), treating a driver error as 0.
// SQLite via modernc.org/sqlite always supports RowsAffected, so this is a
// defensive fallback rather than an expected path.
func mustRowsAffected(res sql.Result) int64 {
	n, err := res.RowsAffected()
	if err != nil {
		return 0
	}
	return n
}

// sessionLeaseTTL note: sessionLeaseTTL is also defined in package db
// (db/watcher_bridge.go) and is not exported, so it is redeclared here for the
// one place migrate needs it. Keep in sync with db.sessionLeaseTTL (5 days) if
// that ever changes.
const sessionLeaseTTL = 5 * 24 * time.Hour

// legacyDBError is the message shown when `handler setup` is run against a
// database that predates the watcher-library migration and hasn't been
// migrated yet. Kept as a constant so the guard and its test assert the same
// text.
const legacyDBError = "Legacy database found. Handler has made breaking changes to its schema as part of adopting a watcher library. Run handler setup --migrate-watcher to perform a migration as part of this update. Handler will not work correctly until you either do this or delete your database and run handler setup again."

// guardLegacyDatabase refuses a normal `handler setup` when the real handler
// database holds unmigrated legacy watcher data, directing the user to run the
// migration (or delete the database) first. `handler setup --migrate-watcher`
// bypasses this (it returns before this is called) since it IS the remedy.
func guardLegacyDatabase() error {
	return guardLegacyDatabaseAt(db.DefaultPath())
}

// guardLegacyDatabaseAt is the testable form of guardLegacyDatabase, pointed at
// an explicit database path. It returns an error carrying legacyDBError when the
// database at dbPath has unmigrated legacy data, and nil otherwise (including
// when the database can't be opened — setup will surface that failure itself
// when it opens the database for real).
func guardLegacyDatabaseAt(dbPath string) error {
	d, err := db.Open(dbPath)
	if err != nil {
		return nil
	}
	defer d.Close()

	if d.HasUnmigratedLegacyData() {
		return fmt.Errorf("%s", legacyDBError)
	}
	return nil
}

// runMigrateWatcher runs the production data migration against the real
// handler database at db.DefaultPath(). It is the entry point wired to
// `handler setup --migrate-watcher`.
func runMigrateWatcher() error {
	return runMigrateWatcherAt(db.DefaultPath(), migrateSkipRunningCheck)
}

// runMigrateWatcherAt runs the full migration command (guards, backup,
// pre-counts, data copy, report, best-effort credential migration) against
// the database at dbPath. Factored out from runMigrateWatcher so tests can
// point it at a temp database instead of the real one.
func runMigrateWatcherAt(dbPath string, skipRunningCheck bool) error {
	d, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database at %s: %w", dbPath, err)
	}
	defer d.Close()

	// Three-state, schema-based lifecycle (see the CRITICAL migration-path
	// section of the Phase 2c plan and db/legacy_guard.go):
	//   - legacy tables ABSENT           -> already migrated, nothing to do.
	//   - tables PRESENT + marker SET     -> a 2b-migrated DB: data already lives
	//                                        in watcher_*, so finish the cleanup
	//                                        by dropping the legacy tables only.
	//   - tables PRESENT + marker UNSET   -> full migration (copy + drop).
	// Detection is purely structural; the marker is read ONLY to choose
	// copy-vs-skip, never to gate anything else (it is otherwise retired).
	if !d.HasUnmigratedLegacyData() {
		fmt.Println("watcher data already migrated (nothing to do)")
		return nil
	}
	finishOnly := d.WatcherMigrationDone()

	if !skipRunningCheck {
		if watcherPkg.IsRunning("github") || watcherPkg.IsRunning("jira") {
			return fmt.Errorf("a watcher scheduler is running; run 'handler watcher stop' before migrating")
		}
	}

	backupPath, err := backupDBFile(dbPath)
	if err != nil {
		return fmt.Errorf("failed to back up database before migrating: %w", err)
	}
	fmt.Printf("Backed up database to %s\n", backupPath)

	if finishOnly {
		fmt.Println("\nDetected a prior (Phase 2b) migration: data already copied into watcher_* tables.")
		fmt.Println("Finishing migration by dropping the retained legacy tables (no data is re-copied).")
		if _, err := migrateWatcherData(d, true); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
		fmt.Println("Dropped legacy tables (subscriptions, resource_state, resource_relationships, watcher_status).")
		// A 2b-migrated DB still has the duplicate github/jira rows in the
		// legacy `events` table (2b copied but never purged them) — purge them
		// now so they don't linger forever.
		purgePostMigration(d)
	} else {
		preCounts, err := sourceRowCounts(d)
		if err != nil {
			return fmt.Errorf("failed to read pre-migration row counts: %w", err)
		}
		fmt.Println("\nSource row counts:")
		fmt.Printf("  events (github/jira):     %d\n", preCounts.Events)
		fmt.Printf("  event_resources:          %d\n", preCounts.EventResources)
		fmt.Printf("  resource_state:           %d\n", preCounts.ResourceState)
		fmt.Printf("  subscriptions:            %d\n", preCounts.Subscriptions)
		fmt.Printf("  resource_relationships:   %d\n", preCounts.Relationships)
		fmt.Printf("  watcher_status:           %d\n", preCounts.PollerStatus)

		report, err := migrateWatcherData(d, false)
		if err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}

		fmt.Println("\nMigration report (rows inserted vs. source):")
		printCompare("events -> watcher_events", report.Events, preCounts.Events)
		printCompare("event_resources -> watcher_event_resources", report.EventResources, preCounts.EventResources)
		printCompare("resource_state -> watcher_resource_state", report.ResourceState, preCounts.ResourceState)
		printCompare("subscriptions -> watcher_subscriptions", report.Subscriptions, preCounts.Subscriptions)
		printCompare("resource_relationships -> watcher_resource_relationships", report.Relationships, preCounts.Relationships)
		printCompare("watcher_status -> watcher_poller_status", report.PollerStatus, preCounts.PollerStatus)
		fmt.Println("\nDropped legacy tables (subscriptions, resource_state, resource_relationships, watcher_status).")
		purgePostMigration(d)
	}

	migrateCredentials()

	fmt.Println("\nBehavior config migration (best-effort):")
	if hcfg, err := config.Read(config.DefaultPath()); err != nil {
		fmt.Printf("  note: could not read handler config (%v); run 'handler watcher auth' manually if needed.\n", err)
	} else {
		migrateBehaviorConfig(hcfg)
	}

	fmt.Println("\nNext steps:")
	fmt.Println("  1. Verify with 'handler health', 'handler watching', and 'handler status'.")
	fmt.Println("  2. Run 'handler watcher start' to resume the github/jira watchers.")

	return nil
}

// printCompare prints a PASS/FAIL line comparing rows inserted into a target
// table against the matching source-table count.
func printCompare(label string, inserted, source int) {
	status := "PASS"
	if inserted != source {
		status = "FAIL"
	}
	fmt.Printf("  [%s] %-58s inserted=%d source=%d\n", status, label, inserted, source)
}

// sourceCounts holds pre-migration row counts read from the legacy handler
// tables, for the printed PASS/FAIL comparison against MigrationReport
// after the copy.
type sourceCounts struct {
	Events         int
	EventResources int
	ResourceState  int
	Subscriptions  int
	Relationships  int
	PollerStatus   int
}

func sourceRowCounts(d *db.DB) (sourceCounts, error) {
	var c sourceCounts
	conn := d.Conn()
	queries := []struct {
		query string
		dst   *int
	}{
		{`SELECT COUNT(*) FROM events WHERE source IN ('github', 'jira')`, &c.Events},
		{`SELECT COUNT(*) FROM event_resources er JOIN events e ON er.event_id = e.id WHERE e.source IN ('github', 'jira')`, &c.EventResources},
		{`SELECT COUNT(*) FROM resource_state`, &c.ResourceState},
		{`SELECT COUNT(*) FROM subscriptions`, &c.Subscriptions},
		{`SELECT COUNT(*) FROM resource_relationships`, &c.Relationships},
		{`SELECT COUNT(*) FROM watcher_status`, &c.PollerStatus},
	}
	for _, q := range queries {
		if err := conn.QueryRow(q.query).Scan(q.dst); err != nil {
			return c, fmt.Errorf("query %q: %w", q.query, err)
		}
	}
	return c, nil
}

// backupDBFile copies the database file at dbPath to a sibling
// "<dbpath>.backup-<UTC timestamp>" file, returning the backup's path. It
// aborts with an error on any copy failure rather than leaving a partial
// backup behind.
func backupDBFile(dbPath string) (string, error) {
	backupPath := dbPath + ".backup-" + time.Now().UTC().Format("20060102T150405Z")

	src, err := os.Open(dbPath)
	if err != nil {
		return "", fmt.Errorf("failed to open %s for backup: %w", dbPath, err)
	}
	defer src.Close()

	dst, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", fmt.Errorf("failed to create backup file %s: %w", backupPath, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("failed to copy %s to %s: %w", dbPath, backupPath, err)
	}

	return backupPath, nil
}

// migrateCredentials best-effort copies GitHub/Jira credentials from
// handler's own config.yaml into the watcher library's auth.yaml, for any
// service present in the handler config but not yet configured in
// auth.yaml. It never blocks or fails the migration: on any error it prints
// a note telling the human to run 'handler watcher auth' manually.
//
// The two config shapes differ and are NOT struct-compatible:
//   - GitHub is the same shape on both sides (Token).
//   - handler Jira has URL; watcher Jira has Host (same value, different
//     field/yaml key).
//   - handler's Jira.CustomFields and Jira.BotUsernames are NOT copied to
//     auth.yaml here — both are behavior settings, not credentials, and are
//     seeded into the watcher library's separate config.yaml instead, by
//     migrateBehaviorConfig (called from runMigrateWatcherAt alongside this
//     function).
//   - handler has no Slack config; Slack is skipped entirely.
func migrateCredentials() {
	fmt.Println("\nCredential migration (best-effort):")

	hcfg, err := config.Read(config.DefaultPath())
	if err != nil {
		fmt.Printf("  note: could not read handler config (%v); run 'handler watcher auth' manually if needed.\n", err)
		return
	}

	authPath := wcfg.DefaultPath()
	acfg, err := wcfg.Load(authPath)
	if err != nil {
		fmt.Printf("  note: could not load watcher auth.yaml (%v); run 'handler watcher auth' manually if needed.\n", err)
		return
	}

	changed := false

	if hcfg.Services.GitHub != nil && hcfg.Services.GitHub.Token != "" {
		if acfg.Services.GitHub == nil || acfg.Services.GitHub.Token == "" {
			acfg.Services.GitHub = &wcfg.GitHubConfig{Token: hcfg.Services.GitHub.Token}
			changed = true
			fmt.Println("  copied GitHub credentials into auth.yaml")
		} else {
			fmt.Println("  GitHub already configured in auth.yaml; skipped")
		}
	}

	if hcfg.Services.Jira != nil && hcfg.Services.Jira.Token != "" {
		if acfg.Services.Jira == nil || acfg.Services.Jira.Token == "" {
			acfg.Services.Jira = &wcfg.JiraConfig{
				Host:  hcfg.Services.Jira.URL,
				Email: hcfg.Services.Jira.Email,
				Token: hcfg.Services.Jira.Token,
			}
			changed = true
			fmt.Println("  copied Jira credentials into auth.yaml")
		} else {
			fmt.Println("  Jira already configured in auth.yaml; skipped")
		}
	}

	if !changed {
		fmt.Println("  no credentials to copy")
		return
	}

	if err := acfg.Save(authPath); err != nil {
		fmt.Printf("  note: failed to save auth.yaml (%v); run 'handler watcher auth' manually.\n", err)
	}
}

// migrateBehaviorConfig best-effort seeds Jira behavior settings (custom
// fields, bot usernames) from handler's own config.yaml into the watcher
// library's separate behavior config file ("config.yaml"), which is
// distinct from the credentials-holding auth.yaml handled by
// migrateCredentials above. It never blocks or fails the migration.
//
// It only fills in values that config.yaml doesn't already have — an
// existing config.yaml entry always wins, so re-running migration (or a
// human having already customized config.yaml) is never clobbered.
func migrateBehaviorConfig(hcfg *config.Config) {
	if hcfg.Services.Jira == nil {
		fmt.Println("  no Jira behavior settings to migrate")
		return
	}

	behaviorPath := wcfg.ConfigDefaultPath()
	bcfg, err := wcfg.LoadConfig(behaviorPath)
	if err != nil {
		fmt.Printf("  note: could not load watcher config.yaml (%v); run 'handler watcher auth' manually if needed.\n", err)
		return
	}

	if bcfg.Jira == nil {
		bcfg.Jira = &wcfg.JiraBehavior{}
	}

	changed := false

	if len(hcfg.Services.Jira.CustomFields) > 0 && len(bcfg.Jira.CustomFields) == 0 {
		bcfg.Jira.CustomFields = hcfg.Services.Jira.CustomFields
		changed = true
		fmt.Println("  copied Jira custom_fields into config.yaml")
	}

	if len(hcfg.Services.Jira.BotUsernames) > 0 && len(bcfg.Jira.BotUsernames) == 0 {
		bcfg.Jira.BotUsernames = hcfg.Services.Jira.BotUsernames
		changed = true
		fmt.Println("  copied Jira bot_usernames into config.yaml")
	}

	if !changed {
		fmt.Println("  no behavior settings to copy")
		return
	}

	if err := writeWatcherBehaviorConfig(bcfg); err != nil {
		fmt.Printf("  note: failed to save config.yaml (%v); run 'handler watcher auth' manually.\n", err)
	}
}

// writeWatcherBehaviorConfig marshals cf to YAML and writes it to the
// watcher library's behavior config path (wcfg.ConfigDefaultPath()),
// creating the parent directory if needed. The watcher library has no
// exported writer for *wcfg.ConfigFile (only LoadConfig), so this does the
// marshal/write itself. config.yaml holds no credentials, so it is written
// with 0o644 permissions (matching the library's convention of not
// enforcing perms on this file, unlike auth.yaml's 0o600).
//
// This mirrors writeWatcherBehaviorConfig in cmd/watcher/auth.go — the two
// call sites live in different packages (cmd vs. cmd/watcher) and the
// watcher library itself isn't a place to add this, so the small helper is
// duplicated rather than factored into a new shared package.
func writeWatcherBehaviorConfig(cf *wcfg.ConfigFile) error {
	path := wcfg.ConfigDefaultPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}
	data, err := yaml.Marshal(cf)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write config %s: %w", path, err)
	}
	return nil
}

// init registers the --skip-running-check flag on setupCmd, next to this
// feature's implementation. The --migrate-watcher flag itself is registered
// in setup.go's own init() alongside setupCmd's other flags.
func init() {
	setupCmd.Flags().BoolVar(&migrateSkipRunningCheck, "skip-running-check", false, "internal: bypass the running-watcher guard (tests only)")
	_ = setupCmd.Flags().MarkHidden("skip-running-check")
}
