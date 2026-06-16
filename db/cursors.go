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

// InheritCursorForBranch finds the most recent cursor from archived sessions
// on the same branch/repo and returns it. Returns empty string if none found.
func (db *DB) InheritCursorForBranch(repo, branch string) (string, error) {
	var ts string
	err := db.conn.QueryRow(`
		SELECT sc.last_seen_ts FROM session_cursors sc
		JOIN sessions s ON s.session_id = sc.session_id
		WHERE s.repo = ? AND s.branch = ? AND s.status = 'archived'
		ORDER BY sc.last_seen_ts DESC LIMIT 1
	`, repo, branch).Scan(&ts)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to inherit cursor: %w", err)
	}
	return ts, nil
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
