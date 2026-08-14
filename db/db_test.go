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

	// Verify the current (Phase 2c) tables exist. The legacy watcher tables
	// (subscriptions, resource_relationships, resource_state, watcher_status)
	// are intentionally NOT created on fresh installs anymore — they live only
	// in pre-2c databases, and their data is now in the watcher library's
	// watcher_* tables.
	expectedTables := []string{
		"events",
		"event_recipients",
		"event_resources",
		"sessions",
		"session_cursors",
		"cost_epoch_state",
		"daily_cost",
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

	// The legacy watcher tables must NOT be created on a fresh install.
	legacyTables := []string{
		"subscriptions",
		"resource_relationships",
		"resource_state",
		"watcher_status",
	}
	for _, table := range legacyTables {
		var count int
		query := "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?"
		if err := db.Conn().QueryRow(query, table).Scan(&count); err != nil {
			t.Fatalf("failed to query for legacy table %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("legacy table %s should NOT exist on a fresh install", table)
		}
	}

	// The watcher library's own tables must exist on a fresh install (Open
	// runs wdb.Migrate) — the single source of truth for subscriptions/
	// resource state/relationships now that the legacy tables are gone.
	watcherTables := []string{
		"watcher_events",
		"watcher_event_resources",
		"watcher_resource_state",
		"watcher_resource_relationships",
		"watcher_subscriptions",
		"watcher_poller_status",
	}
	for _, table := range watcherTables {
		var count int
		query := "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?"
		if err := db.Conn().QueryRow(query, table).Scan(&count); err != nil {
			t.Fatalf("failed to query for watcher table %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("watcher library table %s not found in fresh-install schema", table)
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

// TestReopenDoesNotWipeDailyCost guards against a regression where the
// one-time daily_cost cutover wipe (added when cost_epoch_state replaced
// cost_snapshots) ran unconditionally on every Open() instead of only once
// during the actual schema cutover. Since Open() is called on every CLI
// invocation and every statusline tick, an unconditional wipe would delete
// all accrued cost continuously. A fresh DB (no legacy cost_snapshots table)
// must never trigger the wipe, on the first Open() or any subsequent one.
func TestReopenDoesNotWipeDailyCost(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open() failed: %v", err)
	}
	seedSession(t, db1, "reopen-cost-session")
	if err := db1.UpsertDailyCost("reopen-cost-session", "2026-08-14", 5.00, 50000, 2000); err != nil {
		t.Fatalf("UpsertDailyCost failed: %v", err)
	}
	db1.Close()

	// Re-open the same database. This must NOT wipe daily_cost, since there
	// is no legacy cost_snapshots table to cut over from.
	db2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open() failed: %v", err)
	}
	defer db2.Close()

	dc, err := db2.GetDailyCostForSession("reopen-cost-session", "2026-08-14")
	if err != nil {
		t.Fatalf("GetDailyCostForSession failed: %v", err)
	}
	if dc == nil {
		t.Fatal("expected daily_cost row to survive re-open, got nil")
	}
	if dc.CostUSD != 5.00 {
		t.Errorf("expected cost 5.00 to survive re-open, got %v", dc.CostUSD)
	}
}
