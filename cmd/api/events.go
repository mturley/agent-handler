package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type timelineEvent struct {
	ID          string              `json:"id"`
	TS          string              `json:"ts"`
	Source      string              `json:"source"`
	SessionID   *string             `json:"session_id"`
	SessionName string              `json:"session_name"`
	Type        string              `json:"type"`
	Title       string              `json:"title"`
	Body        *string             `json:"body"`
	Author      *string             `json:"author"`
	AuthorType  *string             `json:"author_type"`
	Broadcast   bool                `json:"broadcast"`
	Tags        *string             `json:"tags"`
	Resources   []eventResourceInfo `json:"resources"`
}

type eventResourceInfo struct {
	ResourceType string            `json:"resource_type"`
	ResourceID   string            `json:"resource_id"`
	ResourceURL  *string           `json:"resource_url"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type eventsResponse struct {
	Events     []timelineEvent `json:"events"`
	HasMore    bool            `json:"has_more"`
	NextCursor string          `json:"next_cursor"`
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	// Parse query params
	before := r.URL.Query().Get("before")
	limitStr := r.URL.Query().Get("limit")
	sessionFilter := r.URL.Query().Get("session")
	typeFilter := r.URL.Query().Get("type")
	sourceFilter := r.URL.Query().Get("source")
	searchFilter := r.URL.Query().Get("search")

	limit := 50
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}

	// Build SQL query
	query := `
		SELECT e.id, e.ts, e.source, e.session_id, COALESCE(s.session_name, ''),
		       e.type, e.title, e.body, e.author, e.author_type, e.broadcast, e.tags
		FROM events e
		LEFT JOIN sessions s ON e.session_id = s.session_id
		WHERE 1=1
	`
	args := []interface{}{}

	if before != "" {
		query += " AND e.ts < ?"
		args = append(args, before)
	}
	if sessionFilter != "" {
		query += ` AND (e.session_id = ? OR e.id IN (
			SELECT er.event_id FROM event_resources er
			JOIN subscriptions sub ON er.resource_type = sub.resource_type AND er.resource_id = sub.resource_id
			WHERE sub.session_id = ?
		))`
		args = append(args, sessionFilter, sessionFilter)
	}
	if typeFilter != "" {
		types := strings.Split(typeFilter, ",")
		placeholders := make([]string, len(types))
		for i, t := range types {
			placeholders[i] = "?"
			args = append(args, strings.TrimSpace(t))
		}
		query += " AND e.type IN (" + strings.Join(placeholders, ",") + ")"
	}
	if sourceFilter != "" {
		query += " AND e.source = ?"
		args = append(args, sourceFilter)
	}
	if searchFilter != "" {
		query += " AND (e.title LIKE ? OR e.body LIKE ?)"
		searchTerm := "%" + searchFilter + "%"
		args = append(args, searchTerm, searchTerm)
	}

	// Fetch limit+1 to determine has_more
	query += " ORDER BY e.ts DESC LIMIT ?"
	args = append(args, limit+1)

	rows, err := s.DB.Query(query, args...)
	if err != nil {
		s.Logger.Printf("Error querying events: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to query events")
		return
	}
	defer rows.Close()

	var events []timelineEvent
	for rows.Next() {
		var evt timelineEvent
		var broadcastInt int
		if err := rows.Scan(&evt.ID, &evt.TS, &evt.Source, &evt.SessionID, &evt.SessionName,
			&evt.Type, &evt.Title, &evt.Body, &evt.Author, &evt.AuthorType, &broadcastInt, &evt.Tags); err != nil {
			s.Logger.Printf("Error scanning event: %v", err)
			continue
		}
		evt.Broadcast = broadcastInt == 1
		events = append(events, evt)
	}

	// Determine has_more and trim to limit
	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}

	// Fetch resources for each event using a helper to avoid defer in loop
	for i := range events {
		resources, err := s.fetchEventResources(events[i].ID)
		if err != nil {
			s.Logger.Printf("Error fetching resources for event %s: %v", events[i].ID, err)
			events[i].Resources = []eventResourceInfo{}
		} else {
			events[i].Resources = resources
		}
	}

	if events == nil {
		events = []timelineEvent{}
	}

	// Build response
	resp := eventsResponse{
		Events:  events,
		HasMore: hasMore,
	}
	if len(events) > 0 {
		resp.NextCursor = events[len(events)-1].TS
	}

	writeJSON(w, http.StatusOK, resp)
}

// fetchEventResources retrieves all resources for a given event ID.
// Returns an empty slice if no resources found (never nil).
func (s *Server) fetchEventResources(eventID string) ([]eventResourceInfo, error) {
	rows, err := s.DB.Query(
		`SELECT er.resource_type, er.resource_id, er.resource_url, rs.state_json
		 FROM event_resources er
		 LEFT JOIN resource_state rs ON er.resource_type = rs.resource_type AND er.resource_id = rs.resource_id
		 WHERE er.event_id = ?`,
		eventID)
	if err != nil {
		return []eventResourceInfo{}, err
	}
	defer rows.Close()

	var resources []eventResourceInfo
	for rows.Next() {
		var res eventResourceInfo
		var stateJSON *string
		if err := rows.Scan(&res.ResourceType, &res.ResourceID, &res.ResourceURL, &stateJSON); err != nil {
			return resources, err
		}
		if stateJSON != nil {
			res.Metadata = extractResourceMetadata(res.ResourceType, *stateJSON)
		}
		resources = append(resources, res)
	}

	if err := rows.Err(); err != nil {
		return resources, err
	}

	// Always return a slice, never nil
	if resources == nil {
		resources = []eventResourceInfo{}
	}

	return resources, nil
}

func extractResourceMetadata(resourceType, stateJSON string) map[string]string {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(stateJSON), &raw); err != nil {
		return nil
	}

	str := func(key string) string {
		if v, ok := raw[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}

	meta := map[string]string{}

	switch resourceType {
	case "pr":
		if t := str("title"); t != "" {
			meta["title"] = t
		}
		if a := str("author"); a != "" {
			meta["author"] = a
		}
		if s := str("state"); s != "" {
			meta["state"] = s
		}
	case "jira":
		if t := str("summary"); t != "" {
			meta["title"] = t
		}
		if a := str("assignee"); a != "" {
			meta["assignee"] = a
		}
		if p := str("priority"); p != "" {
			meta["priority"] = p
		}
		if s := str("status"); s != "" {
			meta["status"] = s
		}
	}

	if len(meta) == 0 {
		return nil
	}
	return meta
}
