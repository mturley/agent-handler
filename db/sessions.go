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
	TerminalType     string
	TerminalID       string
	CmuxWorkspaceID    string
	CmuxWorkspaceName  string
	CmuxWorkspaceColor string
	LastActive         string
	LastPrompt         string
	CWD                string
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
			inbox_mode, auto_poll_interval, role, terminal_type, terminal_id,
			cmux_workspace_id, cmux_workspace_name, cmux_workspace_color, last_active, last_prompt, cwd, registered_at, jsonl_path
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			terminal_type = excluded.terminal_type,
			terminal_id = excluded.terminal_id,
			cmux_workspace_id = excluded.cmux_workspace_id,
			cmux_workspace_name = excluded.cmux_workspace_name,
			cmux_workspace_color = excluded.cmux_workspace_color,
			last_active = excluded.last_active,
			last_prompt = COALESCE(sessions.last_prompt, excluded.last_prompt),
			cwd = excluded.cwd,
			registered_at = excluded.registered_at,
			jsonl_path = excluded.jsonl_path
	`

	_, err := db.conn.Exec(query,
		s.SessionID, s.Harness, s.Repo, s.Branch, s.SessionName, s.PID, s.Status,
		s.InboxMode, s.AutoPollInterval, s.Role, s.TerminalType, s.TerminalID,
		s.CmuxWorkspaceID, s.CmuxWorkspaceName, s.CmuxWorkspaceColor, s.LastActive, s.LastPrompt, s.CWD, s.RegisteredAt, s.JSONLPath,
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
			COALESCE(terminal_type, '') as terminal_type,
			COALESCE(terminal_id, '') as terminal_id,
			COALESCE(cmux_workspace_id, '') as cmux_workspace_id,
			COALESCE(cmux_workspace_name, '') as cmux_workspace_name,
			COALESCE(cmux_workspace_color, '') as cmux_workspace_color,
			last_active,
			COALESCE(last_prompt, '') as last_prompt,
			COALESCE(cwd, '') as cwd,
			registered_at, jsonl_path
		FROM sessions
		WHERE session_id = ?
	`

	var s Session
	err := db.conn.QueryRow(query, sessionID).Scan(
		&s.SessionID, &s.Harness, &s.Repo, &s.Branch,
		&s.SessionName, &s.PID, &s.Status,
		&s.InboxMode, &s.AutoPollInterval, &s.Role,
		&s.TerminalType, &s.TerminalID, &s.CmuxWorkspaceID, &s.CmuxWorkspaceName, &s.CmuxWorkspaceColor,
		&s.LastActive, &s.LastPrompt, &s.CWD, &s.RegisteredAt, &s.JSONLPath,
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
			COALESCE(terminal_type, '') as terminal_type,
			COALESCE(terminal_id, '') as terminal_id,
			COALESCE(cmux_workspace_id, '') as cmux_workspace_id,
			COALESCE(cmux_workspace_name, '') as cmux_workspace_name,
			COALESCE(cmux_workspace_color, '') as cmux_workspace_color,
			last_active,
			COALESCE(last_prompt, '') as last_prompt,
			COALESCE(cwd, '') as cwd,
			registered_at, jsonl_path
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
			&s.TerminalType, &s.TerminalID, &s.CmuxWorkspaceID, &s.CmuxWorkspaceName, &s.CmuxWorkspaceColor,
			&s.LastActive, &s.LastPrompt, &s.CWD, &s.RegisteredAt, &s.JSONLPath,
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

// ListSessionsByName returns all active sessions with the given name.
func (db *DB) ListSessionsByName(name string) ([]Session, error) {
	query := `
		SELECT
			session_id, harness, repo, branch,
			COALESCE(session_name, '') as session_name,
			COALESCE(pid, 0) as pid,
			status,
			inbox_mode,
			auto_poll_interval,
			COALESCE(role, '') as role,
			COALESCE(terminal_type, '') as terminal_type,
			COALESCE(terminal_id, '') as terminal_id,
			COALESCE(cmux_workspace_id, '') as cmux_workspace_id,
			COALESCE(cmux_workspace_name, '') as cmux_workspace_name,
			COALESCE(cmux_workspace_color, '') as cmux_workspace_color,
			last_active,
			COALESCE(last_prompt, '') as last_prompt,
			COALESCE(cwd, '') as cwd,
			registered_at, jsonl_path
		FROM sessions
		WHERE session_name = ? AND status = 'active'
		ORDER BY last_active DESC
	`
	rows, err := db.conn.Query(query, name)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions by name: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		err := rows.Scan(
			&s.SessionID, &s.Harness, &s.Repo, &s.Branch,
			&s.SessionName, &s.PID, &s.Status,
			&s.InboxMode, &s.AutoPollInterval, &s.Role,
			&s.TerminalType, &s.TerminalID, &s.CmuxWorkspaceID, &s.CmuxWorkspaceName, &s.CmuxWorkspaceColor,
			&s.LastActive, &s.LastPrompt, &s.CWD, &s.RegisteredAt, &s.JSONLPath,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session row: %w", err)
		}
		sessions = append(sessions, s)
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

// BumpLastPrompt updates the last_prompt timestamp for a session.
func (db *DB) BumpLastPrompt(sessionID, ts string) error {
	_, err := db.conn.Exec(
		"UPDATE sessions SET last_prompt = ? WHERE session_id = ?",
		ts, sessionID,
	)
	return err
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
