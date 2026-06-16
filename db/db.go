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

	return &DB{conn: conn}, nil
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

// DefaultPath returns the default database path: ~/.agent-handler/data/handler.db
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory if home cannot be determined
		return filepath.Join(".", ".agent-handler", "data", "handler.db")
	}
	return filepath.Join(home, ".agent-handler", "data", "handler.db")
}
