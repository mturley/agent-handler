package db

import (
	"fmt"
	"sort"

	wdb "github.com/mturley/watcher/db"
)

// ResourceRelationship represents a hierarchical relationship between resources.
type ResourceRelationship struct {
	ID           string
	ChildType    string
	ChildID      string
	ChildURL     *string
	ParentType   string
	ParentID     string
	ParentURL    *string
	Relationship string
	Source       string
	CreatedAt    string
}

// LinkResources inserts a resource relationship.
func (db *DB) LinkResources(r ResourceRelationship) error {
	_, err := db.conn.Exec(`
		INSERT INTO resource_relationships (id, child_type, child_id, child_url, parent_type, parent_id, parent_url, relationship, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, r.ID, r.ChildType, r.ChildID, r.ChildURL, r.ParentType, r.ParentID, r.ParentURL, r.Relationship, r.Source, r.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to link resources: %w", err)
	}
	return nil
}

// FindRelatedSessions finds sessions that share direct resource subscriptions OR subscribe to resources with the same parent.
// Excludes the given session and archived sessions. Returns sessions ordered by last_active DESC.
//
// Subscriptions now live in the watcher library's watcher_subscriptions
// table (see db/watcher_bridge.go), so this walks the library via
// SubscribersOf instead of joining handler's old subscriptions table.
// resource_relationships remains a handler-owned table.
func (db *DB) FindRelatedSessions(sessionID string) ([]Session, error) {
	mine, err := wdb.ActiveSubscriptions(db.conn, handlerSubscriber(sessionID), false)
	if err != nil {
		return nil, fmt.Errorf("failed to list subscriptions for session %q: %w", sessionID, err)
	}
	if len(mine) == 0 {
		return nil, nil
	}

	related := map[string]bool{}
	addSubscribers := func(resourceType, resourceID string) error {
		subs, err := wdb.SubscribersOf(db.conn, resourceType, resourceID)
		if err != nil {
			return fmt.Errorf("failed to find subscribers of %s/%s: %w", resourceType, resourceID, err)
		}
		for _, s := range subs {
			if !isLiveSubscription(s) {
				continue
			}
			sid, ok := sessionIDFromSubscriber(s.Subscriber)
			if !ok || sid == sessionID {
				continue
			}
			related[sid] = true
		}
		return nil
	}

	for _, r := range mine {
		// Sessions subscribed to the same resource.
		if err := addSubscribers(r.Resource.Type, r.Resource.ID); err != nil {
			return nil, err
		}

		// Sessions subscribed to a sibling resource sharing a parent.
		siblingRows, err := db.conn.Query(`
			SELECT DISTINCT rr_other.child_type, rr_other.child_id
			FROM resource_relationships rr_mine
			JOIN resource_relationships rr_other
			  ON rr_other.parent_type = rr_mine.parent_type AND rr_other.parent_id = rr_mine.parent_id
			WHERE rr_mine.child_type = ? AND rr_mine.child_id = ?
			  AND (rr_other.child_type != rr_mine.child_type OR rr_other.child_id != rr_mine.child_id)
		`, r.Resource.Type, r.Resource.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to find sibling resources of %s/%s: %w", r.Resource.Type, r.Resource.ID, err)
		}
		var siblings [][2]string
		for siblingRows.Next() {
			var t, id string
			if err := siblingRows.Scan(&t, &id); err != nil {
				siblingRows.Close()
				return nil, fmt.Errorf("failed to scan sibling resource: %w", err)
			}
			siblings = append(siblings, [2]string{t, id})
		}
		if err := siblingRows.Err(); err != nil {
			siblingRows.Close()
			return nil, fmt.Errorf("error iterating sibling resources: %w", err)
		}
		siblingRows.Close()

		for _, sib := range siblings {
			if err := addSubscribers(sib[0], sib[1]); err != nil {
				return nil, err
			}
		}
	}

	sessions := make([]Session, 0, len(related))
	for sid := range related {
		s, err := db.GetSession(sid)
		if err != nil {
			return nil, fmt.Errorf("failed to load related session %q: %w", sid, err)
		}
		if s == nil || s.Status == "archived" {
			continue
		}
		sessions = append(sessions, *s)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].LastActive > sessions[j].LastActive })

	return sessions, nil
}

// ResourceHistory returns all events referencing a resource, ordered by ts DESC.
//
// Event *writes* have not yet been repointed to the watcher library (every
// producer — cmd/emit.go, cmd/api/actions.go, cmd/unregister.go, the
// watcher/framework.go pollers — still calls db.InsertEvent into handler's
// own events/event_resources tables; that migration is a separate,
// not-yet-done task). Reading ONLY via wdb.EventsForResource would make this
// method permanently return nothing and regress TestResourceHistory, which
// is outside this task's tracked red-test window. So this reads both
// sources and merges them, matching the "read from both until writes move"
// pattern used for the inbox UNION (Task 6). Once event writes move to the
// watcher library, the handler-table half of this can be deleted.
func (db *DB) ResourceHistory(resourceType, resourceID string, limit int) ([]Event, error) {
	rows, err := db.conn.Query(`
		SELECT DISTINCT e.id, e.ts, e.external_ts, e.source, e.session_id, e.type, e.title, e.body, e.author, e.author_type, e.broadcast, e.tags
		FROM events e
		JOIN event_resources er ON e.id = er.event_id
		WHERE er.resource_type = ? AND er.resource_id = ?
	`, resourceType, resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to query resource history: %w", err)
	}
	handlerEvents, err := scanEvents(rows)
	rows.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to query resource history: %w", err)
	}

	watcherEvents, err := wdb.EventsForResource(db.conn, resourceType, resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to query resource history from watcher library: %w", err)
	}

	out := make([]Event, 0, len(handlerEvents)+len(watcherEvents))
	seen := make(map[string]bool, len(handlerEvents))
	for _, e := range handlerEvents {
		seen[e.ID] = true
		out = append(out, e)
	}
	for _, e := range watcherEvents {
		if seen[e.ID] {
			continue
		}
		out = append(out, eventFromWatcher(e))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS > out[j].TS })

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// SessionsForResource returns all subscriptions (including deleted) for a resource.
func (db *DB) SessionsForResource(resourceType, resourceID string) ([]Subscription, error) {
	rows, err := wdb.SubscribersOf(db.conn, resourceType, resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to query sessions for resource: %w", err)
	}

	out := make([]Subscription, 0, len(rows))
	for _, r := range rows {
		sid, ok := sessionIDFromSubscriber(r.Subscriber)
		if !ok {
			continue // skip non-handler subscribers
		}
		out = append(out, subFromWatcher(sid, r))
	}
	return out, nil
}
