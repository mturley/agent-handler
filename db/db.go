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

// runMigrations applies schema migrations to existing databases.
// Uses addColumnIfMissing to safely add columns without failing on existing DBs.
func runMigrations(conn *sql.DB) error {
	// Add human_seen_ts column to session_cursors if it doesn't exist
	if err := addColumnIfMissing(conn, "session_cursors", "human_seen_ts", "TEXT"); err != nil {
		return err
	}
	// Add terminal_type column to sessions if it doesn't exist
	if err := addColumnIfMissing(conn, "sessions", "terminal_type", "TEXT"); err != nil {
		return err
	}
	// Add terminal_id column to sessions if it doesn't exist
	if err := addColumnIfMissing(conn, "sessions", "terminal_id", "TEXT"); err != nil {
		return err
	}
	// Add cmux_workspace_id column to sessions if it doesn't exist
	if err := addColumnIfMissing(conn, "sessions", "cmux_workspace_id", "TEXT"); err != nil {
		return err
	}
	return nil
}

// addColumnIfMissing adds a column to a table if it doesn't already exist.
func addColumnIfMissing(conn *sql.DB, table, column, colType string) error {
	var count int
	err := conn.QueryRow(
		fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = '%s'`, table, column),
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check for %s.%s column: %w", table, column, err)
	}
	if count == 0 {
		if _, err := conn.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, colType)); err != nil {
			return fmt.Errorf("failed to add %s.%s column: %w", table, column, err)
		}
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
