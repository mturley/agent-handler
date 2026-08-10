package db

import (
	"fmt"

	watcher "github.com/mturley/watcher"
	wdb "github.com/mturley/watcher/db"
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

// deref returns the empty string for a nil pointer, or the pointed-to value.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Subscribe subscribes a session to a resource, storing the subscription in
// the watcher library's watcher_subscriptions table under handler's
// subscriber namespace. If an active subscription already exists, its
// url/lease are refreshed. If a soft-deleted (non-user) subscription exists,
// it is reinstated. A user tombstone (see Unsubscribe) is left alone.
func (db *DB) Subscribe(s Subscription) error {
	return wdb.Subscribe(db.conn, handlerSubscriber(s.SessionID),
		watcher.Resource{Type: s.ResourceType, ID: s.ResourceID, URL: deref(s.ResourceURL)},
		wdb.SubscribeOpts{TTL: sessionLeaseTTL})
}

// SubscribeIfNew creates a subscription only if one doesn't already exist
// (live). Unlike Subscribe, this does NOT refresh an already-live
// subscription and does NOT resurrect a user-unsubscribed one — used by
// auto-registration from .worktree-resources to avoid resurrecting
// subscriptions that were closed by a watcher or by the user.
func (db *DB) SubscribeIfNew(s Subscription) error {
	return wdb.Subscribe(db.conn, handlerSubscriber(s.SessionID),
		watcher.Resource{Type: s.ResourceType, ID: s.ResourceID, URL: deref(s.ResourceURL)},
		wdb.SubscribeOpts{TTL: sessionLeaseTTL, IfAbsent: true})
}

// Unsubscribe soft-deletes a subscription and marks it as user-initiated, so
// a later Subscribe/SubscribeIfNew will NOT auto-reinstate it — only
// Reinstate can revive it.
func (db *DB) Unsubscribe(sessionID, resourceType, resourceID string) error {
	return wdb.UserUnsubscribe(db.conn, handlerSubscriber(sessionID),
		watcher.Resource{Type: resourceType, ID: resourceID})
}

// Reinstate force-revives a subscription regardless of how or why it was
// removed, clearing both the soft-delete and the user tombstone flag.
func (db *DB) Reinstate(sessionID, resourceType, resourceID string) error {
	return wdb.Reinstate(db.conn, handlerSubscriber(sessionID),
		watcher.Resource{Type: resourceType, ID: resourceID})
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
	rows, err := db.conn.Query(`SELECT session_id FROM sessions WHERE branch = ?`, branch)
	if err != nil {
		return 0, fmt.Errorf("failed to look up sessions for branch %q: %w", branch, err)
	}
	var sessionIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("failed to scan session id: %w", err)
		}
		sessionIDs = append(sessionIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("error iterating sessions for branch %q: %w", branch, err)
	}
	rows.Close()

	total := 0
	for _, sessionID := range sessionIDs {
		n, err := db.SoftDeleteSubscriptionsForSession(sessionID)
		if err != nil {
			return total, fmt.Errorf("failed to soft-delete subscriptions for session %q: %w", sessionID, err)
		}
		total += n
	}
	return total, nil
}

// SoftDeleteSubscriptionsForSession soft-deletes all active subscriptions for a given session.
// Returns the count of subscriptions soft-deleted.
func (db *DB) SoftDeleteSubscriptionsForSession(sessionID string) (int, error) {
	sub := handlerSubscriber(sessionID)
	active, err := wdb.ActiveSubscriptions(db.conn, sub, false)
	if err != nil {
		return 0, fmt.Errorf("failed to count active subscriptions for session %q: %w", sessionID, err)
	}
	if err := wdb.RevokePrefix(db.conn, sub); err != nil {
		return 0, fmt.Errorf("failed to soft-delete subscriptions for session %q: %w", sessionID, err)
	}
	return len(active), nil
}

// RestoreSubscriptionsForSession un-soft-deletes subscriptions for a session
// that were dropped by the archive/restart lifecycle. Subscriptions the user
// explicitly removed via /unwatch are NOT restored. There is no library
// primitive for a bulk "reinstate everything not user-tombstoned", so this
// composes AllSubscriptions with a per-row Reinstate.
func (db *DB) RestoreSubscriptionsForSession(sessionID string) (int, error) {
	sub := handlerSubscriber(sessionID)
	all, err := wdb.AllSubscriptions(db.conn, sub, false)
	if err != nil {
		return 0, fmt.Errorf("failed to list subscriptions for session %q: %w", sessionID, err)
	}
	n := 0
	for _, row := range all {
		if row.UnsubscribedByUser || row.DeletedAt == nil {
			continue
		}
		if err := wdb.Reinstate(db.conn, sub, row.Resource); err != nil {
			return n, fmt.Errorf("failed to reinstate subscription %s/%s for session %q: %w", row.Resource.Type, row.Resource.ID, sessionID, err)
		}
		n++
	}
	return n, nil
}
