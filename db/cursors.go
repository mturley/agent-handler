package db

import (
	"database/sql"
	"fmt"
)

// GetCursor returns the last_seen_ts for the given session.
// Returns an empty string (not an error) if no cursor exists.
func (db *DB) GetCursor(sessionID string) (string, error) {
	var lastSeenTS string
	err := db.conn.QueryRow("SELECT last_seen_ts FROM session_cursors WHERE session_id = ?", sessionID).Scan(&lastSeenTS)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get cursor for session %q: %w", sessionID, err)
	}
	return lastSeenTS, nil
}

// AdvanceCursor inserts or updates the cursor for the given session.
func (db *DB) AdvanceCursor(sessionID, ts string) error {
	_, err := db.conn.Exec(`
		INSERT INTO session_cursors (session_id, last_seen_ts)
		VALUES (?, ?)
		ON CONFLICT(session_id) DO UPDATE SET last_seen_ts = excluded.last_seen_ts
	`, sessionID, ts)
	if err != nil {
		return fmt.Errorf("failed to advance cursor for session %q: %w", sessionID, err)
	}
	return nil
}
