package db

import (
	"path/filepath"
	"testing"
)

func TestOpen(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Open database
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	defer db.Close()

	// Verify WAL mode is set
	var journalMode string
	err = db.Conn().QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("failed to query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("expected journal_mode=wal, got %s", journalMode)
	}

	// Verify all 7 tables exist
	expectedTables := []string{
		"events",
		"event_recipients",
		"event_resources",
		"sessions",
		"session_cursors",
		"subscriptions",
		"resource_relationships",
	}

	for _, table := range expectedTables {
		var count int
		query := "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?"
		err := db.Conn().QueryRow(query, table).Scan(&count)
		if err != nil {
			t.Fatalf("failed to query for table %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("table %s not found in schema", table)
		}
	}
}

func TestOpenIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Open database first time
	db1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open() failed: %v", err)
	}
	db1.Close()

	// Open same database second time
	db2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open() failed: %v", err)
	}
	defer db2.Close()

	// Verify schema is still intact by checking one table
	var count int
	query := "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='events'"
	err = db2.Conn().QueryRow(query).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query for events table: %v", err)
	}
	if count != 1 {
		t.Errorf("events table not found after second Open()")
	}
}
