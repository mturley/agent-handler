# Phase 5b: Timeline Tab Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Timeline tab to the web UI showing a chat-style chronological event feed with cursor-based infinite scroll, full filtering, expandable details, external resource links, and cross-tab navigation with the Sessions tab.

**Architecture:** New `GET /api/events` endpoint with cursor pagination and filtering. SSE stream enhanced with `events_new` event type. React frontend with chat-style timeline layout (vertical line + colored dots + event bubbles), filter bar, infinite scroll hook, and cross-tab navigation via shared state in App.tsx.

**Tech Stack:** Go (net/http), React 19, TypeScript, Tailwind CSS, shadcn/ui, lucide-react

## Global Constraints

- Go binary, pure-Go SQLite (`modernc.org/sqlite`)
- All timestamps ISO 8601 UTC
- Tests must pass: `go test ./...`
- Frontend must build: `cd ui && npm run build`
- Use `--signoff` on all commits
- Dark mode only, responsive to 400px width
- Follow existing patterns in `cmd/api/` and `ui/src/`

---

### Task 1: Events API Endpoint

**Files:**
- Create: `cmd/api/events.go`
- Modify: `cmd/api/server.go`

**Interfaces:**
- Consumes: `db.QueryEvents` (existing, but needs extension), `db.GetSession`, `db.DB.Query` (for raw SQL)
- Produces:
  - `GET /api/events` with query params: `before`, `limit`, `session`, `type`, `source`, `search`
  - JSON response: `{ "events": [...], "has_more": bool, "next_cursor": string }`
  - Each event includes `session_name` (resolved via join) and `resources` (from event_resources table)

- [ ] **Step 1: Create cmd/api/events.go**

Implement the `handleEvents` handler:

```go
package api

import (
	"net/http"
	"strconv"
	"strings"
)

type timelineEvent struct {
	ID         string              `json:"id"`
	TS         string              `json:"ts"`
	Source     string              `json:"source"`
	SessionID  *string             `json:"session_id"`
	SessionName string             `json:"session_name"`
	Type       string              `json:"type"`
	Title      string              `json:"title"`
	Body       *string             `json:"body"`
	Author     *string             `json:"author"`
	AuthorType *string             `json:"author_type"`
	Broadcast  bool                `json:"broadcast"`
	Tags       *string             `json:"tags"`
	Resources  []eventResourceInfo `json:"resources"`
}

type eventResourceInfo struct {
	ResourceType string  `json:"resource_type"`
	ResourceID   string  `json:"resource_id"`
	ResourceURL  *string `json:"resource_url"`
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
		query += " AND e.session_id = ?"
		args = append(args, sessionFilter)
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

	// Fetch resources for each event
	for i := range events {
		resRows, err := s.DB.Query(
			`SELECT resource_type, resource_id, resource_url FROM event_resources WHERE event_id = ?`,
			events[i].ID)
		if err == nil {
			defer resRows.Close()
			for resRows.Next() {
				var res eventResourceInfo
				if err := resRows.Scan(&res.ResourceType, &res.ResourceID, &res.ResourceURL); err == nil {
					events[i].Resources = append(events[i].Resources, res)
				}
			}
		}
		if events[i].Resources == nil {
			events[i].Resources = []eventResourceInfo{}
		}
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
```

- [ ] **Step 2: Register the route in server.go**

Add to `Start()`:
```go
mux.HandleFunc("GET /api/events", s.handleEvents)
```

- [ ] **Step 3: Build and test**

Run: `go build ./... && go test ./...`
Test manually: `curl "http://localhost:8420/api/events?limit=5" | python3 -m json.tool`

- [ ] **Step 4: Commit**

```bash
git add cmd/api/events.go cmd/api/server.go
git commit --signoff -m "feat: GET /api/events endpoint with cursor pagination and filtering"
```

---

### Task 2: SSE Events Enhancement

**Files:**
- Modify: `cmd/api/stream.go`

**Interfaces:**
- Consumes: `db.DB.QueryRow` (existing)
- Produces: SSE `events_new` event type pushed when new events are detected

- [ ] **Step 1: Update stream.go to track and push new events**

Add event tracking to the SSE handler. Track the latest event timestamp, and on each tick check if there are newer events:

```go
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher.Flush()

	// Track latest event timestamp
	var lastEventTS string
	s.DB.QueryRow("SELECT MAX(ts) FROM events").Scan(&lastEventTS)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			// Check for new events
			var currentMaxTS string
			s.DB.QueryRow("SELECT MAX(ts) FROM events").Scan(&currentMaxTS)

			if currentMaxTS != "" && currentMaxTS != lastEventTS {
				lastEventTS = currentMaxTS
				data, _ := json.Marshal(map[string]string{"type": "events_new"})
				fmt.Fprintf(w, "event: events_new\ndata: %s\n\n", data)
			}

			// Always send heartbeat
			data, _ := json.Marshal(map[string]string{"type": "heartbeat"})
			fmt.Fprintf(w, "event: heartbeat\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}
```

