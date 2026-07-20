# Phase 5: Web UI — Overview

## Summary

A web-based dashboard for agent-handler, served from the handler binary itself. Provides visual access to sessions, events, external resources, and cost data — with cmux integration for session switching and terminal peek previews.

## Technology

- **Backend:** Go HTTP server embedded in the handler binary (`handler ui`)
- **Frontend:** React + Vite SPA, built separately, embedded via `//go:embed`
- **Data flow:** REST API for queries and actions, SSE for live updates
- **Design:** Dark mode, responsive (works in narrow cmux browser panes)
- **cmux integration:** Detected at startup. Session switching, peek previews, and other cmux features are available when running inside cmux, gracefully absent otherwise.

## Sub-Phases

### 5a-prereq: Peek Cache

Cache full terminal snapshots in a `peek_state` table so multiple consumers (statusline, web server) share results instead of each doing redundant `cmux capture-pane` scans. Consumers opt in to the cache; `handler peek` CLI remains fresh.

**Spec:** standalone section within [phase5a-web-ui-sessions.md](2026-07-20-phase5a-web-ui-sessions.md)

### 5a: Backend API + React Scaffold + Sessions Tab

The first vertical slice: Go HTTP server, REST API endpoints, SSE stream, React SPA scaffold, and the Sessions tab — the most information-dense view. Sessions grouped by repo/workspace, filterable, sortable, with inbox modals and cmux switch buttons.

**Spec:** [phase5a-web-ui-sessions.md](2026-07-20-phase5a-web-ui-sessions.md)

### 5b: Timeline Tab

A chronological event stream with filters by session, event type, source, and date range. Shows milestones, decisions, status updates, external events, and messages across all sessions.

**Spec:** TBD (future)

### 5c: External Resources Tab

Watched resources (PRs, Jira issues) with cached state from watchers. Shows which sessions subscribe to each resource, watcher health status, and resource state (PR review decision, Jira priority/status). Cross-references to session cards. cmux switch buttons for subscribed sessions.

**Spec:** TBD (future)

### 5d: Terminal Peek Preview (potential)

Render cached terminal snapshots in the web UI — hover a session card to see what its terminal looks like. Requires a terminal rendering library (e.g. xterm.js) to properly handle ANSI escape codes. Depends on the peek cache from 5a-prereq.

**Spec:** TBD (future, may fold into 5a as an enhancement)

### 5e: Cost Dashboard (potential)

Visual cost tracking — daily/monthly charts, per-session breakdowns, model usage. Builds on the cost tracking tables already in the DB.

**Spec:** TBD (future)

## Build Integration

| Target | Description |
|--------|-------------|
| `make build-cli` | Go binary only (fast, no web build) |
| `make build-web` | Vite build only |
| `make build` | Full build (web + CLI) |
| `make install` | Install binary (no build step) |
| `make dev` | Run Vite dev server + Go API server concurrently |

## Execution Order

Each sub-phase gets its own spec → plan → implementation cycle:

1. 5a-prereq (peek cache) — small, can be done quickly
2. 5a (API + Sessions tab) — the big lift, establishes all the infrastructure
3. 5b (Timeline) — builds on the API server from 5a
4. 5c (Resources) — builds on the API server from 5a
5. 5d, 5e — enhancements, order flexible
