package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	wdb "github.com/mturley/watcher/db"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaDDL string

// DB wraps a SQLite database connection for agent-handler.
type DB struct {
	conn *sql.DB
}

// Open creates or opens the SQLite database at the given path.
// It creates parent directories if needed, applies WAL mode, and runs schema migrations.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}

	// Wait up to 3s for locks instead of failing immediately
	if _, err := conn.Exec("PRAGMA busy_timeout=3000"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	// Apply schema
	if _, err := conn.Exec(schemaDDL); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to apply schema: %w", err)
	}

	// Run migrations for existing databases
	if err := runMigrations(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// Create the watcher library's own tables (watcher_*). Disjoint from
	// handler's runMigrations above, which never touches watcher_* tables.
	if err := wdb.Migrate(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to run watcher migrate: %w", err)
	}

	return &DB{conn: conn}, nil
}

// runMigrations applies handler-owned schema migrations to the NEW (current)
// schema on every Open. It is intentionally minimal: the legacy watcher tables
// (subscriptions, resource_state, resource_relationships, watcher_status) and
// their historical migrations were removed in Phase 2c. Legacy schema is now
// touched only once, by `handler setup --migrate-watcher`, which owns the full
// legacy lifecycle (run legacy migrations -> copy into watcher_* -> drop the
// legacy tables). Add future NEW-schema migrations here as schema changes land.
//
// The handler_meta table (which held the retired watcher_migrated marker) is no
// longer created here: fresh installs never need it, and legacy-data detection
// is now schema-based (see db/legacy_guard.go). The marker itself is still read
// by the migration command to distinguish a 2b-migrated DB (marker set, tables
// retained) from an unmigrated one — but only where handler_meta already exists,
// so no creation is required.
func runMigrations(conn *sql.DB) error {
	// dismissed_events: per-session explicit event dismissals (current schema).
	// Also present in schema.sql; CREATE TABLE IF NOT EXISTS in both is
	// idempotent and matches the existing safety-net pattern for databases
	// created before a table was added to schema.sql.
	if _, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS dismissed_events (
			session_id   TEXT NOT NULL,
			event_id     TEXT NOT NULL,
			dismissed_at TEXT NOT NULL,
			PRIMARY KEY (session_id, event_id)
		);
		CREATE INDEX IF NOT EXISTS idx_dismissed_events_session ON dismissed_events(session_id);
	`); err != nil {
		return fmt.Errorf("failed to create dismissed_events table: %w", err)
	}

	// session_crons: per-session Claude Code cron jobs (see db/session_crons.go).
	// Also in schema.sql; CREATE TABLE IF NOT EXISTS in both is idempotent and
	// matches the existing safety-net pattern for pre-existing databases.
	if _, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS session_crons (
			session_id   TEXT NOT NULL,
			job_id       TEXT NOT NULL,
			schedule     TEXT NOT NULL,
			recurring    INTEGER NOT NULL DEFAULT 0,
			prompt       TEXT,
			created_at   TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			PRIMARY KEY (session_id, job_id)
		);
		CREATE INDEX IF NOT EXISTS idx_session_crons_session ON session_crons(session_id);
	`); err != nil {
		return fmt.Errorf("failed to create session_crons table: %w", err)
	}

	// session_rate_limits: per-session 5h rate limit state (see db/rate_limits.go).
	if _, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS session_rate_limits (
			session_id          TEXT PRIMARY KEY,
			five_hour_percent   REAL NOT NULL,
			five_hour_resets_at TEXT,
			updated_at          TEXT NOT NULL
		);
	`); err != nil {
		return fmt.Errorf("failed to create session_rate_limits table: %w", err)
	}

	// last_error_at records rate_limit StopFailures (see db/rate_limits.go).
	if err := addColumnIfMissing(conn, "session_rate_limits", "last_error_at", "TEXT"); err != nil {
		return err
	}

	// Status-emit reminder tracking (see cmd/status_reminder.go).
	if err := addColumnIfMissing(conn, "sessions", "prompts_since_status", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(conn, "sessions", "status_baseline_at", "TEXT"); err != nil {
		return err
	}

	// cost_epoch_state replaces cost_snapshots (epoch-anchored cost tracking).
	// Create it for existing databases and drop the obsolete snapshot table.
	if _, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS cost_epoch_state (
			session_id TEXT PRIMARY KEY REFERENCES sessions(session_id),
			pid INTEGER NOT NULL,
			last_observed_cost REAL NOT NULL,
			last_observed_input INTEGER NOT NULL,
			last_observed_output INTEGER NOT NULL,
			model TEXT,
			updated_at TEXT NOT NULL
		);
	`); err != nil {
		return fmt.Errorf("failed to create cost_epoch_state table: %w", err)
	}
	// Detect whether this Open is the first one after upgrading from the old
	// cost_snapshots schema, BEFORE dropping cost_snapshots below. This lets us
	// gate the one-time daily_cost wipe so it doesn't run on every Open().
	var oldSchemaTableCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='cost_snapshots'`).Scan(&oldSchemaTableCount); err != nil {
		return fmt.Errorf("failed to check for cost_snapshots table: %w", err)
	}

	if _, err := conn.Exec(`DROP TABLE IF EXISTS cost_snapshots`); err != nil {
		return fmt.Errorf("failed to drop cost_snapshots table: %w", err)
	}
	if _, err := conn.Exec(`DROP TABLE IF EXISTS cost_adjustments`); err != nil {
		return fmt.Errorf("failed to drop cost_adjustments table: %w", err)
	}
	// Wipe daily_cost ONCE, only during the cutover from the old schema:
	// existing rows mix UTC dates with values from the old buggy delta logic.
	// Rebuilds cleanly under epoch-anchored tracking. On a brand-new DB or any
	// subsequent Open() (cost_snapshots no longer exists), this must NOT run,
	// or accrued cost would be wiped on every CLI invocation / statusline tick.
	if oldSchemaTableCount > 0 {
		if _, err := conn.Exec(`DELETE FROM daily_cost`); err != nil {
			return fmt.Errorf("failed to wipe daily_cost: %w", err)
		}
	}

	return nil
}

// addColumnIfMissing adds a column to a table only if it doesn't already exist.
func addColumnIfMissing(conn *sql.DB, table, column, colType string) error {
	rows, err := conn.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return fmt.Errorf("failed to inspect %s columns: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("failed to scan %s column: %w", table, err)
		}
		if name == column {
			return nil // already present
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating %s columns: %w", table, err)
	}
	if _, err := conn.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, colType)); err != nil {
		return fmt.Errorf("failed to add column %s.%s: %w", table, column, err)
	}
	return nil
}

// OpenReadOnly opens the database in read-only mode.
// The database must already exist.
func OpenReadOnly(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return &DB{conn: conn}, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	if db.conn != nil {
		return db.conn.Close()
	}
	return nil
}

// Conn exposes the raw SQL connection for ad-hoc queries.
func (db *DB) Conn() *sql.DB {
	return db.conn
}

// QueryRow executes a query that returns at most one row.
func (db *DB) QueryRow(query string, args ...interface{}) *sql.Row {
	return db.conn.QueryRow(query, args...)
}

// Exec executes a query that doesn't return rows.
func (db *DB) Exec(query string, args ...interface{}) (sql.Result, error) {
	return db.conn.Exec(query, args...)
}

// Query executes a query that returns rows.
func (db *DB) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return db.conn.Query(query, args...)
}

// HandlerHome returns the agent-handler home directory.
// Respects HANDLER_HOME env var, defaults to ~/.agent-handler.
func HandlerHome() string {
	if dir := os.Getenv("HANDLER_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".agent-handler")
	}
	return filepath.Join(home, ".agent-handler")
}

// DefaultPath returns the default database path: $HANDLER_HOME/data/handler.db
func DefaultPath() string {
	return filepath.Join(HandlerHome(), "data", "handler.db")
}
