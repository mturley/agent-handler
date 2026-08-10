package cmd

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	watcherPkg "github.com/mturley/agent-handler/watcher"
	wcfg "github.com/mturley/watcher/config"
	wdb "github.com/mturley/watcher/db"
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

// migrateWatcherData copies handler's legacy events/subscriptions/resource
// data into the watcher library's watcher_* tables, preserving original
// ids/timestamps, and sets the watcher_migrated marker on success. It is a
// pure data copy: no backup, no prompts, no "is a watcher running" guard —
// those live in runMigrateWatcherAt, which wraps this.
//
// It refuses to run (returning an error, without touching any table) if the
// marker is already set, so a caller can't accidentally double-insert by
// calling this directly outside the command's own marker check.
func migrateWatcherData(d *db.DB) (MigrationReport, error) {
	var report MigrationReport

	if d.WatcherMigrationDone() {
		return report, fmt.Errorf("watcher data already migrated; refusing to run migrateWatcherData again")
	}

	conn := d.Conn()

	// Ensure target tables exist before the copy — idempotent, and cheap
	// even though db.Open already calls this on every open.
	if err := wdb.Migrate(conn); err != nil {
		return report, fmt.Errorf("failed to ensure watcher tables exist: %w", err)
	}

	tx, err := conn.Begin()
	if err != nil {
		return report, fmt.Errorf("failed to start migration transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful Commit

	// 1. events -> watcher_events (github/jira sourced rows only; agent and
	// handler-own events stay in the legacy table).
	res, err := tx.Exec(`
		INSERT INTO watcher_events (id, ts, external_ts, source, type, title, body, author, author_type, tags)
		SELECT id, ts, external_ts, source, type, title, body, author, author_type, tags
		FROM events WHERE source IN ('github', 'jira')
	`)
	if err != nil {
		return report, fmt.Errorf("failed to copy events: %w", err)
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
		return report, fmt.Errorf("failed to copy event_resources: %w", err)
	}
	report.EventResources = int(mustRowsAffected(res))

	// 3. resource_state -> watcher_resource_state (direct copy, identical columns).
	res, err = tx.Exec(`
		INSERT INTO watcher_resource_state (resource_type, resource_id, state_json, resource_updated_at, watcher_updated_at)
		SELECT resource_type, resource_id, state_json, resource_updated_at, watcher_updated_at
		FROM resource_state
	`)
	if err != nil {
		return report, fmt.Errorf("failed to copy resource_state: %w", err)
	}
	report.ResourceState = int(mustRowsAffected(res))

	// 4. resource_relationships -> watcher_resource_relationships (direct copy).
	res, err = tx.Exec(`
		INSERT INTO watcher_resource_relationships (id, child_type, child_id, child_url, parent_type, parent_id, parent_url, relationship, source, created_at)
		SELECT id, child_type, child_id, child_url, parent_type, parent_id, parent_url, relationship, source, created_at
		FROM resource_relationships
	`)
	if err != nil {
		return report, fmt.Errorf("failed to copy resource_relationships: %w", err)
	}
	report.Relationships = int(mustRowsAffected(res))

	// 5. handler's legacy watcher_status -> watcher_poller_status (direct
	// copy; the original watcher_status table is left intact for rollback).
	res, err = tx.Exec(`
		INSERT INTO watcher_poller_status (name, last_success, last_error, last_error_message)
		SELECT name, last_success, last_error, last_error_message
		FROM watcher_status
	`)
	if err != nil {
		return report, fmt.Errorf("failed to copy watcher_status: %w", err)
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
		return report, fmt.Errorf("failed to copy subscriptions: %w", err)
	}
	report.Subscriptions = int(mustRowsAffected(res))

	// Set the marker inside the transaction so a half-migrated DB can never
	// have it set — either the whole copy + marker commits, or none of it
	// does. This can't call d.SetWatcherMigrated() (the exported wrapper
	// added in db/inbox_scope.go for this command): that method runs its
	// own statement against d.Conn() outside this transaction, which would
	// deadlock against the open write transaction below. Mirror its SQL
	// directly on tx instead.
	if _, err := tx.Exec(`
		INSERT INTO handler_meta (key, value)
		VALUES ('watcher_migrated', '1')
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`); err != nil {
		return report, fmt.Errorf("failed to set watcher_migrated marker: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return report, fmt.Errorf("failed to commit migration transaction: %w", err)
	}

	return report, nil
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

// sessionLeaseTTL note: db.SetWatcherMigrated and friends live in package
// db; sessionLeaseTTL itself is also defined there (db/watcher_bridge.go)
// and is not exported, so it is redeclared here for the one place migrate
// needs it. Keep in sync with db.sessionLeaseTTL (5 days) if that ever
// changes.
const sessionLeaseTTL = 5 * 24 * time.Hour

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

	if d.WatcherMigrationDone() {
		fmt.Println("watcher data already migrated (nothing to do)")
		return nil
	}

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

	report, err := migrateWatcherData(d)
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

	migrateCredentials()

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
//   - handler's per-service Jira.CustomFields moves to the watcher config's
//     top-level JiraCustomFields (not nested under JiraConfig).
//   - handler's Jira.BotUsernames is NOT copied here — it belongs in the
//     library's separate config.yaml (behavior config), handled by task 9c,
//     not in auth.yaml (credentials).
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
			if len(hcfg.Services.Jira.CustomFields) > 0 {
				acfg.JiraCustomFields = hcfg.Services.Jira.CustomFields
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

// init registers the --skip-running-check flag on setupCmd, next to this
// feature's implementation. The --migrate-watcher flag itself is registered
// in setup.go's own init() alongside setupCmd's other flags.
func init() {
	setupCmd.Flags().BoolVar(&migrateSkipRunningCheck, "skip-running-check", false, "internal: bypass the running-watcher guard (tests only)")
	_ = setupCmd.Flags().MarkHidden("skip-running-check")
}
