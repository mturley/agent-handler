package db

import "testing"

// createLegacySubscriptionsTable creates the pre-2c `subscriptions` table
// WITHOUT the unsubscribed_by column, so a test can verify
// EnsureLegacySubscriptionsColumn adds it.
func createLegacySubscriptionsTable(t *testing.T, d *DB) {
	t.Helper()
	if _, err := d.conn.Exec(`
		CREATE TABLE subscriptions (
		    id TEXT PRIMARY KEY,
		    session_id TEXT NOT NULL,
		    resource_type TEXT NOT NULL,
		    resource_id TEXT NOT NULL,
		    resource_url TEXT,
		    created_at TEXT NOT NULL,
		    deleted_at TEXT
		);
	`); err != nil {
		t.Fatalf("create legacy subscriptions table: %v", err)
	}
}

func columnExists(t *testing.T, d *DB, table, column string) bool {
	t.Helper()
	rows, err := d.conn.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		t.Fatalf("pragma_table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate columns: %v", err)
	}
	return false
}

// On a fresh 2c install the legacy `subscriptions` table does not exist, so
// EnsureLegacySubscriptionsColumn must be a genuine no-op (return nil), not
// error with "no such table".
func TestEnsureLegacySubscriptionsColumnNoOpWhenTableAbsent(t *testing.T) {
	d := testDB(t)
	if tableExists(d.conn, "subscriptions") {
		t.Fatal("fresh 2c install should not have a subscriptions table")
	}
	if err := EnsureLegacySubscriptionsColumn(d.conn); err != nil {
		t.Fatalf("EnsureLegacySubscriptionsColumn on DB without subscriptions = %v, want nil", err)
	}
}

// When the legacy `subscriptions` table exists without the unsubscribed_by
// column, EnsureLegacySubscriptionsColumn must add it (and be idempotent).
func TestEnsureLegacySubscriptionsColumnAddsColumnWhenTablePresent(t *testing.T) {
	d := testDB(t)
	createLegacySubscriptionsTable(t, d)

	if columnExists(t, d, "subscriptions", "unsubscribed_by") {
		t.Fatal("legacy subscriptions table should start without unsubscribed_by")
	}
	if err := EnsureLegacySubscriptionsColumn(d.conn); err != nil {
		t.Fatalf("EnsureLegacySubscriptionsColumn = %v, want nil", err)
	}
	if !columnExists(t, d, "subscriptions", "unsubscribed_by") {
		t.Fatal("unsubscribed_by column was not added")
	}
	// Idempotent: a second call must not error.
	if err := EnsureLegacySubscriptionsColumn(d.conn); err != nil {
		t.Fatalf("second EnsureLegacySubscriptionsColumn = %v, want nil (idempotent)", err)
	}
}
