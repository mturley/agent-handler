# Phase 5a: Web UI — Backend API + Sessions Tab

## Overview

Phase 5a delivers the first vertical slice of the agent-handler web UI: a Go HTTP server embedded in the handler binary, a React/Vite SPA, and the Sessions tab — the most information-dense view. A prerequisite peek cache optimization reduces redundant terminal captures across consumers.

This is part of Phase 5 (Web UI), decomposed into sub-phases:
- **5a-prereq: Peek cache** — cache terminal snapshots in the DB so multiple consumers share results
- **5a: Backend API + React scaffold + Sessions tab** — the full vertical slice
- **5b: Timeline tab** (future spec)
- **5c: External Resources tab** (future spec)

---

## 5a-prereq: Peek Cache

### Problem

Every statusline refresh (every 10s, per session) independently captures all other sessions' terminal content via `cmux capture-pane`. With 20 sessions, that's 20 parallel scans every 10 seconds — redundant and wasteful. The web server would add yet another consumer.

### Solution

Cache full terminal snapshots in a `peek_state` table. Any consumer that wants peek data checks the cache first and only does a fresh capture if the data is stale.

### Schema

```sql
CREATE TABLE IF NOT EXISTS peek_state (
    session_id TEXT PRIMARY KEY,
    content TEXT NOT NULL,
    needs_input INTEGER NOT NULL DEFAULT 0,
    reason TEXT,
    updated_at TEXT NOT NULL
);
```

- `content` — full captured terminal output (everything `cmux capture-pane` returns, typically 2-5KB per session)
- `needs_input` — derived from content via `terminal.NeedsInput()`, stored for fast flag checks
- `reason` — human-readable reason if needs_input is true (e.g. "awaiting approval")
- `updated_at` — when the cache was last refreshed

### Cache helper

`PeekScanWithCache(db *DB, maxAge time.Duration) []PeekResult` — checks if the newest `updated_at` in `peek_state` is within `maxAge`. If fresh, returns cached results. If stale, does the full cmux capture-pane scan on all peekable sessions, upserts `peek_state`, returns fresh results.

### Consumer behavior

| Consumer | Cache behavior |
|----------|---------------|
| `handler peek --session X` (CLI, user-invoked) | Always fresh capture, no cache |
| `handler peek --list-need-input` | Uses cache |
| Statusline `scanAwaitingApproval()` | Uses cache with 5s maxAge |
| Web server | Background goroutine refreshes cache every 5s |

### Cleanup

`handler cleanup` deletes `peek_state` rows for any sessions it archives.

---

## Architecture

### `handler ui` Command

A new CLI command that starts the web UI server.

**Startup flow:**
1. Detect cmux via `terminal.Detect()`
2. If not in cmux: warn that cmux features (session switching, peek previews, etc.) won't be available. Prompt "Continue without cmux features? [y/N]"
3. Start HTTP server on port 8420
4. Open the URL in the browser — in cmux this triggers a cmux browser pane; outside cmux it opens the system default browser

**Server components:**
- **Static files** — built React SPA embedded via `//go:embed web/dist/*`
- **REST API** — JSON endpoints over the existing DB
- **SSE stream** — `/api/stream` pushes live updates to the frontend
- **Action endpoints** — POST endpoints that run handler/cmux CLI commands
- **Capabilities** — `/api/capabilities` tells the frontend what's available

If `web/dist/` was not built (empty embed), `handler ui` prints "Web UI not built. Run `make build-web` first." and exits.

### Embedded SPA

The React SPA is built with Vite and embedded into the Go binary at compile time. Production builds are typically 200-500KB gzipped — negligible impact on the 19MB binary.

### Data Flow

- **Queries:** Frontend fetches data via REST endpoints (JSON)
- **Live updates:** Frontend connects to SSE endpoint. Server polls DB every 3 seconds and pushes deltas as typed events (`sessions_updated`, `peek_updated`)
- **Actions:** Frontend POSTs to action endpoints. Server executes handler/cmux CLI commands, returns success/failure. Frontend shows toast notification.

---

## REST API Endpoints

### Session data

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/sessions` | All sessions with display state, unread counts, peek state, subscribed resources |
| GET | `/api/sessions/:id` | Single session detail |
| GET | `/api/sessions/:id/peek` | Cached terminal content from peek_state |
| GET | `/api/sessions/:id/inbox` | Unread events for a session |

### Capabilities

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/capabilities` | `{ "cmux": true/false }` |

### Actions

| Method | Path | Body | Description |
|--------|------|------|-------------|
| POST | `/api/actions/switch` | `{ "session_id": "..." }` | Switch to session via `handler switch` |
| POST | `/api/actions/peek` | `{ "session_id": "..." }` | Force fresh peek (bypass cache) |
| POST | `/api/actions/dismiss-inbox` | `{ "session_id": "..." }` | Advance cursor (dismiss unread without delivering) |

