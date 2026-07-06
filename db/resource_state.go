package db

import (
	"database/sql"
	"fmt"
)

// ResourceState represents cached state of an external resource.
type ResourceState struct {
	ResourceType      string `json:"resource_type"`
	ResourceID        string `json:"resource_id"`
	StateJSON         string `json:"state_json"`
	ResourceUpdatedAt string `json:"resource_updated_at"`
	WatcherUpdatedAt  string `json:"watcher_updated_at"`
}

// ResourceStateWithSubscription pairs a resource state with subscription metadata.
type ResourceStateWithSubscription struct {
	ResourceType      string  `json:"resource_type"`
	ResourceID        string  `json:"resource_id"`
	ResourceURL       *string `json:"resource_url,omitempty"`
	StateJSON         string  `json:"state_json"`
	ResourceUpdatedAt string  `json:"resource_updated_at"`
	WatcherUpdatedAt  string  `json:"watcher_updated_at"`
}

func (db *DB) UpsertResourceState(resourceType, resourceID, stateJSON, resourceUpdatedAt, watcherUpdatedAt string) error {
	_, err := db.conn.Exec(`
		INSERT INTO resource_state (resource_type, resource_id, state_json, resource_updated_at, watcher_updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(resource_type, resource_id) DO UPDATE SET
			state_json = excluded.state_json,
			resource_updated_at = excluded.resource_updated_at,
			watcher_updated_at = excluded.watcher_updated_at
	`, resourceType, resourceID, stateJSON, resourceUpdatedAt, watcherUpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to upsert resource state: %w", err)
	}
	return nil
}

func (db *DB) GetResourceState(resourceType, resourceID string) (*ResourceState, error) {
	var rs ResourceState
	err := db.conn.QueryRow(`
		SELECT resource_type, resource_id, state_json, resource_updated_at, watcher_updated_at
		FROM resource_state
		WHERE resource_type = ? AND resource_id = ?
	`, resourceType, resourceID).Scan(&rs.ResourceType, &rs.ResourceID, &rs.StateJSON, &rs.ResourceUpdatedAt, &rs.WatcherUpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get resource state: %w", err)
	}
	return &rs, nil
}

func (db *DB) DeleteResourceState(resourceType, resourceID string) error {
	_, err := db.conn.Exec(`DELETE FROM resource_state WHERE resource_type = ? AND resource_id = ?`,
		resourceType, resourceID)
	if err != nil {
		return fmt.Errorf("failed to delete resource state: %w", err)
	}
	return nil
}

// ListResourceStatesForSession returns resource states for all active subscriptions of a session.
func (db *DB) ListResourceStatesForSession(sessionID string) ([]ResourceStateWithSubscription, error) {
	rows, err := db.conn.Query(`
		SELECT s.resource_type, s.resource_id, s.resource_url,
		       COALESCE(rs.state_json, '{}'), COALESCE(rs.resource_updated_at, ''), COALESCE(rs.watcher_updated_at, '')
		FROM subscriptions s
		LEFT JOIN resource_state rs ON rs.resource_type = s.resource_type AND rs.resource_id = s.resource_id
		WHERE s.session_id = ? AND s.deleted_at IS NULL
		ORDER BY s.created_at
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list resource states: %w", err)
	}
	defer rows.Close()

	var results []ResourceStateWithSubscription
	for rows.Next() {
		var r ResourceStateWithSubscription
		if err := rows.Scan(&r.ResourceType, &r.ResourceID, &r.ResourceURL,
			&r.StateJSON, &r.ResourceUpdatedAt, &r.WatcherUpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan resource state: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}
