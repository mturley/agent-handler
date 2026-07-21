# Phase 5b: Web UI — Timeline Tab

## Overview

The Timeline tab provides a chronological event feed in a chat-style layout with a vertical timeline line, colored type dots, expandable event details, external resource links, and full filtering. Events load via infinite scroll with SSE pushing new events to the top.

---

## API Endpoint

### `GET /api/events`

Cursor-based pagination using event timestamps.

**Query parameters:**

| Param | Type | Description |
|-------|------|-------------|
| `before` | string (ISO 8601) | Return events older than this timestamp. Omit for most recent. |
| `limit` | int | Max events to return (default 50) |
| `session` | string | Filter by session ID |
| `type` | string | Filter by event type (comma-separated for multiple) |
| `source` | string | Filter by source (agent, github, jira, handler) |
| `search` | string | Free-text search on event title and body |

**Response:**

```json
{
  "events": [
    {
      "id": "...",
      "ts": "2026-07-20T14:30:00Z",
      "source": "agent",
      "session_id": "abc-123",
      "session_name": "auth-feature",
      "type": "milestone",
      "title": "Implemented retry logic",
      "body": "Added exponential backoff...",
      "author": null,
      "author_type": null,
      "broadcast": false,
      "tags": null,
      "resources": [
        {
          "resource_type": "pr",
          "resource_id": "opendatahub-io/odh-dashboard#8485",
          "resource_url": "https://github.com/opendatahub-io/odh-dashboard/pull/8485"
        }
      ]
    }
  ],
  "has_more": true,
  "next_cursor": "2026-07-20T14:25:00Z"
}
```

**Notes:**
- `session_name` is resolved server-side by joining with the sessions table
- `resources` are included from the `event_resources` table
- `next_cursor` is the `ts` of the oldest event in the response — pass as `before` for the next page
- `has_more` indicates whether there are older events

### SSE Enhancement

The existing `GET /api/stream` SSE endpoint adds a new event type: `events_new`. When new events are inserted (detected by polling the DB), the server pushes this event so the frontend can prepend new events to the top of the feed.

---

## Timeline Layout

### Chat-style feed

- **Vertical timeline line** — thin (2px) muted gray line on the left side, running the full height of the feed
- **Colored dot** per event — positioned on the timeline line, color determined by event type:
  - milestone: blue
  - decision: purple
  - status: gray
  - blocked/unblocked: amber
  - message: indigo
  - pr_comment/pr_review_comment: green
  - pr_approved: green
  - pr_merged/pr_closed: gray
  - ci_check_passed: green
  - ci_check_failed: red
  - jira_comment/jira_status_change/jira_assigned: blue
  - handoff/followup: orange
  - session_end: gray
  - default: gray

- **Event bubble** — card-like container to the right of the dot:
  - **Header:** Type badge (small colored pill), title (bold), timestamp (relative, e.g. "5m ago")
  - **Meta line:** Session name (clickable — navigates to Sessions tab filtered by that session), author (if present)
  - **Body:** Hidden by default, expandable on click. Shows full event body text.
  - **Resource links:** If the event has associated resources, show clickable links:
    - PR: "PR #8485" → links to GitHub URL
    - Jira: "RHOAIENG-69748" → links to Jira URL

### Infinite scroll

- Initial load: most recent 50 events
- Scroll to bottom: load next 50 (older events) using `next_cursor`
- Show a spinner while loading more
- "No more events" message when `has_more` is false
- SSE `events_new` prepends new events to the top with a subtle animation

---

## Filters

### Filter bar (above the feed)

- **Session filter** — Select dropdown listing all active sessions by name. "All sessions" default.
- **Type filter** — Multi-select dropdown or chip group for event types. Group by category:
  - Agent: milestone, decision, status, blocked, unblocked, handoff, followup, message
  - GitHub: pr_comment, pr_review_comment, pr_approved, pr_merged, pr_closed, ci_check_passed, ci_check_failed
  - Jira: jira_comment, jira_status_change, jira_assigned
  - System: session_end, watch_started
- **Source filter** — Chips: Agent, GitHub, Jira, Handler (toggleable)
- **Date range** — Two date pickers (from/to). Optional — default is no date constraint.
- **Free-text search** — Input field that searches event titles and bodies.

All filters are combinable (AND logic). Changing any filter resets the cursor and reloads from the most recent matching events.

### Responsive

At narrow widths (< 480px):
- Filters stack vertically
- Type filter collapses to a single "Filter types..." button that opens a modal
- Timeline dots shrink, bubbles take full width

---

## Cross-tab Navigation

### Sessions → Timeline

Each session card on the Sessions tab gets a "Timeline" button (lucide-react `Clock` icon). Clicking it:
1. Switches to the Timeline tab
2. Sets the session filter to that session's ID
3. Reloads the feed filtered by that session

This is implemented via shared state (React context or URL params) between the tabs.

### Timeline → Sessions

The session name on each event bubble is clickable. Clicking it:
1. Switches to the Sessions tab
2. Sets the search query to that session's name
3. The session card scrolls into view (or is highlighted)

---

## Files to Create/Modify

### New (backend)

- `cmd/api/events.go` — `GET /api/events` endpoint with cursor pagination, filtering, session name resolution, resource inclusion

### Modified (backend)

- `cmd/api/server.go` — register the new route
- `cmd/api/stream.go` — add `events_new` SSE event type (poll for new events)

### New (frontend)

- `ui/src/pages/TimelinePage.tsx` — the Timeline tab page
- `ui/src/components/TimelineEvent.tsx` — individual event bubble component
- `ui/src/components/TimelineFilters.tsx` — filter bar component
- `ui/src/hooks/useTimeline.ts` — data hook with cursor pagination, filtering, SSE integration

### Modified (frontend)

- `ui/src/App.tsx` — wire Timeline tab to TimelinePage (replace placeholder)
- `ui/src/api/client.ts` — add `getEvents()` fetch wrapper
- `ui/src/api/types.ts` — add `TimelineEvent` type (Event + session_name + resources)
- `ui/src/components/SessionCard.tsx` — add "Timeline" button
- `ui/src/hooks/useSSE.ts` — handle `events_new` SSE event type
