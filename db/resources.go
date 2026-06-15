package db

import (
	"fmt"
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
func (db *DB) FindRelatedSessions(sessionID string) ([]Session, error) {
	query := `
		SELECT DISTINCT
			s.session_id, s.harness, s.repo, s.branch,
			COALESCE(s.session_name, '') as session_name,
			COALESCE(s.pid, 0) as pid,
			s.status,
			s.inbox_mode,
			s.auto_poll_interval,
			s.last_active, s.registered_at, s.jsonl_path
		FROM sessions s
		JOIN subscriptions sub ON s.session_id = sub.session_id AND sub.deleted_at IS NULL
		WHERE s.session_id != ? AND s.status != 'archived'
		  AND (
		    -- Sessions subscribed to the same resource
		    (sub.resource_type, sub.resource_id) IN (
		      SELECT resource_type, resource_id
		      FROM subscriptions
		      WHERE session_id = ? AND deleted_at IS NULL
		    )
		    OR
		    -- Sessions subscribed to resources with the same parent
		    EXISTS (
		      SELECT 1
		      FROM resource_relationships rr_other
		      JOIN resource_relationships rr_mine ON rr_mine.parent_type = rr_other.parent_type AND rr_mine.parent_id = rr_other.parent_id
		      JOIN subscriptions sub_mine ON sub_mine.resource_type = rr_mine.child_type AND sub_mine.resource_id = rr_mine.child_id
		      WHERE sub_mine.session_id = ? AND sub_mine.deleted_at IS NULL
		        AND rr_other.child_type = sub.resource_type AND rr_other.child_id = sub.resource_id
		        AND (rr_other.child_type != rr_mine.child_type OR rr_other.child_id != rr_mine.child_id)
		    )
		  )
		ORDER BY s.last_active DESC
	`

	rows, err := db.conn.Query(query, sessionID, sessionID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to find related sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		err := rows.Scan(
			&s.SessionID, &s.Harness, &s.Repo, &s.Branch,
			&s.SessionName, &s.PID, &s.Status,
			&s.InboxMode, &s.AutoPollInterval,
			&s.LastActive, &s.RegisteredAt, &s.JSONLPath,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		sessions = append(sessions, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating sessions: %w", err)
	}

	return sessions, nil
}

// ResourceHistory returns all events referencing a resource, ordered by ts DESC.
func (db *DB) ResourceHistory(resourceType, resourceID string, limit int) ([]Event, error) {
	query := `
		SELECT DISTINCT e.id, e.ts, e.external_ts, e.source, e.session_id, e.type, e.title, e.body, e.author, e.author_type, e.broadcast, e.tags
		FROM events e
		JOIN event_resources er ON e.id = er.event_id
		WHERE er.resource_type = ? AND er.resource_id = ?
		ORDER BY e.ts DESC
	`

	if limit > 0 {
		query += " LIMIT ?"
	}

	var args []interface{}
	args = append(args, resourceType, resourceID)
	if limit > 0 {
		args = append(args, limit)
	}

	rows, err := db.conn.Query(query, args...)

	if err != nil {
		return nil, fmt.Errorf("failed to query resource history: %w", err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

// SessionsForResource returns all subscriptions (including deleted) for a resource.
func (db *DB) SessionsForResource(resourceType, resourceID string) ([]Subscription, error) {
	query := `
		SELECT id, session_id, resource_type, resource_id, resource_url, created_at, deleted_at
		FROM subscriptions
		WHERE resource_type = ? AND resource_id = ?
		ORDER BY created_at DESC
	`

	rows, err := db.conn.Query(query, resourceType, resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to query sessions for resource: %w", err)
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
