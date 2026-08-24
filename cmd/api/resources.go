package api

import (
	"encoding/json"
	"net/http"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
	"github.com/mturley/agent-handler/watcher"
	wdb "github.com/mturley/watcher/db"
)

type resourceSession struct {
	SessionID    string `json:"session_id"`
	SessionName  string `json:"session_name"`
	DisplayState string `json:"display_state"`
}

type resourceEntry struct {
	ResourceType      string                 `json:"resource_type"`
	ResourceID        string                 `json:"resource_id"`
	ResourceURL       string                 `json:"resource_url,omitempty"`
	State             map[string]interface{} `json:"state,omitempty"`
	ResourceUpdatedAt string                 `json:"resource_updated_at,omitempty"`
	WatcherUpdatedAt  string                 `json:"watcher_updated_at,omitempty"`
	CustomName        string                 `json:"custom_name,omitempty"`
	// DisplayTitle is what the UI should show for the resource: the custom
	// name if set, else the cached platform title (e.g. a Slack thread's
	// first message), else the resource id.
	DisplayTitle string            `json:"display_title,omitempty"`
	Sessions     []resourceSession `json:"sessions"`
}

type watcherStatusInfo struct {
	Configured  bool    `json:"configured"`
	Installed   bool    `json:"installed"`
	LastSuccess *string `json:"last_success,omitempty"`
	LastError   *string `json:"last_error,omitempty"`
	HasError    bool    `json:"has_error"`
}

type resourcesResponse struct {
	Resources []resourceEntry              `json:"resources"`
	Watchers  map[string]watcherStatusInfo `json:"watchers"`
}

