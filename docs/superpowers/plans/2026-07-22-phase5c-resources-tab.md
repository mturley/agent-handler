# Phase 5c: External Resources Tab Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an External Resources tab to the web UI showing watched PRs and Jira issues with cached state, subscribing sessions, watcher health, and cross-tab navigation.

**Architecture:** New `GET /api/resources` endpoint joins resource_state, subscriptions, and watcher_status tables. Frontend renders resource cards grouped by type with sort controls, expandable details, and session Switch buttons.

**Tech Stack:** Go (net/http), React 19, TypeScript, Tailwind CSS, shadcn/ui, lucide-react, TanStack Query

## Global Constraints

- Go binary, pure-Go SQLite (`modernc.org/sqlite`)
- All timestamps ISO 8601 UTC
- Tests must pass: `go test ./...`
- Frontend must build: `cd ui && npm run build`
- Use `--signoff` on all commits
- Dark mode only, responsive to 400px width
- Follow existing patterns in `cmd/api/` and `ui/src/`
- Use TanStack Query for data fetching (already adopted in the project)

---

### Task 1: Resources API Endpoint

**Files:**
- Create: `cmd/api/resources.go`
- Modify: `cmd/api/server.go`

**Interfaces:**
- Consumes: `db.ListSubscriptions`, `db.GetResourceState`, `db.GetSession`, `db.GetWatcherStatus`, `config.IsServiceConfigured`, `watcher.IsInstalled` (all existing)
- Produces: `GET /api/resources` returning resources with state, subscribed sessions, and watcher status

- [ ] **Step 1: Create cmd/api/resources.go**

Implement `handleResources`:
1. Query all active subscriptions (across all non-archived sessions)
2. Deduplicate by resource — group subscriptions by (resource_type, resource_id)
3. For each unique resource, fetch resource_state and build the response
4. For each subscription, include the session info (id, name, display_state)
5. Fetch watcher_status for github and jira
6. Return the combined response

Key types:
```go
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
    Configured bool    `json:"configured"`
    Installed  bool    `json:"installed"`
    LastSuccess *string `json:"last_success"`
    LastError   *string `json:"last_error"`
    HasError   bool    `json:"has_error"`
}
```

Use `encoding/json` to parse `state_json` from resource_state into `map[string]interface{}`.

- [ ] **Step 2: Register route in server.go**

Add: `mux.HandleFunc("GET /api/resources", s.handleResources)`

- [ ] **Step 3: Build and test**

Run: `go build ./... && go test ./...`
Test: `curl http://localhost:8420/api/resources | python3 -m json.tool`

- [ ] **Step 4: Commit**

```bash
git add cmd/api/resources.go cmd/api/server.go
git commit --signoff -m "feat: GET /api/resources endpoint with state and session data"
```

---

### Task 2: Frontend Types and Data Layer

**Files:**
- Modify: `ui/src/api/types.ts`
- Modify: `ui/src/api/client.ts`
- Create: `ui/src/hooks/useResources.ts`

**Interfaces:**
- Consumes: API from Task 1, TanStack Query (already in project)
- Produces: `useResources()` hook with resources, watcher status, sort controls

- [ ] **Step 1: Add types to types.ts**

```typescript
interface ResourceSession {
  session_id: string
  session_name: string
  display_state: string
}

interface ResourceEntry {
  resource_type: string
  resource_id: string
  resource_url?: string
  state?: Record<string, unknown>
  resource_updated_at?: string
  watcher_updated_at?: string
  sessions: ResourceSession[]
}

interface WatcherStatusInfo {
  configured: boolean
  installed: boolean
  last_success?: string
  last_error?: string
  has_error: boolean
}

interface ResourcesResponse {
  resources: ResourceEntry[]
  watcher_status: Record<string, WatcherStatusInfo>
}
```

- [ ] **Step 2: Add getResources to client.ts**

```typescript
export async function getResources(): Promise<ResourcesResponse> {
  return fetchJSON<ResourcesResponse>("/api/resources")
}
```

- [ ] **Step 3: Create useResources hook**

Uses TanStack Query to fetch resources, refetches on SSE heartbeat. Exposes:
- `resources` — raw data
- `watcherStatus` — watcher health info
- `prResources` / `jiraResources` — filtered by type
- `sortField` / `setSortField` — "urgency" (default) or "recent"
- Sort logic: urgency puts failing CI / changes requested / blocked items first

- [ ] **Step 4: Verify build**

Run: `cd ui && npm run build`

- [ ] **Step 5: Commit**

```bash
git add ui/src/api/types.ts ui/src/api/client.ts ui/src/hooks/useResources.ts
git commit --signoff -m "feat: resources data layer with types, API client, and useResources hook"
```

---

### Task 3: Resources UI Components

**Files:**
- Create: `ui/src/pages/ResourcesPage.tsx`
- Create: `ui/src/components/ResourceCard.tsx`
- Create: `ui/src/components/WatcherStatus.tsx`
- Modify: `ui/src/App.tsx`

**Interfaces:**
- Consumes: `useResources` from Task 2, navigation callbacks from App.tsx, `switchSession` from client.ts
- Produces: Complete Resources tab

- [ ] **Step 1: Create WatcherStatus component**

Small status bar at top:
- For each watcher (github, jira): show name, green checkmark or red X, "last run N ago"
- If has_error: show error message in red
- If not configured: show "not configured" in gray
- Use lucide-react icons (CheckCircle, XCircle, AlertCircle)

- [ ] **Step 2: Create ResourceCard component**

**PR card:**
- Title (bold) with ExternalLink icon linking to resource_url
- Badges: state (open/merged/closed), review_decision, ci_status
- Author name
- Session pills: small badges with session names, Switch button on each (cmux only)
- Expandable section: has_new_commits_since_review, is_draft, watcher freshness (watcher_updated_at as relative time)

**Jira card:**
- Summary (bold) with ExternalLink icon
- Badges: status, priority (with icon — AlertTriangle for Blocker/Critical)
- Assignee
- Session pills with Switch buttons
- Expandable section: blocked, blocked_reason, epic_key, story_points, labels, watcher freshness

Color coding for badges:
- PR state: open=green, merged=purple, closed=red
- Review: approved=green, changes_requested=amber, review_required=gray
- CI: success=green, failure=red, pending=yellow
- Jira priority: blocker/critical=red, major=amber, normal=blue, minor=gray
- Jira status: use appropriate colors based on status name

- [ ] **Step 3: Create ResourcesPage**

- WatcherStatus bar at top
- Two collapsible sections: "Pull Requests" and "Jira Issues" (using chevron icons)
- Sort controls per section (or global): urgency / recent activity
- Render ResourceCard for each resource
- "Timeline" button on each card → navigates to Timeline filtered by that resource
- Session name clicks → navigate to Sessions tab
- Switch buttons → postSwitch + toast

- [ ] **Step 4: Wire into App.tsx**

Replace Resources tab placeholder with ResourcesPage. Add navigation callback for Resources → Timeline (filter by resource).

- [ ] **Step 5: Verify build**

Run: `cd ui && npm run build`

- [ ] **Step 6: Commit**

```bash
git add ui/src/pages/ResourcesPage.tsx ui/src/components/ResourceCard.tsx ui/src/components/WatcherStatus.tsx ui/src/App.tsx
git commit --signoff -m "feat: External Resources tab with resource cards, watcher health, and cross-tab navigation"
```
