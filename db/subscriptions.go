package db

import (
	"database/sql"
	"fmt"
)

// Subscription represents a session's subscription to a resource.
type Subscription struct {
	ID           string  `json:"id"`
	SessionID    string  `json:"session_id"`
	ResourceType string  `json:"resource_type"`
	ResourceID   string  `json:"resource_id"`
	ResourceURL  *string `json:"resource_url,omitempty"`
	CreatedAt    string  `json:"created_at"`
	DeletedAt    *string `json:"deleted_at,omitempty"`
}

// Subscribe subscribes a session to a resource.
// If an active subscription already exists, returns nil (dedup).
// If a soft-deleted subscription exists, reinstates it.
// Otherwise, inserts a new subscription.
func (db *DB) Subscribe(s Subscription) error {
	// Check for existing subscription (active or soft-deleted)
	var existingID string
	var deletedAt *string
	err := db.conn.QueryRow(`
		SELECT id, deleted_at FROM subscriptions
		WHERE session_id = ? AND resource_type = ? AND resource_id = ?
	`, s.SessionID, s.ResourceType, s.ResourceID).Scan(&existingID, &deletedAt)

	if err == nil {
		// Subscription exists
		if deletedAt == nil {
			// Already active, nothing to do
			return nil
		}
		// Soft-deleted, reinstate it
		return db.Reinstate(s.SessionID, s.ResourceType, s.ResourceID)
	}

	if err != sql.ErrNoRows {
		return fmt.Errorf("failed to check for existing subscription: %w", err)
	}

	// No existing subscription, insert new
	_, err = db.conn.Exec(`
		INSERT INTO subscriptions (id, session_id, resource_type, resource_id, resource_url, created_at, deleted_at)
		VALUES (?, ?, ?, ?, ?, ?, NULL)
	`, s.ID, s.SessionID, s.ResourceType, s.ResourceID, s.ResourceURL, s.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert subscription: %w", err)
	}
	return nil
}

// SubscribeIfNew creates a subscription only if one doesn't already exist
// (active or soft-deleted). Unlike Subscribe, this does NOT reinstate
// soft-deleted subscriptions — used by auto-registration from .worktree-resources
// to avoid resurrecting subscriptions that were closed by a watcher.
func (db *DB) SubscribeIfNew(s Subscription) error {
	var count int
	err := db.conn.QueryRow(`
		SELECT COUNT(*) FROM subscriptions
		WHERE session_id = ? AND resource_type = ? AND resource_id = ?
	`, s.SessionID, s.ResourceType, s.ResourceID).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check existing subscription: %w", err)
	}
	if count > 0 {
		return nil
	}

	_, err = db.conn.Exec(`
		INSERT INTO subscriptions (id, session_id, resource_type, resource_id, resource_url, created_at, deleted_at)
		VALUES (?, ?, ?, ?, ?, ?, NULL)
	`, s.ID, s.SessionID, s.ResourceType, s.ResourceID, s.ResourceURL, s.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert subscription: %w", err)
	}
	return nil
}

// Unsubscribe soft-deletes a subscription.
// Returns an error if no active subscription is found.
// If this was the last active subscription for the resource, also deletes the resource_state row.
func (db *DB) Unsubscribe(sessionID, resourceType, resourceID string) error {
	res, err := db.conn.Exec(`
		UPDATE subscriptions
		SET deleted_at = datetime('now')
		WHERE session_id = ? AND resource_type = ? AND resource_id = ? AND deleted_at IS NULL
	`, sessionID, resourceType, resourceID)
	if err != nil {
		return fmt.Errorf("failed to unsubscribe: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("no active subscription found for session %q, resource %s/%s", sessionID, resourceType, resourceID)
	}

	// Check if this was the last active subscription for this resource
	var remaining int
	err = db.conn.QueryRow(`
		SELECT COUNT(*) FROM subscriptions
		WHERE resource_type = ? AND resource_id = ? AND deleted_at IS NULL
	`, resourceType, resourceID).Scan(&remaining)
	if err != nil {
		return fmt.Errorf("failed to check remaining subscriptions: %w", err)
	}

	if remaining == 0 {
		if err := db.DeleteResourceState(resourceType, resourceID); err != nil {
			return fmt.Errorf("failed to clean up resource state: %w", err)
		}
	}

	return nil
}

// Reinstate clears the deleted_at timestamp for a soft-deleted subscription.
func (db *DB) Reinstate(sessionID, resourceType, resourceID string) error {
	res, err := db.conn.Exec(`
		UPDATE subscriptions
		SET deleted_at = NULL
		WHERE session_id = ? AND resource_type = ? AND resource_id = ? AND deleted_at IS NOT NULL
	`, sessionID, resourceType, resourceID)
	if err != nil {
		return fmt.Errorf("failed to reinstate subscription: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("no soft-deleted subscription found for session %q, resource %s/%s", sessionID, resourceType, resourceID)
	}

	return nil
}

// ListSubscriptions returns subscriptions for a session, optionally including soft-deleted ones.
func (db *DB) ListSubscriptions(sessionID string, includeDeleted bool) ([]Subscription, error) {
	query := `
		SELECT id, session_id, resource_type, resource_id, resource_url, created_at, deleted_at
		FROM subscriptions
		WHERE session_id = ?
	`
	if !includeDeleted {
		query += " AND deleted_at IS NULL"
	}
	query += " ORDER BY created_at DESC"

	rows, err := db.conn.Query(query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []Subscription
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(&s.ID, &s.SessionID, &s.ResourceType, &s.ResourceID, &s.ResourceURL, &s.CreatedAt, &s.DeletedAt); err != nil {
			return nil, fmt.Errorf("failed to scan subscription: %w", err)
		}
		subs = append(subs, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating subscriptions: %w", err)
	}

	return subs, nil
}

// SoftDeleteSubscriptionsForBranch soft-deletes all active subscriptions for sessions on a given branch.
// Returns the count of subscriptions soft-deleted.
func (db *DB) SoftDeleteSubscriptionsForBranch(branch string) (int, error) {
	res, err := db.conn.Exec(`
		UPDATE subscriptions
		SET deleted_at = datetime('now')
		WHERE session_id IN (SELECT session_id FROM sessions WHERE branch = ?)
		  AND deleted_at IS NULL
	`, branch)
	if err != nil {
		return 0, fmt.Errorf("failed to soft-delete subscriptions for branch %q: %w", branch, err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to check rows affected: %w", err)
	}

	return int(rows), nil
}

// SoftDeleteSubscriptionsForSession soft-deletes all active subscriptions for a given session.
// Returns the count of subscriptions soft-deleted.
func (db *DB) SoftDeleteSubscriptionsForSession(sessionID string) (int, error) {
	res, err := db.conn.Exec(`
		UPDATE subscriptions
		SET deleted_at = datetime('now')
		WHERE session_id = ? AND deleted_at IS NULL
	`, sessionID)
	if err != nil {
		return 0, fmt.Errorf("failed to soft-delete subscriptions for session %q: %w", sessionID, err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to check rows affected: %w", err)
	}

	return int(rows), nil
}

// RestoreSubscriptionsForSession un-soft-deletes all subscriptions for a session.
func (db *DB) RestoreSubscriptionsForSession(sessionID string) (int, error) {
	res, err := db.conn.Exec(`
		UPDATE subscriptions
		SET deleted_at = NULL
		WHERE session_id = ? AND deleted_at IS NOT NULL
	`, sessionID)
	if err != nil {
		return 0, fmt.Errorf("failed to restore subscriptions for session %q: %w", sessionID, err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to check rows affected: %w", err)
	}

	return int(rows), nil
}