- [ ] **Step 2: Build and test**

Run: `go build ./...`
Test: `curl -N http://localhost:8420/api/stream` — should see heartbeat events, and `events_new` when a new event is emitted (e.g. `handler emit --type status --title "test"`)

- [ ] **Step 3: Commit**

```bash
git add cmd/api/stream.go
git commit --signoff -m "feat: SSE events_new event type for live timeline updates"
```

---

### Task 3: Frontend Data Layer + Types

**Files:**
- Modify: `ui/src/api/types.ts`
- Modify: `ui/src/api/client.ts`
- Create: `ui/src/hooks/useTimeline.ts`
- Modify: `ui/src/hooks/useSSE.ts`

**Interfaces:**
- Consumes: `GET /api/events` from Task 1, SSE `events_new` from Task 2
- Produces:
  - `TimelineEvent` type (extends Event with session_name, resources)
  - `getEvents(params)` API client function
  - `useTimeline()` hook with filtering, cursor pagination, SSE integration
  - `useSSE` enhanced to support `events_new` callback

- [ ] **Step 1: Add types to types.ts**

```typescript
export interface EventResource {
  resource_type: string
  resource_id: string
  resource_url?: string
}

export interface TimelineEvent extends Event {
  session_name: string
  resources: EventResource[]
}

export interface EventsResponse {
  events: TimelineEvent[]
  has_more: boolean
  next_cursor: string
}
```

- [ ] **Step 2: Add getEvents to client.ts**

```typescript
export interface EventsParams {
  before?: string
  limit?: number
  session?: string
  type?: string
  source?: string
  search?: string
}

export async function getEvents(params: EventsParams = {}): Promise<EventsResponse> {
  const searchParams = new URLSearchParams()
  if (params.before) searchParams.set("before", params.before)
  if (params.limit) searchParams.set("limit", String(params.limit))
  if (params.session) searchParams.set("session", params.session)
  if (params.type) searchParams.set("type", params.type)
  if (params.source) searchParams.set("source", params.source)
  if (params.search) searchParams.set("search", params.search)
  const qs = searchParams.toString()
  return fetchJSON<EventsResponse>(`/api/events${qs ? `?${qs}` : ""}`)
}
```

- [ ] **Step 3: Update useSSE to support events_new callback**

Update `useSSE` to accept an optional second callback for `events_new`:

```typescript
export function useSSE(onHeartbeat: () => void, onEventsNew?: () => void) {
  const heartbeatRef = useRef(onHeartbeat)
  heartbeatRef.current = onHeartbeat
  const eventsNewRef = useRef(onEventsNew)
  eventsNewRef.current = onEventsNew

  useEffect(() => {
    let es: EventSource | null = null
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null

    function connect() {
      es = new EventSource("/api/stream")

      es.addEventListener("heartbeat", () => {
        heartbeatRef.current()
      })

      es.addEventListener("events_new", () => {
        eventsNewRef.current?.()
      })

      es.onerror = () => {
        es?.close()
        es = null
        reconnectTimer = setTimeout(connect, 3000)
      }
    }

    connect()

    return () => {
      es?.close()
      if (reconnectTimer) clearTimeout(reconnectTimer)
    }
  }, [])
}
```

- [ ] **Step 4: Create useTimeline hook**

Create `ui/src/hooks/useTimeline.ts`:

