package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type PeekState struct {
	SessionID  string `json:"session_id"`
	Content    string `json:"content"`
	NeedsInput bool   `json:"needs_input"`
	Reason     string `json:"reason"`
	UpdatedAt  string `json:"updated_at"`
}

func (db *DB) UpsertPeekState(sessionID, content string, needsInput bool, reason, updatedAt string) error {
	needsInputInt := 0
	if needsInput {
		needsInputInt = 1
	}
	_, err := db.conn.Exec(`
		INSERT INTO peek_state (session_id, content, needs_input, reason, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			content = excluded.content,
			needs_input = excluded.needs_input,
			reason = excluded.reason,
			updated_at = excluded.updated_at
	`, sessionID, content, needsInputInt, reason, updatedAt)
	if err != nil {
		return fmt.Errorf("failed to upsert peek state: %w", err)
	}
	return nil
}

func (db *DB) GetPeekState(sessionID string) (*PeekState, error) {
	var ps PeekState
	var needsInputInt int
	err := db.conn.QueryRow(`
		SELECT session_id, content, needs_input, COALESCE(reason, ''), updated_at
		FROM peek_state WHERE session_id = ?
	`, sessionID).Scan(&ps.SessionID, &ps.Content, &needsInputInt, &ps.Reason, &ps.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get peek state: %w", err)
	}
	ps.NeedsInput = needsInputInt == 1
	return &ps, nil
}

func (db *DB) ListPeekStates() ([]PeekState, error) {
	rows, err := db.conn.Query(`
		SELECT session_id, content, needs_input, COALESCE(reason, ''), updated_at
		FROM peek_state ORDER BY session_id
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list peek states: %w", err)
	}
	defer rows.Close()

	var states []PeekState
	for rows.Next() {
		var ps PeekState
		var needsInputInt int
		if err := rows.Scan(&ps.SessionID, &ps.Content, &needsInputInt, &ps.Reason, &ps.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan peek state: %w", err)
		}
		ps.NeedsInput = needsInputInt == 1
		states = append(states, ps)
	}
	return states, rows.Err()
}

// PeekStatesAge returns the time since the newest updated_at in peek_state.
// Returns a very large duration if the table is empty.
func (db *DB) PeekStatesAge() (time.Duration, error) {
	var newest string
	err := db.conn.QueryRow(`SELECT MAX(updated_at) FROM peek_state`).Scan(&newest)
	if err != nil || newest == "" {
		return 24 * time.Hour, nil
	}
	t, err := time.Parse(time.RFC3339, newest)
	if err != nil {
		return 24 * time.Hour, nil
	}
	return time.Since(t), nil
}

func (db *DB) DeletePeekStatesForSessions(sessionIDs []string) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	placeholders := make([]string, len(sessionIDs))
	args := make([]interface{}, len(sessionIDs))
	for i, id := range sessionIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf("DELETE FROM peek_state WHERE session_id IN (%s)",
		strings.Join(placeholders, ", "))
	_, err := db.conn.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to delete peek states: %w", err)
	}
	return nil
}
