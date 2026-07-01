package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// Session represents a Claude Code session registered with the handler.
type Session struct {
	SessionID        string
	Harness          string
	Repo             string
	Branch           string
	SessionName      string
	PID              int
	Status           string
	InboxMode        string
	AutoPollInterval *int
	Role             string
	LastActive       string
	RegisteredAt     string
	JSONLPath        string
}

// UpsertSession inserts or updates a session.
// On conflict, it updates all fields EXCEPT inbox_mode and auto_poll_interval,
// which are preserved from the existing row if the new values are zero/nil.
func (db *DB) UpsertSession(s Session) error {
	query := `
		INSERT INTO sessions (
			session_id, harness, repo, branch, session_name, pid, status,
			inbox_mode, auto_poll_interval, role, last_active, registered_at, jsonl_path
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			harness = excluded.harness,
			repo = excluded.repo,
			branch = excluded.branch,
			session_name = excluded.session_name,
			pid = excluded.pid,
			status = excluded.status,
			inbox_mode = sessions.inbox_mode,
			auto_poll_interval = COALESCE(excluded.auto_poll_interval, sessions.auto_poll_interval),
			role = sessions.role,
			last_active = excluded.last_active,
			registered_at = excluded.registered_at,
			jsonl_path = excluded.jsonl_path
	`

	_, err := db.conn.Exec(query,
		s.SessionID, s.Harness, s.Repo, s.Branch, s.SessionName, s.PID, s.Status,
		s.InboxMode, s.AutoPollInterval, s.Role, s.LastActive, s.RegisteredAt, s.JSONLPath,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert session: %w", err)
	}

	return nil
}

// GetSession retrieves a session by its ID.
// Returns nil if the session does not exist.
func (db *DB) GetSession(sessionID string) (*Session, error) {
	query := `
		SELECT
			session_id, harness, repo, branch,
			COALESCE(session_name, '') as session_name,
			COALESCE(pid, 0) as pid,
			status,
			inbox_mode,
			auto_poll_interval,
			COALESCE(role, '') as role,
			last_active, registered_at, jsonl_path
		FROM sessions
		WHERE session_id = ?
	`

	var s Session
	err := db.conn.QueryRow(query, sessionID).Scan(
		&s.SessionID, &s.Harness, &s.Repo, &s.Branch,
		&s.SessionName, &s.PID, &s.Status,
		&s.InboxMode, &s.AutoPollInterval, &s.Role,
		&s.LastActive, &s.RegisteredAt, &s.JSONLPath,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	return &s, nil
}

// ListSessions retrieves sessions ordered by last_active DESC.
// If includeArchived is false, filters out sessions with status='archived'.
func (db *DB) ListSessions(includeArchived bool, limit, offset int) ([]Session, error) {
	var whereClauses []string
	if !includeArchived {
		whereClauses = append(whereClauses, "status != 'archived'")
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT
			session_id, harness, repo, branch,
			COALESCE(session_name, '') as session_name,
			COALESCE(pid, 0) as pid,
			status,
			inbox_mode,
			auto_poll_interval,
			COALESCE(role, '') as role,
			last_active, registered_at, jsonl_path
		FROM sessions
		%s
		ORDER BY last_active DESC
		LIMIT ? OFFSET ?
	`, whereSQL)

	rows, err := db.conn.Query(query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		err := rows.Scan(
			&s.SessionID, &s.Harness, &s.Repo, &s.Branch,
			&s.SessionName, &s.PID, &s.Status,
			&s.InboxMode, &s.AutoPollInterval, &s.Role,
			&s.LastActive, &s.RegisteredAt, &s.JSONLPath,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session row: %w", err)
		}
		sessions = append(sessions, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating session rows: %w", err)
	}

	return sessions, nil
}

// BumpLastActive updates the last_active timestamp for a session.
// Returns an error if the session is not found.
func (db *DB) BumpLastActive(sessionID, ts string) error {
	result, err := db.conn.Exec(
		"UPDATE sessions SET last_active = ? WHERE session_id = ?",
		ts, sessionID,
	)
	if err != nil {
		return fmt.Errorf("failed to bump last_active: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	return nil
}

// ArchiveSessions sets status='archived' for the given session IDs.
// Returns the number of sessions archived.
func (db *DB) ArchiveSessions(sessionIDs []string) (int, error) {
	if len(sessionIDs) == 0 {
		return 0, nil
	}

	placeholders := make([]string, len(sessionIDs))
	args := make([]interface{}, len(sessionIDs))
	for i, id := range sessionIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		"UPDATE sessions SET status = 'archived' WHERE session_id IN (%s)",
		strings.Join(placeholders, ", "),
	)

	result, err := db.conn.Exec(query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to archive sessions: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rowsAffected), nil
}

// ConfigureSession updates the inbox_mode, auto_poll_interval, and role for a session.
func (db *DB) ConfigureSession(sessionID, inboxMode string, autoPollInterval *int, role *string) error {
	result, err := db.conn.Exec(
		"UPDATE sessions SET inbox_mode = ?, auto_poll_interval = ?, role = COALESCE(?, role) WHERE session_id = ?",
		inboxMode, autoPollInterval, role, sessionID,
	)
	if err != nil {
		return fmt.Errorf("failed to configure session: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	return nil
}
