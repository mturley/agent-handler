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
	resourceFilter := r.URL.Query().Get("resource")
	typeFilter := r.URL.Query().Get("type")
	sourceFilter := r.URL.Query().Get("source")
	searchFilter := r.URL.Query().Get("search")

	limit := 50
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}

	// Build SQL query. The FROM is a UNION ALL of two arms projecting the
	// same 12 columns in the same order: handler's own `events` (agent arm,
	// has session_id/broadcast/session_name) and the watcher library's
	// `watcher_events` (github/jira events live only here post-migration;
	// the table has no session_id/broadcast/session_name, so those are
	// projected as NULL/0/'' literals). Wrapping it as a subquery aliased
	// `e` lets every existing filter below (ts, type, source, title/body,
	// ORDER BY) apply unchanged over the combined set — none of those
	// columns differ between the arms, and none of the arms contain
	// placeholders, so the existing `args` ordering is unaffected.
	query := `
		SELECT e.id, e.ts, e.source, e.session_id, e.session_name,
		       e.type, e.title, e.body, e.author, e.author_type, e.broadcast, e.tags
		FROM (
			SELECT e.id, e.ts, e.source, e.session_id, COALESCE(s.session_name, '') AS session_name,
			       e.type, e.title, e.body, e.author, e.author_type, e.broadcast, e.tags
			FROM events e
			LEFT JOIN sessions s ON e.session_id = s.session_id
			UNION ALL
			SELECT e.id, e.ts, e.source, NULL AS session_id, '' AS session_name,
			       e.type, e.title, e.body, e.author, e.author_type, 0 AS broadcast, e.tags
			FROM watcher_events e
		) e
		WHERE 1=1
	`
	args := []interface{}{}

	if before != "" {
		query += " AND e.ts < ?"
		args = append(args, before)
	}
	if sessionFilter != "" {
		// watcher_subscriptions has no session_id column — handler's
		// subscriptions are namespaced under a "handler:session:<id>"
		// subscriber string (see db/watcher_bridge.go's handlerSubscriber).
		// That helper is unexported and this is a different package, so the
		// prefix is duplicated here as a literal; keep it in sync by hand if
		// it ever changes (same tradeoff already made in
		// cmd/migrate_watcher.go's legacy-subscriptions copy).
		query += ` AND (e.session_id = ? OR e.id IN (
			SELECT er.event_id FROM (
				SELECT event_id, resource_type, resource_id FROM event_resources
				UNION ALL
				SELECT event_id, resource_type, resource_id FROM watcher_event_resources
			) er
			JOIN watcher_subscriptions sub ON er.resource_type = sub.resource_type AND er.resource_id = sub.resource_id
			WHERE sub.subscriber = ?
		))`
		args = append(args, sessionFilter, "handler:session:"+sessionFilter)
	}
	if resourceFilter != "" {
		parts := strings.SplitN(resourceFilter, ":", 2)
		if len(parts) == 2 {
			query += ` AND e.id IN (
				SELECT event_id FROM event_resources WHERE resource_type = ? AND resource_id = ?
				UNION ALL
				SELECT event_id FROM watcher_event_resources WHERE resource_type = ? AND resource_id = ?
			)`
			args = append(args, parts[0], parts[1], parts[0], parts[1])
		}
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
		 FROM (
			SELECT event_id, resource_type, resource_id, resource_url FROM event_resources
			UNION ALL
			SELECT event_id, resource_type, resource_id, resource_url FROM watcher_event_resources
		 ) er
		 LEFT JOIN watcher_resource_state rs ON er.resource_type = rs.resource_type AND er.resource_id = rs.resource_id
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
		if it := str("issue_type"); it != "" {
			meta["issue_type"] = it
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
