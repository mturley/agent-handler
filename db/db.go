package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

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

	return &DB{conn: conn}, nil
}

func runMigrations(conn *sql.DB) error {
	// Safety net for existing databases created before a table was added to
	// schema.sql. CREATE TABLE IF NOT EXISTS is idempotent.
	_, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS dismissed_events (
			session_id   TEXT NOT NULL,
			event_id     TEXT NOT NULL,
			dismissed_at TEXT NOT NULL,
			PRIMARY KEY (session_id, event_id)
		);
		CREATE INDEX IF NOT EXISTS idx_dismissed_events_session ON dismissed_events(session_id);
	`)
	if err != nil {
		return fmt.Errorf("failed to create dismissed_events table: %w", err)
	}

	// Add subscriptions.unsubscribed_by for existing databases. ALTER TABLE
	// ADD COLUMN is not idempotent, so only add it when absent.
	if err := addColumnIfMissing(conn, "subscriptions", "unsubscribed_by", "TEXT"); err != nil {
		return err
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
