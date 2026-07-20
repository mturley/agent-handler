package db

import (
	"database/sql"
	"fmt"
)

// inboxExcludedTypes are event types excluded from non-handler inbox queries.
// These are bookkeeping events that shouldn't appear as unread in session inboxes.
const inboxExcludedTypesSQL = "AND e.type NOT IN ('watch_started')"

// Event represents an event in the system.
type Event struct {
	ID         string
	TS         string
	ExternalTS *string
	Source     string
	SessionID  *string
	Type       string
	Title      string
	Body       *string
	Author     *string
	AuthorType *string
	Broadcast  bool
	Tags       *string
}

// EventRecipient represents a recipient of an event.
type EventRecipient struct {
	RecipientType  string
	RecipientValue string
}

// EventResource represents a resource referenced by an event.
type EventResource struct {
	ResourceType string
	ResourceID   string
	ResourceURL  *string
}

// EventFilter defines criteria for querying events.
type EventFilter struct {
	SessionID *string
	Source    *string
	Type      *string
	Since     *string
	Limit     int
	Offset    int
}

// InsertEvent inserts an event along with its recipients and resources in a transaction.
func (db *DB) InsertEvent(e Event, recipients []EventRecipient, resources []EventResource) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Insert event
	broadcastInt := 0
	if e.Broadcast {
		broadcastInt = 1
	}

	_, err = tx.Exec(`
		INSERT INTO events (id, ts, external_ts, source, session_id, type, title, body, author, author_type, broadcast, tags)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, e.ID, e.TS, e.ExternalTS, e.Source, e.SessionID, e.Type, e.Title, e.Body, e.Author, e.AuthorType, broadcastInt, e.Tags)
	if err != nil {
		return fmt.Errorf("failed to insert event: %w", err)
	}

	// Insert recipients
	for _, r := range recipients {
		_, err = tx.Exec(`
			INSERT INTO event_recipients (event_id, recipient_type, recipient_value)
			VALUES (?, ?, ?)
		`, e.ID, r.RecipientType, r.RecipientValue)
		if err != nil {
			return fmt.Errorf("failed to insert event recipient: %w", err)
		}
	}

	// Insert resources
	for _, r := range resources {
		_, err = tx.Exec(`
			INSERT INTO event_resources (event_id, resource_type, resource_id, resource_url)
			VALUES (?, ?, ?, ?)
		`, e.ID, r.ResourceType, r.ResourceID, r.ResourceURL)
		if err != nil {
			return fmt.Errorf("failed to insert event resource: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// QueryEvents returns events matching the given filter, ordered by ts DESC.
func (db *DB) QueryEvents(f EventFilter) ([]Event, error) {
	query := "SELECT id, ts, external_ts, source, session_id, type, title, body, author, author_type, broadcast, tags FROM events WHERE 1=1"
	args := []interface{}{}

	if f.SessionID != nil {
		query += " AND session_id = ?"
		args = append(args, *f.SessionID)
	}
	if f.Source != nil {
		query += " AND source = ?"
		args = append(args, *f.Source)
	}
	if f.Type != nil {
		query += " AND type = ?"
		args = append(args, *f.Type)
	}
	if f.Since != nil {
		query += " AND ts > ?"
		args = append(args, *f.Since)
	}

	query += " ORDER BY ts DESC"

	if f.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, f.Limit)
	}
	if f.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, f.Offset)
	}

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

// UnreadForSession returns unread events for a session, ordered by ts ASC.
// An event is unread if:
// - ts > cursor AND
// - (broadcast=1 OR recipient matches session/branch OR event references a subscribed resource)
func (db *DB) UnreadForSession(sessionID string) ([]Event, error) {
	// Get cursor
	cursor, err := db.GetCursor(sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get cursor: %w", err)
	}

	// Get session branch
	session, err := db.GetSession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}
	if session == nil {
		return nil, fmt.Errorf("session %q not found", sessionID)
	}

	// Build the unread query (excludes watch_started — those are handler-only bookkeeping)
	query := `
		SELECT DISTINCT e.id, e.ts, e.external_ts, e.source, e.session_id, e.type, e.title, e.body, e.author, e.author_type, e.broadcast, e.tags
		FROM events e
		LEFT JOIN event_recipients er ON e.id = er.event_id
		LEFT JOIN event_resources eres ON e.id = eres.event_id
		LEFT JOIN subscriptions s ON s.resource_type = eres.resource_type AND s.resource_id = eres.resource_id AND s.session_id = ? AND s.deleted_at IS NULL
		WHERE e.ts > ?
		  ` + inboxExcludedTypesSQL + `
		  AND (
		    e.broadcast = 1
		    OR (er.recipient_type = 'session' AND er.recipient_value = ?)
		    OR (er.recipient_type = 'branch' AND (er.recipient_value = ? OR er.recipient_value = ?))
		    OR (er.recipient_type = 'role' AND er.recipient_value = ?)
		    OR s.id IS NOT NULL
		  )
		ORDER BY e.ts ASC
	`

	repoBranch := session.Repo + ":" + session.Branch
	rows, err := db.conn.Query(query, sessionID, cursor, sessionID, session.Branch, repoBranch, session.Role)
	if err != nil {
		return nil, fmt.Errorf("failed to query unread events: %w", err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

// UnreadCountForSession returns the total unread count and a breakdown by type.
func (db *DB) UnreadCountForSession(sessionID string) (int, map[string]int, error) {
	// Get cursor
	cursor, err := db.GetCursor(sessionID)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to get cursor: %w", err)
	}

	// Get session branch
	session, err := db.GetSession(sessionID)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to get session: %w", err)
	}
	if session == nil {
		return 0, nil, fmt.Errorf("session %q not found", sessionID)
	}

	// Count query (excludes watch_started — those are handler-only bookkeeping)
	query := `
		SELECT e.type, COUNT(DISTINCT e.id) as count
		FROM events e
		LEFT JOIN event_recipients er ON e.id = er.event_id
		LEFT JOIN event_resources eres ON e.id = eres.event_id
		LEFT JOIN subscriptions s ON s.resource_type = eres.resource_type AND s.resource_id = eres.resource_id AND s.session_id = ? AND s.deleted_at IS NULL
		WHERE e.ts > ?
		  ` + inboxExcludedTypesSQL + `
		  AND (
		    e.broadcast = 1
		    OR (er.recipient_type = 'session' AND er.recipient_value = ?)
		    OR (er.recipient_type = 'branch' AND (er.recipient_value = ? OR er.recipient_value = ?))
		    OR (er.recipient_type = 'role' AND er.recipient_value = ?)
		    OR s.id IS NOT NULL
		  )
		GROUP BY e.type
	`

	repoBranch := session.Repo + ":" + session.Branch
	rows, err := db.conn.Query(query, sessionID, cursor, sessionID, session.Branch, repoBranch, session.Role)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to count unread events: %w", err)
	}
	defer rows.Close()

	breakdown := make(map[string]int)
	total := 0
	for rows.Next() {
		var eventType string
		var count int
		if err := rows.Scan(&eventType, &count); err != nil {
			return 0, nil, fmt.Errorf("failed to scan count: %w", err)
		}
		breakdown[eventType] = count
		total += count
	}

	if err := rows.Err(); err != nil {
		return 0, nil, fmt.Errorf("error iterating counts: %w", err)
	}

	return total, breakdown, nil
}

// UnreadResourcesForSession returns the set of resource "type:id" strings that have unread events.
func (db *DB) UnreadResourcesForSession(sessionID string) (map[string]bool, error) {
	cursor, err := db.GetCursor(sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get cursor: %w", err)
	}
	session, err := db.GetSession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}
	if session == nil {
		return nil, fmt.Errorf("session %q not found", sessionID)
	}

	query := `
		SELECT DISTINCT eres.resource_type, eres.resource_id
		FROM events e
		LEFT JOIN event_recipients er ON e.id = er.event_id
		JOIN event_resources eres ON e.id = eres.event_id
		LEFT JOIN subscriptions s ON s.resource_type = eres.resource_type AND s.resource_id = eres.resource_id AND s.session_id = ? AND s.deleted_at IS NULL
		WHERE e.ts > ?
		  ` + inboxExcludedTypesSQL + `
		  AND (
		    e.broadcast = 1
		    OR (er.recipient_type = 'session' AND er.recipient_value = ?)
		    OR (er.recipient_type = 'branch' AND (er.recipient_value = ? OR er.recipient_value = ?))
		    OR (er.recipient_type = 'role' AND er.recipient_value = ?)
		    OR s.id IS NOT NULL
		  )
	`
	repoBranch := session.Repo + ":" + session.Branch
	rows, err := db.conn.Query(query, sessionID, cursor, sessionID, session.Branch, repoBranch, session.Role)
	if err != nil {
		return nil, fmt.Errorf("failed to query unread resources: %w", err)
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var resType, resID string
		if err := rows.Scan(&resType, &resID); err != nil {
			return nil, fmt.Errorf("failed to scan resource: %w", err)
		}
		result[resType+":"+resID] = true
	}
	return result, rows.Err()
}

// GlobalUnreadForSession returns ALL events since the session's cursor, regardless of targeting.
func (db *DB) GlobalUnreadForSession(sessionID string) ([]Event, error) {
	cursor, err := db.GetCursor(sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get cursor: %w", err)
	}

	query := `
		SELECT DISTINCT e.id, e.ts, e.external_ts, e.source, e.session_id, e.type, e.title, e.body, e.author, e.author_type, e.broadcast, e.tags
		FROM events e
		WHERE e.ts > ?
		ORDER BY e.ts ASC
	`

	rows, err := db.conn.Query(query, cursor)
	if err != nil {
		return nil, fmt.Errorf("failed to query global unread events: %w", err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

// HumanUnreadCountForSession returns the count of events unread by the human
// (using human_seen_ts cursor instead of last_seen_ts). This counts auto-delivered
// events as unread until the user has actually seen them.
func (db *DB) HumanUnreadCountForSession(sessionID string) (int, error) {
	cursor, err := db.GetHumanCursor(sessionID)
	if err != nil {
		return 0, err
	}

	session, err := db.GetSession(sessionID)
	if err != nil || session == nil {
		return 0, err
	}

	query := `
		SELECT COUNT(DISTINCT e.id)
		FROM events e
		LEFT JOIN event_recipients er ON e.id = er.event_id
		LEFT JOIN event_resources eres ON e.id = eres.event_id
		LEFT JOIN subscriptions s ON s.resource_type = eres.resource_type AND s.resource_id = eres.resource_id AND s.session_id = ? AND s.deleted_at IS NULL
		WHERE e.ts > ?
		  ` + inboxExcludedTypesSQL + `
		  AND (
		    e.broadcast = 1
		    OR (er.recipient_type = 'session' AND er.recipient_value = ?)
		    OR (er.recipient_type = 'branch' AND (er.recipient_value = ? OR er.recipient_value = ?))
		    OR (er.recipient_type = 'role' AND er.recipient_value = ?)
		    OR s.id IS NOT NULL
		  )
	`

	repoBranch := session.Repo + ":" + session.Branch
	var count int
	err = db.conn.QueryRow(query, sessionID, cursor, sessionID, session.Branch, repoBranch, session.Role).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// GlobalUnreadCountForSession returns the total count and breakdown by type of ALL events since the session's cursor.
func (db *DB) GlobalUnreadCountForSession(sessionID string) (int, map[string]int, error) {
	cursor, err := db.GetCursor(sessionID)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to get cursor: %w", err)
	}

	query := `
		SELECT e.type, COUNT(*) as count
		FROM events e
		WHERE e.ts > ?
		GROUP BY e.type
	`

	rows, err := db.conn.Query(query, cursor)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to count global unread events: %w", err)
	}
	defer rows.Close()

	breakdown := make(map[string]int)
	total := 0
	for rows.Next() {
		var eventType string
		var count int
		if err := rows.Scan(&eventType, &count); err != nil {
			return 0, nil, fmt.Errorf("failed to scan count: %w", err)
		}
		breakdown[eventType] = count
		total += count
	}

	if err := rows.Err(); err != nil {
		return 0, nil, fmt.Errorf("error iterating counts: %w", err)
	}

	return total, breakdown, nil
}

// DirectCountForSession returns the count of unread events directly addressed to a session
// (via event_recipients, not subscription routing).
func (db *DB) DirectCountForSession(sessionID string) (int, error) {
	cursor, err := db.GetCursor(sessionID)
	if err != nil {
		return 0, fmt.Errorf("failed to get cursor: %w", err)
	}
	if cursor == "" {
		cursor = "1970-01-01T00:00:00Z"
	}

	session, err := db.GetSession(sessionID)
	if err != nil {
		return 0, fmt.Errorf("failed to get session: %w", err)
	}
	if session == nil {
		return 0, nil
	}

	repoBranch := session.Repo + ":" + session.Branch
	var count int
	err = db.conn.QueryRow(`
		SELECT COUNT(DISTINCT e.id) FROM events e
		JOIN event_recipients er ON er.event_id = e.id
		WHERE e.ts > ?
		  AND (
		    (er.recipient_type = 'session' AND er.recipient_value = ?)
		    OR (er.recipient_type = 'branch' AND (er.recipient_value = ? OR er.recipient_value = ?))
		    OR (er.recipient_type = 'role' AND er.recipient_value = ?)
		  )
	`, cursor, sessionID, session.Branch, repoBranch, session.Role).Scan(&count)

	if err != nil {
		return 0, fmt.Errorf("failed to count direct events: %w", err)
	}

	return count, nil
}

// scanEvents is a helper that scans sql.Rows into []Event.
func scanEvents(rows *sql.Rows) ([]Event, error) {
	var events []Event
	for rows.Next() {
		var e Event
		var broadcastInt int
		if err := rows.Scan(&e.ID, &e.TS, &e.ExternalTS, &e.Source, &e.SessionID, &e.Type, &e.Title, &e.Body, &e.Author, &e.AuthorType, &broadcastInt, &e.Tags); err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}
		e.Broadcast = broadcastInt == 1
		events = append(events, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating events: %w", err)
	}

	return events, nil
}
