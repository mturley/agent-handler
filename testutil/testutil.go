package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mturley/agent-handler/db"
)

// HandlerDir returns a temp directory for testing CLI state.
// The directory is created and automatically cleaned up after the test.
func HandlerDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "handler-test-*")
	if err != nil {
		t.Fatalf("failed to create temp handler dir: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(dir)
	})
	return dir
}

// TempDB creates a temporary SQLite database for testing.
// The database is properly initialized and automatically cleaned up after the test.
func TempDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "handler.db")
	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open temp db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}
