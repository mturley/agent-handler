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

// ResourceHistory returns all events referencing a resource, ordered by ts
// DESC. ResourceHistory is resource-scoped (it joins on the resource's
// event-linkage table): only github/jira-sourced events ever carry a
// resource linkage, and those events live exclusively in the watcher
// library's watcher_events/watcher_event_resources tables post-migration —
// handler's own events table never has event_resources rows (agent/handler/
// web events aren't resource-scoped). So this reads watcher_events only.
func (db *DB) ResourceHistory(resourceType, resourceID string, limit int) ([]Event, error) {
	events, err := wdb.EventsForResource(db.conn, resourceType, resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to query resource history: %w", err)
	}

	// EventsForResource orders ascending; handler's ResourceHistory contract is ts DESC.
	out := make([]Event, 0, len(events))
	for i := len(events) - 1; i >= 0; i-- {
		out = append(out, eventFromWatcher(events[i]))
	}
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
