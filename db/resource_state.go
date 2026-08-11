package db

import (
	"fmt"

	wdb "github.com/mturley/watcher/db"
)

// ResourceStateWithSubscription pairs a resource state with subscription metadata.
type ResourceStateWithSubscription struct {
	ResourceType      string  `json:"resource_type"`
	ResourceID        string  `json:"resource_id"`
	ResourceURL       *string `json:"resource_url,omitempty"`
	StateJSON         string  `json:"state_json"`
	ResourceUpdatedAt string  `json:"resource_updated_at"`
	WatcherUpdatedAt  string  `json:"watcher_updated_at"`
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
