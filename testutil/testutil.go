package testutil

import (
	"os"
	"path/filepath"
	"testing"
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
// This is a placeholder stub - will be implemented in Task 2 when db package exists.
func TempDB(t *testing.T) (dbPath string) {
	t.Helper()
	dir := HandlerDir(t)
	dbPath = filepath.Join(dir, "test.db")
	// TODO: Initialize database when db package is available
	return dbPath
}