### SSE

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/stream` | Server-sent events. Event types: `sessions_updated`, `peek_updated` |

### Deferred (5b/5c)

- `/api/events` — timeline (5b)
- `/api/resources` — watched resources (5c)
- `/api/cost` — cost data (future)

---

## Sessions Tab

### Top Bar

- **Fuzzy search** — searches session name AND branch simultaneously. Full width on narrow screens.
- **"Group by repo & workspace" toggle** — on by default. Falls back to repo-only grouping without cmux. When off, sessions show as a flat list with repo/workspace as badges.
- **Sort dropdown** — options: "Match cmux" (default when cmux available), "Last prompt" (default fallback), "Created", "Unread count", "Cost", "Name (A-Z)". Each option reversible with an arrow toggle.
- **State filter chips** (multi-select): Active, Idle, Dead, Needs input, Has unread, Blocked
- **Repo filter** — dropdown, only shown when multiple repos exist

### Grouped View (toggle on)

- **Repo header** — repo name (e.g. `opendatahub-io/odh-dashboard`)
- **Workspace group** — workspace name with a **vertical colored bar** on the left edge (matching cmux's sidebar style, using the workspace's custom color). If all sessions in the workspace share a branch, show the branch at the workspace level instead of per-card. Hidden when cmux is unavailable.
- **Session cards** within each group

### Flat View (toggle off)

- Session cards in a flat list, each showing repo and workspace as badges

### Sort Bubble-Up

Sorting applies within each group. Groups are then sorted by their top-ranked member — the repo containing the highest-sorted session appears first, the workspace containing the highest-sorted session within that repo appears first within it.

### Session Card (Medium Density)

- **Header row:** Session name (bold), state badge (colored dot + label: active/idle/dead), needs-input indicator (✋ emoji, amber highlight on the entire card if needs input)
- **Meta row:** Branch (if not shown at workspace level), last prompt time (relative, e.g. "5m ago")
- **Unread badge:** If > 0, a count badge with a button to open the inbox modal
- **Resource pills:** Small pills showing subscribed resources (e.g. "PR #8485", "RHOAIENG-69748"). Clickable — cross-references to Resources tab in 5c.
- **cmux switch button:** "Switch" button, only rendered when cmux capability is available. Click → POST → toast with success/failure.

### Inbox Modal

Opened from the unread badge on a session card.

- Lists unread events for the session: type icon, title, timestamp, author
- Each event expandable for full body content
- Bottom message: "**Go to the session** and type /inbox to deliver these, or use /inbox-mode auto to deliver them automatically" — "Go to the session" is clickable and triggers the switch action
- **"Dismiss all" button** — opens a confirmation modal: "Dismiss N unread events without delivering them to session X?" with Confirm/Cancel. On confirm, POSTs to `/api/actions/dismiss-inbox`.

---

## Visual Design

### Dark Mode

The entire UI uses a dark color scheme — dark backgrounds, light text, muted borders. Consistent with a terminal-adjacent developer tool. No light mode toggle.

### Responsive Design

Designed mobile-first for narrow cmux browser panes (can be as narrow as 400px).

**Breakpoints:**
- < 480px: compact mode — controls stack, filter chips scroll horizontally, resource pills collapse to count badge with tooltip
- 480-768px: medium — side-by-side where possible
- \> 768px: full layout

**Narrow-width adaptations:**
- Search bar takes full width
- Sort dropdown collapses to an icon button
- Session cards: meta row wraps below header, resource pills collapse to "3 resources" count badge
- Grouped view: repo headers and workspace bars remain as structural elements
- Modals: full-width on narrow screens, centered overlay on wide screens

---

## Build Integration

### Directory Structure

```
web/
  package.json
  vite.config.ts
  tsconfig.json
  src/
    main.tsx
    App.tsx
    components/
    api/
    hooks/
  public/
  dist/          ← build output, gitignored, embedded into Go binary
```

### Makefile Targets

| Target | Description |
|--------|-------------|
| `make build-cli` | Go binary only (fast). If `web/dist/` doesn't exist, web embed is empty. |
| `make build-web` | Vite build only (`cd web && npm run build`). Populates `web/dist/`. |
| `make build` | `build-web` + `build-cli`. Full build with embedded SPA. |
| `make install` | Installs the binary (no build). If `web/dist/` is missing, warns: "Web UI not built — handler ui will not work. Continue? [y/N]". Skips warning with `NONINTERACTIVE=1`. Use `make build && make install` for a full build+install, or `make build-cli && make install` for CLI-only. |
| `make dev` | Runs Vite dev server (port 5173) and Go API server (port 8420) concurrently. |
| `make clean` | Removes `bin/` and `web/dist/`. |

### Embedding

A new `web_embed.go` file with `//go:embed web/dist/*` that handles the case where `web/dist/` doesn't exist (conditional embed or build tag). The `handler ui` command checks for empty embed and reports a helpful error.

### Dev Workflow

- Terminal 1: `cd web && npm run dev` — Vite dev server on 5173 with proxy to Go API on 8420
- Terminal 2: `go run . ui` — Go API server, skips static file serving in dev mode (Vite handles it)
- Or: `make dev` — runs both

### Frontend Dependencies

- React 19 + TypeScript
- Component library / CSS approach to be decided during implementation — must support dark mode and responsive design with minimal bundle size

---

## Files to Create/Modify

### New (peek cache)

- `db/peek.go` — PeekState struct, CRUD functions, PeekScanWithCache
- `db/peek_test.go` — tests

### New (web server)

- `cmd/ui.go` — `handler ui` command
- `web_embed.go` — `//go:embed` for web assets
- `web/` — entire React SPA directory

### Modified (peek cache)

- `db/schema.sql` — add peek_state table
- `db/db.go` — migration for peek_state
- `cmd/statusline.go` — use PeekScanWithCache instead of direct scan
- `cmd/peek.go` — `--list-need-input` uses cache
- `cmd/cleanup.go` — delete peek_state for archived sessions

### Modified (web server)

- `cmd/root.go` — add `ui` to PersistentPreRunE skip list
- `Makefile` — new targets (build-cli, build-web, build, dev)
