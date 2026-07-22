package api

import (
	"encoding/json"
	"net/http"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
	"github.com/mturley/agent-handler/watcher"
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
	Sessions          []resourceSession      `json:"sessions"`
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
	// Query all active subscriptions across all non-archived sessions
	rows, err := s.DB.Query(`
		SELECT s.resource_type, s.resource_id, s.resource_url, s.session_id, sess.session_name, sess.status, sess.pid, sess.last_prompt
		FROM subscriptions s
		INNER JOIN sessions sess ON sess.session_id = s.session_id
		WHERE s.deleted_at IS NULL AND sess.status != 'archived'
		ORDER BY s.resource_type, s.resource_id, s.created_at
	`)
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
		state, err := s.DB.GetResourceState(entry.ResourceType, entry.ResourceID)
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

	// Fetch watcher status for both github and jira
	cfg, err := config.Read(config.DefaultPath())
	if err != nil {
		s.Logger.Printf("Error reading config: %v", err)
		cfg = &config.Config{}
	}

	watchers := map[string]watcherStatusInfo{
		"github": buildWatcherStatus(s.DB, cfg, "github"),
		"jira":   buildWatcherStatus(s.DB, cfg, "jira"),
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
func buildWatcherStatus(database *db.DB, cfg *config.Config, service string) watcherStatusInfo {
	info := watcherStatusInfo{
		Configured: cfg.IsServiceConfigured(service),
		Installed:  watcher.IsInstalled(service),
		HasError:   false,
	}

	ws, err := database.GetWatcherStatus(service)
	if err == nil && ws != nil {
		if ws.LastSuccess != "" {
			info.LastSuccess = &ws.LastSuccess
		}
		if ws.LastError != "" {
			info.LastError = &ws.LastError
			info.HasError = database.HasWatcherError(service)
		}
	}

	return info
}
