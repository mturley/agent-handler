package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// RateLimitState is a session's last observed 5-hour rate limit usage.
//
// The statusline hook is the ONLY source of rate_limits data — no other hook
// receives it (see docs/claude-hook-stdin.md, "rate_limits is first-party-API
// only"). It is persisted here so the wake hooks, which never see the
// statusline payload, can read it.
//
// Sessions on Vertex omit rate_limits entirely, so many sessions will simply
// have no row here. Absence means "unknown", never "zero usage".
type RateLimitState struct {
	SessionID        string  `json:"session_id"`
	FiveHourPercent  float64 `json:"five_hour_percent"`
	FiveHourResetsAt string  `json:"five_hour_resets_at"`
	UpdatedAt        string  `json:"updated_at"`
	// LastErrorAt is when a turn last ended on a rate_limit API error
	// (StopFailure). Telemetry: it is the ground truth for whether the wake
	// mechanism was ever actually needed.
	LastErrorAt string `json:"last_error_at"`
}

// IsStale reports whether the row is older than window as of now. An
// unparseable or empty UpdatedAt counts as stale: failing closed means bad data
// suppresses wake jobs rather than triggering them off state that stopped being
// refreshed.
//
// now is passed in rather than read from the clock so callers and tests are
// deterministic.
func (r *RateLimitState) IsStale(now time.Time, window time.Duration) bool {
	if r == nil || r.UpdatedAt == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, r.UpdatedAt)
	if err != nil {
		return true
	}
	return now.Sub(t) > window
}

// UpsertRateLimit records a session's latest 5h rate limit observation.
func (db *DB) UpsertRateLimit(sessionID string, fiveHourPercent float64, fiveHourResetsAt, now string) error {
	_, err := db.conn.Exec(`
		INSERT INTO session_rate_limits (session_id, five_hour_percent, five_hour_resets_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			five_hour_percent = excluded.five_hour_percent,
			five_hour_resets_at = excluded.five_hour_resets_at,
			updated_at = excluded.updated_at
	`, sessionID, fiveHourPercent, fiveHourResetsAt, now)
	if err != nil {
		return fmt.Errorf("failed to upsert rate limit: %w", err)
	}
	return nil
}

// GetRateLimit returns a session's last observed rate limit state, or nil when
// none has been recorded.
func (db *DB) GetRateLimit(sessionID string) (*RateLimitState, error) {
	var r RateLimitState
	err := db.conn.QueryRow(`
		SELECT session_id, five_hour_percent, COALESCE(five_hour_resets_at, ''), updated_at,
		       COALESCE(last_error_at, '')
		FROM session_rate_limits WHERE session_id = ?
	`, sessionID).Scan(&r.SessionID, &r.FiveHourPercent, &r.FiveHourResetsAt, &r.UpdatedAt, &r.LastErrorAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get rate limit: %w", err)
	}
	return &r, nil
}

// DeleteRateLimitsForSessions clears rate limit rows for archived sessions.
func (db *DB) DeleteRateLimitsForSessions(sessionIDs []string) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	placeholders := make([]string, len(sessionIDs))
	args := make([]interface{}, len(sessionIDs))
	for i, id := range sessionIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf("DELETE FROM session_rate_limits WHERE session_id IN (%s)",
		strings.Join(placeholders, ", "))
	if _, err := db.conn.Exec(query, args...); err != nil {
		return fmt.Errorf("failed to delete rate limits: %w", err)
	}
	return nil
}

// RecordRateLimitError notes that a turn ended with a rate_limit API error
// (StopFailure, error == "rate_limit"). The Stop hook consults this so it does
// not clean up a wake job at the exact moment that job is needed — it is
// unknown whether a rate-limited turn fires Stop as well as StopFailure, and
// this makes the answer irrelevant.
//
// Uses a bare INSERT ... ON CONFLICT so it works even when the statusline has
// never written a row, and never overwrites a real usage reading.
func (db *DB) RecordRateLimitError(sessionID, now string) error {
	_, err := db.conn.Exec(`
		INSERT INTO session_rate_limits (session_id, five_hour_percent, five_hour_resets_at, updated_at, last_error_at)
		VALUES (?, 0, '', ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET last_error_at = excluded.last_error_at
	`, sessionID, now, now)
	if err != nil {
		return fmt.Errorf("failed to record rate limit error: %w", err)
	}
	return nil
}
