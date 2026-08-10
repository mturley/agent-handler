package db

import (
	"database/sql"
	"fmt"

	wdb "github.com/mturley/watcher/db"
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
	subs, err := wdb.ActiveSubscriptions(db.conn, handlerSubscriber(sessionID), false)
	if err != nil {
		return nil, fmt.Errorf("failed to list active subscriptions: %w", err)
	}
	results := make([]ResourceStateWithSubscription, 0, len(subs))
	for _, s := range subs {
		r := ResourceStateWithSubscription{
			ResourceType: s.Resource.Type,
			ResourceID:   s.Resource.ID,
			StateJSON:    "{}",
		}
		if s.Resource.URL != "" {
			url := s.Resource.URL
			r.ResourceURL = &url
		}
		st, err := wdb.GetResourceState(db.conn, s.Resource.Type, s.Resource.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get resource state for %s/%s: %w", s.Resource.Type, s.Resource.ID, err)
		}
		if st != nil {
			r.StateJSON = st.StateJSON
			r.ResourceUpdatedAt = st.ResourceUpdatedAt
			r.WatcherUpdatedAt = st.WatcherUpdatedAt
		}
		results = append(results, r)
	}
	return results, nil
}
