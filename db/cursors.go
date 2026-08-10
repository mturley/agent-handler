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

// GetHumanCursor returns the human_seen_ts for the given session.
// Falls back to last_seen_ts if human_seen_ts is NULL.
func (db *DB) GetHumanCursor(sessionID string) (string, error) {
	var cursor string
	err := db.conn.QueryRow(
		"SELECT COALESCE(human_seen_ts, last_seen_ts) FROM session_cursors WHERE session_id = ?",
		sessionID,
	).Scan(&cursor)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get human cursor for session %q: %w", sessionID, err)
	}
	return cursor, nil
}

// pruneDismissedBehindCursor removes dismissed_events rows whose event has
// scrolled behind BOTH cursors for the session. Those rows are redundant
// because the event is already excluded by the ts > cursor filter.
func (db *DB) pruneDismissedBehindCursor(sessionID string) error {
	// Use the lower of the two cursors so a row is only pruned once the event
	// is behind both. GetHumanCursor coalesces to last_seen_ts when NULL.
	agent, err := db.GetCursor(sessionID)
	if err != nil {
		return err
	}
	human, err := db.GetHumanCursor(sessionID)
	if err != nil {
		return err
	}
	threshold := agent
	if human != "" && human < threshold {
		threshold = human
	}
	if threshold == "" {
		return nil
	}
	_, err = db.conn.Exec(`
		DELETE FROM dismissed_events
		WHERE session_id = ?
		  AND event_id IN (SELECT id FROM events WHERE ts <= ?)
	`, sessionID, threshold)
	if err != nil {
		return fmt.Errorf("failed to prune dismissed events for %q: %w", sessionID, err)
	}
	return nil
}

// MaxEventTS returns the maximum ts among the given events, or "" if the
// slice is empty. Event ts values are RFC3339 UTC timestamps, which sort
// correctly with plain string comparison. Callers that advance a cursor
// after reading a batch of events should advance to this value rather than
// wall-clock time.Now(), which can skip events written in the same
// second-granularity tick as the read.
func MaxEventTS(events []Event) string {
	max := ""
	for _, e := range events {
		if e.TS > max {
			max = e.TS
		}
	}
	return max
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
	_ = db.pruneDismissedBehindCursor(sessionID)
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
	_ = db.pruneDismissedBehindCursor(sessionID)
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
	_ = db.pruneDismissedBehindCursor(sessionID)
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

	// Same inbox scope as the unread queries, plus an upper time bound (events
	// between the human cursor and the agent cursor). The extra `e.ts <= ?`
	// predicate's placeholder follows all of inboxArgs's placeholders, so
	// agentCursor is appended after inboxArgs.
	gated := db.watcherMigrationDone()
	query := inboxSelectPred("SELECT", inboxCountCols, gated, "e.ts <= ?")
	args := append(inboxArgs(session, *humanCursor, gated), agentCursor)
	var count int
	err = db.conn.QueryRow(query, args...).Scan(&count)
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
