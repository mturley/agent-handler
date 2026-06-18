package watcher

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/mturley/agent-handler/db"
)

// Resource represents a resource that watchers monitor.
type Resource struct {
	ResourceType string
	ResourceID   string
	ResourceURL  string
}

// ActiveResources returns resources of the given type that have active subscriptions.
// A resource is active if at least one session with status='active' has a non-deleted subscription to it.
func ActiveResources(d *db.DB, resourceType string) ([]Resource, error) {
	query := `
		SELECT DISTINCT sub.resource_type, sub.resource_id, COALESCE(sub.resource_url, '')
		FROM subscriptions sub
		JOIN sessions s ON s.session_id = sub.session_id
		WHERE sub.deleted_at IS NULL AND sub.resource_type = ? AND s.status = 'active'
	`

	rows, err := d.Query(query, resourceType)
	if err != nil {
		return nil, fmt.Errorf("failed to query active resources: %w", err)
	}
	defer rows.Close()

	var resources []Resource
	for rows.Next() {
		var r Resource
		if err := rows.Scan(&r.ResourceType, &r.ResourceID, &r.ResourceURL); err != nil {
			return nil, fmt.Errorf("failed to scan resource: %w", err)
		}
		resources = append(resources, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating resources: %w", err)
	}

	return resources, nil
}

// EventCursor returns the maximum external_ts from events for the given source, resource type, and resource ID.
// Returns empty string if no events exist.
func EventCursor(d *db.DB, source, resourceType, resourceID string) string {
	query := `
		SELECT MAX(e.external_ts)
		FROM events e
		JOIN event_resources er ON er.event_id = e.id
		WHERE e.source = ? AND er.resource_type = ? AND er.resource_id = ?
	`

	var cursor *string
	err := d.QueryRow(query, source, resourceType, resourceID).Scan(&cursor)
	if err != nil || cursor == nil {
		return ""
	}

	return *cursor
}

// IsDuplicate checks if an event with the given source, resource type, resource ID, event type, and external timestamp already exists.
func IsDuplicate(d *db.DB, source, resourceType, resourceID, eventType, externalTS string) bool {
	query := `
		SELECT 1
		FROM events e
		JOIN event_resources er ON er.event_id = e.id
		WHERE e.source = ? AND er.resource_type = ? AND er.resource_id = ? AND e.type = ? AND e.external_ts = ?
		LIMIT 1
	`

	var exists int
	err := d.QueryRow(query, source, resourceType, resourceID, eventType, externalTS).Scan(&exists)
	return err == nil
}

// EmitWatcherEvent inserts a watcher event with event_resources.
func EmitWatcherEvent(d *db.DB, source, eventType, title string, body *string, externalTS string, author, authorType *string, resource Resource) error {
	event := db.Event{
		ID:         uuid.New().String(),
		TS:         time.Now().UTC().Format(time.RFC3339),
		ExternalTS: &externalTS,
		Source:     source,
		SessionID:  nil,
		Type:       eventType,
		Title:      title,
		Body:       body,
		Author:     author,
		AuthorType: authorType,
		Broadcast:  false,
		Tags:       nil,
	}

	resources := []db.EventResource{
		{
			ResourceType: resource.ResourceType,
			ResourceID:   resource.ResourceID,
			ResourceURL:  strPtr(resource.ResourceURL),
		},
	}

	if err := d.InsertEvent(event, nil, resources); err != nil {
		return fmt.Errorf("failed to insert watcher event: %w", err)
	}

	return nil
}

// EmitWatcherError inserts a watcher_error event with event_resources.
// Skips emitting if the watcher is already in error state with the same message.
func EmitWatcherError(d *db.DB, source, title string, body *string, resource Resource) error {
	if body != nil {
		ws, err := d.GetWatcherStatus(source)
		if err == nil && ws != nil && ws.LastErrorMessage == *body && d.HasWatcherError(source) {
			return nil
		}
	}

	event := db.Event{
		ID:         uuid.New().String(),
		TS:         time.Now().UTC().Format(time.RFC3339),
		ExternalTS: nil,
		Source:     source,
		SessionID:  nil,
		Type:       "watcher_error",
		Title:      title,
		Body:       body,
		Author:     nil,
		AuthorType: nil,
		Broadcast:  false,
		Tags:       nil,
	}

	resources := []db.EventResource{
		{
			ResourceType: resource.ResourceType,
			ResourceID:   resource.ResourceID,
			ResourceURL:  strPtr(resource.ResourceURL),
		},
	}

	if err := d.InsertEvent(event, nil, resources); err != nil {
		return fmt.Errorf("failed to insert watcher error event: %w", err)
	}

	return nil
}

// OpenLog opens an append-only log file for the named watcher.
// Log files are stored at ~/.agent-handler/data/logs/watcher-<name>.log
func OpenLog(watcherName string) *log.Logger {
	logDir := filepath.Join(db.HandlerHome(), "data", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		// Fall back to stderr if we can't create the log directory
		return log.New(os.Stderr, fmt.Sprintf("[%s] ", watcherName), log.LstdFlags)
	}

	logPath := filepath.Join(logDir, fmt.Sprintf("watcher-%s.log", watcherName))
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		// Fall back to stderr if we can't open the log file
		return log.New(os.Stderr, fmt.Sprintf("[%s] ", watcherName), log.LstdFlags)
	}

	return log.New(logFile, "", log.LstdFlags)
}

// strPtr converts a string to a string pointer.
// Returns nil if the string is empty.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