```typescript
import { useState, useCallback, useEffect, useRef } from "react"
import type { TimelineEvent } from "@/api/types"
import { getEvents, type EventsParams } from "@/api/client"

export interface TimelineFilters {
  session?: string
  types?: string[]
  source?: string
  search?: string
}

export function useTimeline() {
  const [events, setEvents] = useState<TimelineEvent[]>([])
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [hasMore, setHasMore] = useState(true)
  const [nextCursor, setNextCursor] = useState<string | null>(null)
  const [filters, setFilters] = useState<TimelineFilters>({})
  const filtersRef = useRef(filters)
  filtersRef.current = filters

  const buildParams = useCallback((cursor?: string): EventsParams => {
    const params: EventsParams = { limit: 50 }
    if (cursor) params.before = cursor
    if (filtersRef.current.session) params.session = filtersRef.current.session
    if (filtersRef.current.types?.length) params.type = filtersRef.current.types.join(",")
    if (filtersRef.current.source) params.source = filtersRef.current.source
    if (filtersRef.current.search) params.search = filtersRef.current.search
    return params
  }, [])

  // Initial load + filter change reload
  const loadInitial = useCallback(async () => {
    setLoading(true)
    try {
      const data = await getEvents(buildParams())
      setEvents(data.events)
      setHasMore(data.has_more)
      setNextCursor(data.next_cursor || null)
    } catch (err) {
      console.error("Failed to load events:", err)
    } finally {
      setLoading(false)
    }
  }, [buildParams])

  useEffect(() => {
    loadInitial()
  }, [loadInitial, filters])

  // Load more (infinite scroll)
  const loadMore = useCallback(async () => {
    if (loadingMore || !hasMore || !nextCursor) return
    setLoadingMore(true)
    try {
      const data = await getEvents(buildParams(nextCursor))
      setEvents(prev => [...prev, ...data.events])
      setHasMore(data.has_more)
      setNextCursor(data.next_cursor || null)
    } catch (err) {
      console.error("Failed to load more events:", err)
    } finally {
      setLoadingMore(false)
    }
  }, [loadingMore, hasMore, nextCursor, buildParams])

  // SSE: prepend new events
  const handleNewEvents = useCallback(async () => {
    const params = buildParams()
    // Only fetch events newer than the newest we have
    if (events.length > 0) {
      // Fetch without cursor to get latest, then prepend any we don't have
      const data = await getEvents({ ...params, limit: 20 })
      const existingIds = new Set(events.map(e => e.id))
      const newEvents = data.events.filter(e => !existingIds.has(e.id))
      if (newEvents.length > 0) {
        setEvents(prev => [...newEvents, ...prev])
      }
    }
  }, [events, buildParams])

  const updateFilters = useCallback((newFilters: Partial<TimelineFilters>) => {
    setFilters(prev => ({ ...prev, ...newFilters }))
  }, [])

  const clearFilters = useCallback(() => {
    setFilters({})
  }, [])

  return {
    events,
    loading,
    loadingMore,
    hasMore,
    loadMore,
    handleNewEvents,
    filters,
    updateFilters,
    clearFilters,
  }
}
```

- [ ] **Step 5: Build**

Run: `cd ui && npm run build`
Expected: compiles successfully.

- [ ] **Step 6: Commit**

```bash
git add ui/src/api/types.ts ui/src/api/client.ts ui/src/hooks/useTimeline.ts ui/src/hooks/useSSE.ts
git commit --signoff -m "feat: timeline data layer with cursor pagination, filtering, and SSE"
```

---

### Task 4: Timeline UI Components

**Files:**
- Create: `ui/src/pages/TimelinePage.tsx`
- Create: `ui/src/components/TimelineEvent.tsx`
- Create: `ui/src/components/TimelineFilters.tsx`
- Create: `ui/src/utils/eventColors.ts`
- Modify: `ui/src/App.tsx`

**Interfaces:**
- Consumes: `useTimeline()` from Task 3, `useSSE` from Task 3, `TimelineEvent` type, `formatEventType` from `ui/src/utils/formatLabel.ts` (existing), `timeAgo` from `ui/src/utils/timeAgo.ts` (existing)
- Produces: Complete Timeline tab with chat-style feed, filter bar, infinite scroll

- [ ] **Step 1: Create eventColors.ts**

```typescript
export function eventDotColor(type: string): string {
  switch (type) {
    case "milestone": return "bg-blue-500"
    case "decision": return "bg-purple-500"
    case "status": return "bg-gray-400"
    case "blocked": case "unblocked": return "bg-amber-500"
    case "message": return "bg-indigo-500"
    case "pr_comment": case "pr_review_comment": case "pr_approved": return "bg-green-500"
    case "pr_merged": case "pr_closed": return "bg-gray-400"
    case "ci_check_passed": return "bg-green-500"
    case "ci_check_failed": return "bg-red-500"
    case "jira_comment": case "jira_status_change": case "jira_assigned": return "bg-blue-500"
    case "handoff": case "followup": return "bg-orange-500"
    case "session_end": return "bg-gray-400"
    default: return "bg-gray-400"
  }
}

export function eventBadgeVariant(type: string): string {
  switch (type) {
    case "milestone": return "bg-blue-500/20 text-blue-300 border-blue-500/30"
    case "decision": return "bg-purple-500/20 text-purple-300 border-purple-500/30"
    case "blocked": case "unblocked": return "bg-amber-500/20 text-amber-300 border-amber-500/30"
    case "message": return "bg-indigo-500/20 text-indigo-300 border-indigo-500/30"
    case "ci_check_failed": return "bg-red-500/20 text-red-300 border-red-500/30"
    case "ci_check_passed": case "pr_approved": return "bg-green-500/20 text-green-300 border-green-500/30"
    case "handoff": case "followup": return "bg-orange-500/20 text-orange-300 border-orange-500/30"
    default: return "bg-gray-500/20 text-gray-300 border-gray-500/30"
  }
}
```

