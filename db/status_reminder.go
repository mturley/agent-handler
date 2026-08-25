package db

import (
	"database/sql"
	"fmt"
)

// StatusReminderState returns how many prompts a session has submitted since
// its last status event, and the timestamp that elapsed-time is measured from.
// The baseline falls back to registered_at for sessions that have never emitted
// a status, so a brand new session is measured from when it registered.
func (db *DB) StatusReminderState(sessionID string) (int, string, error) {
	var prompts int
	var baseline string
	err := db.conn.QueryRow(`
		SELECT COALESCE(prompts_since_status, 0),
		       COALESCE(NULLIF(status_baseline_at, ''), registered_at)
		FROM sessions WHERE session_id = ?`, sessionID).Scan(&prompts, &baseline)
	if err == sql.ErrNoRows {
		return 0, "", fmt.Errorf("session not found: %s", sessionID)
	}
	if err != nil {
		return 0, "", fmt.Errorf("failed to read status reminder state: %w", err)
	}
	return prompts, baseline, nil
}

// BumpPromptsSinceStatus increments the session's prompt counter and returns
// the new value.
func (db *DB) BumpPromptsSinceStatus(sessionID string) (int, error) {
	if _, err := db.conn.Exec(`
		UPDATE sessions SET prompts_since_status = COALESCE(prompts_since_status, 0) + 1
		WHERE session_id = ?`, sessionID); err != nil {
		return 0, fmt.Errorf("failed to bump prompts_since_status: %w", err)
	}
	prompts, _, err := db.StatusReminderState(sessionID)
	return prompts, err
}

// ResetStatusReminderBaseline zeroes the prompt counter and restarts the clock.
// Called when a status event is emitted and when a reminder fires, so a
// reminder can't repeat until another full interval has passed.
func (db *DB) ResetStatusReminderBaseline(sessionID, ts string) error {
	if _, err := db.conn.Exec(`
		UPDATE sessions SET prompts_since_status = 0, status_baseline_at = ?
		WHERE session_id = ?`, ts, sessionID); err != nil {
		return fmt.Errorf("failed to reset status reminder baseline: %w", err)
	}
	return nil
}
