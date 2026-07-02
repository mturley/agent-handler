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

// AdvanceCursor inserts or updates the agent cursor for the given session.
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

// AdvanceBothCursors advances both the agent and human cursors together.
// Used when the user is actively reading events (manual /inbox, explicit ack).
func (db *DB) AdvanceBothCursors(sessionID, ts string) error {
	_, err := db.conn.Exec(`
		INSERT INTO session_cursors (session_id, last_seen_ts, human_seen_ts)
		VALUES (?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET last_seen_ts = excluded.last_seen_ts, human_seen_ts = excluded.human_seen_ts
	`, sessionID, ts, ts)
	if err != nil {
		return fmt.Errorf("failed to advance both cursors for session %q: %w", sessionID, err)
	}
	return nil
}

// CatchUpHumanCursor sets human_seen_ts to match last_seen_ts.
// Called when the user sends a prompt, indicating they've seen everything.
func (db *DB) CatchUpHumanCursor(sessionID string) error {
	_, err := db.conn.Exec(`
		UPDATE session_cursors SET human_seen_ts = last_seen_ts WHERE session_id = ?
	`, sessionID)
	if err != nil {
		return fmt.Errorf("failed to catch up human cursor for session %q: %w", sessionID, err)
	}
	return nil
}

// ClearHumanCursor sets human_seen_ts to NULL (used when leaving auto mode).
func (db *DB) ClearHumanCursor(sessionID string) error {
	_, err := db.conn.Exec(`
		UPDATE session_cursors SET human_seen_ts = NULL WHERE session_id = ?
	`, sessionID)
	if err != nil {
		return fmt.Errorf("failed to clear human cursor for session %q: %w", sessionID, err)
	}
	return nil
}

// AutoDeliveredCount returns the number of events between the human cursor and
// agent cursor that match the session's subscription/broadcast rules.
// Returns 0 if cursors are equal or human cursor is NULL.
func (db *DB) AutoDeliveredCount(sessionID string) (int, error) {
	// Get both cursors
	var agentCursor string
	var humanCursor *string
	err := db.conn.QueryRow(`
		SELECT last_seen_ts, human_seen_ts FROM session_cursors WHERE session_id = ?
	`, sessionID).Scan(&agentCursor, &humanCursor)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get cursors for session %q: %w", sessionID, err)
	}

	if humanCursor == nil || *humanCursor == agentCursor {
		return 0, nil
	}

	session, err := db.GetSession(sessionID)
	if err != nil || session == nil {
		return 0, err
	}

	repoBranch := session.Repo + ":" + session.Branch
	var count int
	err = db.conn.QueryRow(`
		SELECT COUNT(DISTINCT e.id)
		FROM events e
		LEFT JOIN event_recipients er ON e.id = er.event_id
		LEFT JOIN event_resources eres ON e.id = eres.event_id
		LEFT JOIN subscriptions s ON s.resource_type = eres.resource_type AND s.resource_id = eres.resource_id AND s.session_id = ? AND s.deleted_at IS NULL
		WHERE e.ts > ? AND e.ts <= ?
		  AND (
		    e.broadcast = 1
		    OR (er.recipient_type = 'session' AND er.recipient_value = ?)
		    OR (er.recipient_type = 'branch' AND (er.recipient_value = ? OR er.recipient_value = ?))
		    OR (er.recipient_type = 'role' AND er.recipient_value = ?)
		    OR s.id IS NOT NULL
		  )
	`, sessionID, *humanCursor, agentCursor, sessionID, session.Branch, repoBranch, session.Role).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count auto-delivered events: %w", err)
	}

	return count, nil
}

// AutoDeliveredCountAll returns the number of all events between the human
// cursor and agent cursor, regardless of routing rules.
// Used by the handler session which sees all events globally.
func (db *DB) AutoDeliveredCountAll(sessionID string) (int, error) {
	var agentCursor string
	var humanCursor *string
	err := db.conn.QueryRow(`
		SELECT last_seen_ts, human_seen_ts FROM session_cursors WHERE session_id = ?
	`, sessionID).Scan(&agentCursor, &humanCursor)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get cursors for session %q: %w", sessionID, err)
	}

	if humanCursor == nil || *humanCursor == agentCursor {
		return 0, nil
	}

	var count int
	err = db.conn.QueryRow(`
		SELECT COUNT(*) FROM events WHERE ts > ? AND ts <= ?
	`, *humanCursor, agentCursor).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count auto-delivered events: %w", err)
	}

	return count, nil
}