- [ ] **Step 2: Create TimelineEvent.tsx**

The individual event bubble component. Shows:
- Colored dot on timeline line
- Type badge, title (bold), relative timestamp
- Session name (clickable), author
- Expandable body on click
- Resource links (PR → GitHub, Jira → Jira URL)

Use shadcn Card for the bubble. Use lucide-react icons (ChevronRight/ChevronDown for expand, ExternalLink for resource links). Use `timeAgo` for timestamps, `formatEventType` for type labels.

The component receives an `onSessionClick` callback prop for cross-tab navigation (wired in Task 5).

- [ ] **Step 3: Create TimelineFilters.tsx**

Filter bar with:
- Session select (shadcn Select, "All sessions" default, lists sessions from `/api/sessions`)
- Source chips (Agent, GitHub, Jira, Handler — toggleable badges)
- Free-text search (shadcn Input)
- Type filter grouped by source category (use shadcn badges, multi-select toggle)

Read the existing `SessionsPage.tsx` filter chip pattern for reference.

- [ ] **Step 4: Create TimelinePage.tsx**

Wire everything together:
- TimelineFilters at top
- Chat-style feed below with vertical timeline line (CSS: `border-l-2 border-muted ml-3`)
- Events rendered as TimelineEvent components
- Infinite scroll: observe the last element with IntersectionObserver, call `loadMore`
- SSE integration: call `handleNewEvents` on `events_new`
- Loading spinner at bottom when loading more
- "No more events" when hasMore is false

- [ ] **Step 5: Wire into App.tsx**

Replace the Timeline placeholder with `<TimelinePage />`. Pass SSE events_new via the `useSSE` hook.

- [ ] **Step 6: Build and verify**

Run: `cd ui && npm run build`
Test in dev mode: `make dev`, navigate to Timeline tab, verify events render.

- [ ] **Step 7: Commit**

```bash
git add ui/src/pages/TimelinePage.tsx ui/src/components/TimelineEvent.tsx ui/src/components/TimelineFilters.tsx ui/src/utils/eventColors.ts ui/src/App.tsx
git commit --signoff -m "feat: Timeline tab with chat-style feed, filtering, and infinite scroll"
```

---

### Task 5: Cross-tab Navigation

**Files:**
- Modify: `ui/src/App.tsx`
- Modify: `ui/src/components/SessionCard.tsx`
- Modify: `ui/src/pages/TimelinePage.tsx`
- Modify: `ui/src/pages/SessionsPage.tsx`

**Interfaces:**
- Consumes: All from Tasks 3-4
- Produces: Bidirectional navigation between Sessions and Timeline tabs

- [ ] **Step 1: Add shared navigation state to App.tsx**

Lift the tab state from shadcn's `defaultValue` to a controlled `value` state in App. Add state for timeline session filter and sessions search query:

```typescript
const [activeTab, setActiveTab] = useState("sessions")
const [timelineSessionFilter, setTimelineSessionFilter] = useState<string | undefined>()
const [sessionsSearchQuery, setSessionsSearchQuery] = useState<string | undefined>()
```

Pass these as props to SessionsPage and TimelinePage. When TimelinePage receives `timelineSessionFilter`, it pre-sets the session filter. When SessionsPage receives `sessionsSearchQuery`, it pre-sets the search.

Use `<Tabs value={activeTab} onValueChange={setActiveTab}>` for controlled tabs.

- [ ] **Step 2: Add "Timeline" button to SessionCard**

Add a Clock icon button to each session card. On click:
1. Call `onTimelineClick(session.session_id)` callback prop
2. Parent (SessionsPage) calls up to App which sets `timelineSessionFilter` and switches to the Timeline tab

- [ ] **Step 3: Add session name click handler to TimelineEvent**

The session name in each event bubble is already clickable (from Task 4). Wire `onSessionClick(sessionName)` callback:
1. Parent (TimelinePage) calls up to App which sets `sessionsSearchQuery` and switches to the Sessions tab

- [ ] **Step 4: Clear navigation state on tab switch**

When the user manually clicks a tab (not via cross-navigation), clear the filter/search overrides so they don't persist unexpectedly.

- [ ] **Step 5: Build and verify**

Run: `cd ui && npm run build`
Test in dev mode:
1. On Sessions tab, click a session's Timeline button → Timeline tab opens filtered by that session
2. On Timeline tab, click a session name on an event → Sessions tab opens with that session searched

- [ ] **Step 6: Commit**

```bash
git add ui/src/App.tsx ui/src/components/SessionCard.tsx ui/src/pages/TimelinePage.tsx ui/src/pages/SessionsPage.tsx
git commit --signoff -m "feat: cross-tab navigation between Sessions and Timeline"
```
