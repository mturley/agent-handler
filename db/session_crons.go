package db

import (
	"fmt"
	"strings"
)

// SessionCron is one Claude Code cron job belonging to a session.
//
// Claude's cron jobs are in-memory and session-scoped: they die with the
// session, one-shot jobs auto-delete the moment they fire, and recurring jobs
// auto-expire after 7 days. Those removals happen WITHOUT a CronDelete tool
// call, so the PostToolUse create/delete hooks alone cannot keep this table
// accurate. The Stop hook's session_crons snapshot is authoritative and is
// reconciled in via SyncSessionCrons.
//
// See docs/claude-hook-stdin.md ("Tracking a session's cron jobs").
type SessionCron struct {
	SessionID string `json:"session_id"`
	JobID     string `json:"job_id"`
	Schedule  string `json:"schedule"`
	Recurring bool   `json:"recurring"`
	Prompt    string `json:"prompt"`
	CreatedAt string `json:"created_at"`
	LastSeen  string `json:"last_seen_at"`
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// UpsertSessionCron records a cron job for a session. Called from the
// PostToolUse/CronCreate hook and from snapshot reconciliation. Re-upserting an
// existing (session_id, job_id) updates the mutable fields and refreshes
// last_seen_at, but preserves the original created_at.
func (db *DB) UpsertSessionCron(sessionID string, c SessionCron, now string) error {
	_, err := db.conn.Exec(`
		INSERT INTO session_crons (session_id, job_id, schedule, recurring, prompt, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id, job_id) DO UPDATE SET
			schedule = excluded.schedule,
			recurring = excluded.recurring,
			prompt = excluded.prompt,
			last_seen_at = excluded.last_seen_at
	`, sessionID, c.JobID, c.Schedule, boolToInt(c.Recurring), c.Prompt, now, now)
	if err != nil {
		return fmt.Errorf("failed to upsert session cron: %w", err)
	}
	return nil
}

// DeleteSessionCron removes a single cron job. Called from the
// PostToolUse/CronDelete hook. Deleting a job that isn't present is not an
// error — the snapshot may have already reconciled it away.
func (db *DB) DeleteSessionCron(sessionID, jobID string) error {
	_, err := db.conn.Exec(
		`DELETE FROM session_crons WHERE session_id = ? AND job_id = ?`,
		sessionID, jobID)
	if err != nil {
		return fmt.Errorf("failed to delete session cron: %w", err)
	}
	return nil
}

// ListSessionCrons returns the cron jobs recorded for a session.
func (db *DB) ListSessionCrons(sessionID string) ([]SessionCron, error) {
	rows, err := db.conn.Query(`
		SELECT session_id, job_id, schedule, recurring, COALESCE(prompt, ''), created_at, last_seen_at
		FROM session_crons WHERE session_id = ? ORDER BY created_at, job_id
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list session crons: %w", err)
	}
	defer rows.Close()
	return scanSessionCrons(rows)
}

// ListAllSessionCrons returns every recorded cron job across all sessions.
func (db *DB) ListAllSessionCrons() ([]SessionCron, error) {
	rows, err := db.conn.Query(`
		SELECT session_id, job_id, schedule, recurring, COALESCE(prompt, ''), created_at, last_seen_at
		FROM session_crons ORDER BY session_id, created_at, job_id
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list session crons: %w", err)
	}
	defer rows.Close()
	return scanSessionCrons(rows)
}

func scanSessionCrons(rows interface {
	Next() bool
	Scan(...interface{}) error
	Err() error
}) ([]SessionCron, error) {
	var crons []SessionCron
	for rows.Next() {
		var c SessionCron
		var recurringInt int
		if err := rows.Scan(&c.SessionID, &c.JobID, &c.Schedule, &recurringInt, &c.Prompt, &c.CreatedAt, &c.LastSeen); err != nil {
			return nil, fmt.Errorf("failed to scan session cron: %w", err)
		}
		c.Recurring = recurringInt == 1
		crons = append(crons, c)
	}
	return crons, rows.Err()
}

// SyncSessionCrons reconciles a session's rows against an authoritative
// snapshot (the Stop hook's session_crons array): jobs absent from the snapshot
// are deleted (they fired and auto-deleted, expired, or were deleted while a
// hook was missed), and jobs present but unrecorded are inserted.
//
// The snapshot is scoped to one session and never touches another session's
// rows. Callers MUST NOT invoke this when the payload omitted session_crons
// entirely — an absent field is "unknown", not "no jobs", and would wrongly
// clear the session. Pass an explicitly empty slice only when the snapshot
// really was empty.
func (db *DB) SyncSessionCrons(sessionID string, snapshot []SessionCron, now string) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin session cron sync: %w", err)
	}
	defer tx.Rollback()

	if len(snapshot) == 0 {
		if _, err := tx.Exec(`DELETE FROM session_crons WHERE session_id = ?`, sessionID); err != nil {
			return fmt.Errorf("failed to clear session crons: %w", err)
		}
		return tx.Commit()
	}

	placeholders := make([]string, len(snapshot))
	args := []interface{}{sessionID}
	for i, c := range snapshot {
		placeholders[i] = "?"
		args = append(args, c.JobID)
	}
	del := fmt.Sprintf(
		`DELETE FROM session_crons WHERE session_id = ? AND job_id NOT IN (%s)`,
		strings.Join(placeholders, ", "))
	if _, err := tx.Exec(del, args...); err != nil {
		return fmt.Errorf("failed to prune session crons: %w", err)
	}

	for _, c := range snapshot {
		if _, err := tx.Exec(`
			INSERT INTO session_crons (session_id, job_id, schedule, recurring, prompt, created_at, last_seen_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(session_id, job_id) DO UPDATE SET
				schedule = excluded.schedule,
				recurring = excluded.recurring,
				prompt = excluded.prompt,
				last_seen_at = excluded.last_seen_at
		`, sessionID, c.JobID, c.Schedule, boolToInt(c.Recurring), c.Prompt, now, now); err != nil {
			return fmt.Errorf("failed to reconcile session cron %q: %w", c.JobID, err)
		}
	}

	return tx.Commit()
}

// DeleteSessionCronsForSessions clears cron rows for the given sessions. Used
// by cleanup when sessions are archived — the jobs died with the process.
func (db *DB) DeleteSessionCronsForSessions(sessionIDs []string) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	placeholders := make([]string, len(sessionIDs))
	args := make([]interface{}, len(sessionIDs))
	for i, id := range sessionIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf("DELETE FROM session_crons WHERE session_id IN (%s)",
		strings.Join(placeholders, ", "))
	if _, err := db.conn.Exec(query, args...); err != nil {
		return fmt.Errorf("failed to delete session crons: %w", err)
	}
	return nil
}