func (s *Server) handleResources(w http.ResponseWriter, r *http.Request) {
	// Query all active subscriptions across all non-archived sessions, read
	// from the watcher library's watcher_subscriptions table. That table has
	// no session_id column — handler's subscriptions are namespaced under
	// db.HandlerSubscriberPrefix() (see db/watcher_bridge.go's
	// handlerSubscriber/sessionIDFromSubscriber), so the session id is
	// recovered with substr() and the subscriber is restricted to handler's
	// own prefix to exclude any non-handler subscriber.
	prefix := db.HandlerSubscriberPrefix()
	rows, err := s.DB.Query(`
		SELECT s.resource_type, s.resource_id, s.resource_url,
		       substr(s.subscriber, ?) AS session_id,
		       sess.session_name, sess.status, sess.pid, sess.last_prompt
		FROM watcher_subscriptions s
		INNER JOIN sessions sess ON sess.session_id = substr(s.subscriber, ?)
		WHERE s.deleted_at IS NULL AND s.subscriber LIKE ? AND sess.status != 'archived'
		ORDER BY s.resource_type, s.resource_id, s.created_at
	`, len(prefix)+1, len(prefix)+1, prefix+"%")
	if err != nil {
		s.Logger.Printf("Error querying subscriptions: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to query subscriptions")
		return
	}
	defer rows.Close()

	// Group subscriptions by resource
	resourceMap := make(map[string]*resourceEntry)
	type sessionRow struct {
		sessionID   string
		sessionName string
		status      string
		pid         int
		lastPrompt  string
	}
	resourceSessions := make(map[string][]sessionRow)

	for rows.Next() {
		var resourceType, resourceID, sessionID, sessionName, status, lastPrompt string
		var resourceURL *string
		var pid int

		if err := rows.Scan(&resourceType, &resourceID, &resourceURL, &sessionID, &sessionName, &status, &pid, &lastPrompt); err != nil {
			s.Logger.Printf("Error scanning subscription: %v", err)
			writeError(w, http.StatusInternalServerError, "Failed to scan subscription")
			return
		}

		resourceKey := resourceType + "::" + resourceID

		// Create resource entry if first time seeing this resource
		if _, exists := resourceMap[resourceKey]; !exists {
			entry := &resourceEntry{
				ResourceType: resourceType,
				ResourceID:   resourceID,
				Sessions:     []resourceSession{},
			}
			if resourceURL != nil {
				entry.ResourceURL = *resourceURL
			}
			resourceMap[resourceKey] = entry
		}

		// Track session for this resource
		resourceSessions[resourceKey] = append(resourceSessions[resourceKey], sessionRow{
			sessionID:   sessionID,
			sessionName: sessionName,
			status:      status,
			pid:         pid,
			lastPrompt:  lastPrompt,
		})
	}

	if err := rows.Err(); err != nil {
		s.Logger.Printf("Error iterating subscriptions: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to iterate subscriptions")
		return
	}

	// Fetch resource state and enrich each entry
	for resourceKey, entry := range resourceMap {
		// Fetch resource state
		state, err := wdb.GetResourceState(s.DB.Conn(), entry.ResourceType, entry.ResourceID)
		if err != nil {
			s.Logger.Printf("Error fetching state for %s: %v", resourceKey, err)
			continue
		}

		if state != nil {
			// Parse state JSON
			var stateMap map[string]interface{}
			if err := json.Unmarshal([]byte(state.StateJSON), &stateMap); err != nil {
				s.Logger.Printf("Error parsing state JSON for %s: %v", resourceKey, err)
			} else {
				entry.State = stateMap
			}
			entry.ResourceUpdatedAt = state.ResourceUpdatedAt
			entry.WatcherUpdatedAt = state.WatcherUpdatedAt
		}

		// Resolve the display title: custom name -> cached platform title
		// (e.g. a Slack thread's first message) -> resource id.
		if meta, err := wdb.GetResourceMeta(s.DB.Conn(), entry.ResourceType, entry.ResourceID); err != nil {
			s.Logger.Printf("Error fetching meta for %s: %v", resourceKey, err)
		} else if meta != nil {
			entry.CustomName = meta.CustomName
		}
		entry.DisplayTitle = resolveDisplayTitle(entry.CustomName, entry.State, entry.ResourceID)

		// Compute display_state for each session and add to Sessions list
		for _, sess := range resourceSessions[resourceKey] {
			displayState := computeDisplayState(sess.status, sess.pid, sess.sessionID, sess.lastPrompt)
			entry.Sessions = append(entry.Sessions, resourceSession{
				SessionID:    sess.sessionID,
				SessionName:  sess.sessionName,
				DisplayState: displayState,
			})
		}
	}

	// Convert map to slice
	resources := make([]resourceEntry, 0, len(resourceMap))
	for _, entry := range resourceMap {
		resources = append(resources, *entry)
	}

	// Fetch watcher status for every known watcher (github, jira, slack).
	watchers := make(map[string]watcherStatusInfo, len(watcher.KnownWatchers))
	for _, name := range watcher.KnownWatchers {
		watchers[name] = buildWatcherStatus(s.DB, name)
	}

	writeJSON(w, http.StatusOK, resourcesResponse{
		Resources: resources,
		Watchers:  watchers,
	})
}

// computeDisplayState computes the display state for a session.
// Extracted from enrichSession to avoid code duplication.
func computeDisplayState(status string, pid int, sessionID, lastPrompt string) string {
	if status == "archived" {
		return "archived"
	}
	processAlive := discover.IsSessionProcess(pid, sessionID)
	if !processAlive {
		return "dead"
	}
	return "idle"
}

// buildWatcherStatus builds watcher status info for a service.
// resolveDisplayTitle picks the best human-readable label for a resource:
// the user's custom name, else the cached platform title from the resource's
// state (e.g. a Slack thread's first message), else the raw resource id.
func resolveDisplayTitle(customName string, state map[string]interface{}, resourceID string) string {
	if customName != "" {
		return customName
	}
	if state != nil {
		if t, ok := state["title"].(string); ok && t != "" {
			return t
		}
	}
	return resourceID
}

func buildWatcherStatus(database *db.DB, service string) watcherStatusInfo {
	info := watcherStatusInfo{
		Configured: config.ServiceConfiguredForWatching(service),
		Installed:  watcher.IsInstalled(service),
		HasError:   false,
	}

	ws, err := wdb.GetPollerStatus(database.Conn(), service)
	if err == nil && ws != nil {
		if ws.LastSuccess != "" {
			info.LastSuccess = &ws.LastSuccess
		}
		if ws.LastError != "" {
			info.LastError = &ws.LastError
			info.HasError = wdb.HasPollerError(database.Conn(), service)
		}
	}

	return info
}
