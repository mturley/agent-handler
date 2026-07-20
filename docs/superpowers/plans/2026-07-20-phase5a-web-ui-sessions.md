# Phase 5a: Web UI — Backend API + Sessions Tab Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the first vertical slice of the agent-handler web UI: a Go HTTP server, React/Vite SPA, and the Sessions tab with search, filtering, sorting, grouping, inbox modals, and cmux integration.

**Architecture:** Go HTTP server (`handler ui`) serves a React SPA embedded via `//go:embed`. REST API endpoints expose session data. SSE pushes live updates. Frontend is React 19 + TypeScript + Vite, dark mode, responsive to 400px. Actions (cmux switch, dismiss inbox) go through POST endpoints.

**Tech Stack:** Go (net/http), React 19, TypeScript, Vite, SSE, SQLite

## Global Constraints

- Go binary, pure-Go SQLite (`modernc.org/sqlite`)
- All timestamps ISO 8601 UTC
- `--json` flag on CLI commands
- Tests must pass: `go test ./...`
- Use `--signoff` on all commits
- Dark mode only, responsive to 400px width
- Web assets in `web/` directory, build output in `web/dist/` (gitignored)
- `make build-cli` for Go-only builds, `make build` for full builds

---

### Task 1: Build Infrastructure (Makefile + Web Embed)

**Files:**
- Modify: `Makefile`
- Create: `web_embed.go`
- Modify: `.gitignore`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `make build-cli` target (Go binary only)
  - `make build-web` target (Vite build)
  - `make build` target (both)
  - `make dev` target (concurrent dev servers)
  - `EmbeddedWeb embed.FS` global in `web_embed.go`

- [ ] **Step 1: Update Makefile**

Replace the existing `build` target and add new targets:

```makefile
BINARY_NAME := handler
BIN_DIR := bin
INSTALL_DIR := /usr/local/bin

.PHONY: build build-cli build-web install test clean dev

build-cli:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY_NAME) .
	@echo ""
	@echo "Built $(BIN_DIR)/$(BINARY_NAME)"
	@echo "Run 'make install' to install."

build-web:
	@if [ ! -f web/package.json ]; then echo "Error: web/package.json not found. Run from the repo root." && exit 1; fi
	@cd web && npm install --silent && npm run build
	@echo "Built web/dist/"

build: build-web build-cli

install:
	@test -f $(BIN_DIR)/$(BINARY_NAME) || (echo "Error: $(BIN_DIR)/$(BINARY_NAME) not found. Run 'make build' or 'make build-cli' first." && exit 1)
ifndef NONINTERACTIVE
	@if [ ! -d web/dist ] || [ -z "$$(ls -A web/dist 2>/dev/null)" ]; then \
		echo "Warning: Web UI not built — handler ui will not work."; \
		echo "Run 'make build' for a full build, or 'make build-cli' for CLI-only."; \
		printf "Continue? [y/N] "; \
		read answer; \
		case "$$answer" in [yY]*) ;; *) echo "Aborted."; exit 1;; esac; \
	fi
endif
	@cp $(BIN_DIR)/$(BINARY_NAME) $(INSTALL_DIR)/.$(BINARY_NAME).tmp
	@chmod 755 $(INSTALL_DIR)/.$(BINARY_NAME).tmp
	@mv $(INSTALL_DIR)/.$(BINARY_NAME).tmp $(INSTALL_DIR)/$(BINARY_NAME)
	@echo "Installed binary to $(INSTALL_DIR)/$(BINARY_NAME)"
	@echo ""
ifdef NONINTERACTIVE
	@$(INSTALL_DIR)/$(BINARY_NAME) setup --yes
else
	@$(INSTALL_DIR)/$(BINARY_NAME) setup
endif

test:
	go test ./... -v

clean:
	rm -rf $(BIN_DIR) web/dist web/node_modules

dev:
	@echo "Starting dev servers..."
	@echo "  API server: http://localhost:8420"
	@echo "  Vite dev:   http://localhost:5173"
	@(cd web && npm run dev) & go run . ui --dev &
	@wait
```

- [ ] **Step 2: Create web_embed.go**

The `//go:embed` directive fails if the directory doesn't exist. Use a build tag or a placeholder approach. The simplest: create a `web/dist/.gitkeep` file so the directory always exists, and use `//go:embed all:web/dist` which handles empty dirs.

Create `web_embed.go`:

```go
package main

import "embed"

//go:embed all:web/dist
var EmbeddedWeb embed.FS
```

Create `web/dist/.gitkeep` (empty file) so the embed works even without a build.

- [ ] **Step 3: Update .gitignore**

Add to `.gitignore`:

```
web/dist/*
!web/dist/.gitkeep
web/node_modules/
```

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: compiles — the embed picks up the empty `web/dist/` directory.

- [ ] **Step 5: Commit**

```bash
git add Makefile web_embed.go web/dist/.gitkeep .gitignore
git commit --signoff -m "feat: build infrastructure for web UI (Makefile targets + embed)"
```

---

### Task 2: Go API Server + `handler ui` Command

**Files:**
- Create: `cmd/ui.go`
- Create: `cmd/api/server.go`
- Create: `cmd/api/capabilities.go`
- Modify: `cmd/root.go` (add `ui` to skip list)

**Interfaces:**
- Consumes: `EmbeddedWeb` from Task 1, `terminal.Detect()` (existing), `db.Open()` (existing)
- Produces:
  - `handler ui` command — starts HTTP server on port 8420
  - `GET /api/capabilities` endpoint
  - Static file serving from embedded SPA

- [ ] **Step 1: Create cmd/api/server.go**

```go
package api

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"

	"github.com/mturley/agent-handler/db"
)

type Server struct {
	DB       *db.DB
	CmuxAvailable bool
	DevMode  bool
	WebFS    fs.FS
	Port     int
	Logger   *log.Logger
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("GET /api/capabilities", s.handleCapabilities)

	// Static files (skip in dev mode — Vite serves them)
	if !s.DevMode && s.WebFS != nil {
		mux.Handle("/", http.FileServer(http.FS(s.WebFS)))
	}

	addr := fmt.Sprintf(":%d", s.Port)
	s.Logger.Printf("Listening on http://localhost%s", addr)
	return http.ListenAndServe(addr, mux)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
```

- [ ] **Step 2: Create cmd/api/capabilities.go**

```go
package api

import "net/http"

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cmux": s.CmuxAvailable,
	})
}
```

- [ ] **Step 3: Create cmd/ui.go**

```go
package cmd

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"runtime"

	"github.com/mturley/agent-handler/cmd/api"
	"github.com/mturley/agent-handler/terminal"
	"github.com/spf13/cobra"
)

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Start the web UI server",
	RunE:  runUI,
}

var (
	uiPort int
	uiDev  bool
)

func init() {
	uiCmd.GroupID = "human"
	rootCmd.AddCommand(uiCmd)
	uiCmd.Flags().IntVar(&uiPort, "port", 8420, "HTTP server port")
	uiCmd.Flags().BoolVar(&uiDev, "dev", false, "development mode (skip static file serving)")
}

func runUI(cmd *cobra.Command, args []string) error {
	// Check if web assets are built
	webFS, err := fs.Sub(getWebFS(), "web/dist")
	if err != nil || !uiDev {
		// Check if dist has any real files
		entries, _ := fs.ReadDir(getWebFS(), "web/dist")
		hasContent := false
		for _, e := range entries {
			if e.Name() != ".gitkeep" {
				hasContent = true
				break
			}
		}
		if !hasContent && !uiDev {
			fmt.Println("Web UI not built. Run 'make build-web' first.")
			return nil
		}
	}

	// Detect cmux
	backendType, _ := terminal.Detect()
	cmuxAvailable := backendType == "cmux"

	if !cmuxAvailable {
		fmt.Println("cmux not detected. Session switching and other cmux features will not be available.")
		fmt.Print("Continue without cmux features? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if answer != "y" && answer != "Y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Open DB
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	logger := log.New(os.Stderr, "[handler-ui] ", log.LstdFlags)

	server := &api.Server{
		DB:            d,
		CmuxAvailable: cmuxAvailable,
		DevMode:       uiDev,
		WebFS:         webFS,
		Port:          uiPort,
		Logger:        logger,
	}

	// Open browser
	url := fmt.Sprintf("http://localhost:%d", uiPort)
	logger.Printf("Opening %s in browser...", url)
	openBrowser(url)

	return server.Start()
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	cmd.Start()
}
```

Note: `getWebFS()` needs to return the embedded FS. Add a function in `web_embed.go` or pass it through. The simplest approach: in `web_embed.go`, add:

```go
func GetWebFS() embed.FS {
	return EmbeddedWeb
}
```

And in `cmd/ui.go`, call it via a package-level function set up in `main.go`, or by passing it through. Since `EmbeddedWeb` is in `package main` and `cmd` is a separate package, the cleanest way is to have `main.go` pass it:

In `main.go`, before `cmd.Execute()`:
```go
cmd.SetWebFS(EmbeddedWeb)
```

In `cmd/root.go`:
```go
var webFS embed.FS

func SetWebFS(fs embed.FS) {
	webFS = fs
}
```

In `cmd/ui.go`, replace `getWebFS()` with `webFS`.

- [ ] **Step 4: Update cmd/root.go**

Add `"ui"` to the PersistentPreRunE skip list:

```go
if cmd.Name() == "setup" || cmd.Name() == "help" || cmd.Name() == "completion" || cmd.Name() == "claude" || cmd.Name() == "ui" {
```

Add the `SetWebFS` function and `webFS` variable.

- [ ] **Step 5: Update main.go**

Add `cmd.SetWebFS(EmbeddedWeb)` before `cmd.Execute()`.

- [ ] **Step 6: Build and test**

Run: `go build ./... && go test ./...`
Expected: compiles, tests pass. `handler ui --help` shows the command.

- [ ] **Step 7: Commit**

```bash
git add cmd/ui.go cmd/api/server.go cmd/api/capabilities.go cmd/root.go main.go web_embed.go
git commit --signoff -m "feat: handler ui command with Go API server and capabilities endpoint"
```

---

### Task 3: REST API — Sessions and Inbox Endpoints

**Files:**
- Create: `cmd/api/sessions.go`
- Create: `cmd/api/actions.go`
- Modify: `cmd/api/server.go` (register routes)

**Interfaces:**
- Consumes: `db.ListSessions`, `db.GetSession`, `db.GetPeekState`, `db.UnreadForSession`, `db.UnreadCountForSession`, `discover.IsSessionProcess`, `db.AdvanceBothCursors` (all existing)
- Produces:
  - `GET /api/sessions` — all sessions with display state, unread, peek, resources
  - `GET /api/sessions/:id` — single session
  - `GET /api/sessions/:id/peek` — cached peek content
  - `GET /api/sessions/:id/inbox` — unread events
  - `POST /api/actions/switch` — cmux session switch
  - `POST /api/actions/peek` — force fresh peek
  - `POST /api/actions/dismiss-inbox` — advance cursor

- [ ] **Step 1: Create cmd/api/sessions.go**

Implement `handleSessions` (GET /api/sessions) which mirrors `handler status --json` logic: list sessions, compute display_state, fetch unread counts, fetch peek state, fetch subscriptions. Return enriched JSON array.

Implement `handleSession` (GET /api/sessions/:id), `handleSessionPeek`, `handleSessionInbox`.

Use `buildSessionStatuses` from `cmd/status.go` if possible, or replicate the display_state logic. The key fields per session: session_id, session_name, branch, repo, display_state, inbox_mode, peekable, terminal_type, unread_count, unread_breakdown, last_active, last_prompt, cmux_workspace, needs_input.

- [ ] **Step 2: Create cmd/api/actions.go**

Implement `handleSwitch` (POST /api/actions/switch): parse JSON body, run `handler switch --session <id>` via exec.Command, return success/error.

Implement `handleForcePeek` (POST /api/actions/peek): parse JSON body, run `handler peek --session <id> --json` via exec.Command, return the output.

Implement `handleDismissInbox` (POST /api/actions/dismiss-inbox): parse JSON body, open writable DB, advance both cursors for the session, return success.

- [ ] **Step 3: Register routes in server.go**

Add all new routes to the mux in `Server.Start()`.

- [ ] **Step 4: Build and test**

Run: `go build ./... && go test ./...`
Test manually: `go run . ui --dev &` then `curl http://localhost:8420/api/sessions | python3 -m json.tool`

- [ ] **Step 5: Commit**

```bash
git add cmd/api/sessions.go cmd/api/actions.go cmd/api/server.go
git commit --signoff -m "feat: REST API endpoints for sessions, inbox, and actions"
```

---

### Task 4: SSE Stream

**Files:**
- Create: `cmd/api/stream.go`
- Modify: `cmd/api/server.go` (register route)

**Interfaces:**
- Consumes: `db.ListSessions`, `db.ListPeekStates` (existing)
- Produces: `GET /api/stream` — SSE endpoint with `sessions_updated` and `peek_updated` events

- [ ] **Step 1: Implement cmd/api/stream.go**

```go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

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

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			// Send sessions update
			data, err := json.Marshal(map[string]string{"type": "heartbeat"})
			if err == nil {
				fmt.Fprintf(w, "event: heartbeat\ndata: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}
```

The initial implementation sends heartbeats. The frontend will refetch on each heartbeat. This is simpler than diffing DB state server-side and equally effective for a 3-second polling interval. Enhanced delta-based SSE can come later.

- [ ] **Step 2: Register in server.go**

Add: `mux.HandleFunc("GET /api/stream", s.handleStream)`

- [ ] **Step 3: Build and test**

Run: `go build ./...`
Test: `curl -N http://localhost:8420/api/stream` — should see heartbeat events every 3 seconds.

- [ ] **Step 4: Commit**

```bash
git add cmd/api/stream.go cmd/api/server.go
git commit --signoff -m "feat: SSE stream endpoint for live UI updates"
```

---

### Task 5: React Scaffold + Dark Mode Shell

**Files:**
- Create: `web/package.json`
- Create: `web/vite.config.ts`
- Create: `web/tsconfig.json`
- Create: `web/tsconfig.node.json`
- Create: `web/index.html`
- Create: `web/src/main.tsx`
- Create: `web/src/App.tsx`
- Create: `web/src/App.css`
- Create: `web/src/index.css`
- Create: `web/src/api/client.ts`
- Create: `web/src/api/types.ts`
- Create: `web/src/hooks/useSSE.ts`
- Create: `web/src/hooks/useCapabilities.ts`

**Interfaces:**
- Consumes: REST API from Tasks 2-4
- Produces: Running React app with tab navigation shell (Sessions tab active, Timeline and Resources as placeholders)

- [ ] **Step 1: Initialize Vite project**

Run `npm create vite@latest web -- --template react-ts` or create the files manually. Set up:

- `package.json` with React 19, TypeScript, Vite
- `vite.config.ts` with API proxy to port 8420
- `tsconfig.json` and `tsconfig.node.json`
- `index.html` entry point

- [ ] **Step 2: Create app shell**

`web/src/App.tsx` — tabbed layout with Sessions (active), Timeline (placeholder), Resources (placeholder). Dark mode CSS variables in `index.css`. Responsive layout.

`web/src/api/types.ts` — TypeScript interfaces matching the API JSON shapes (Session, PeekState, Event, Capabilities).

`web/src/api/client.ts` — fetch wrappers for all API endpoints.

`web/src/hooks/useSSE.ts` — hook that connects to `/api/stream` and triggers refetches on heartbeat events.

`web/src/hooks/useCapabilities.ts` — hook that fetches and caches `/api/capabilities`.

- [ ] **Step 3: Verify dev workflow**

Terminal 1: `go run . ui --dev`
Terminal 2: `cd web && npm run dev`
Open http://localhost:5173 — should see the dark-mode app shell with tabs.

- [ ] **Step 4: Verify production build**

Run: `cd web && npm run build`
Run: `make build`
Run: `./bin/handler ui` — should serve the SPA at http://localhost:8420.

- [ ] **Step 5: Commit**

```bash
git add web/
git commit --signoff -m "feat: React scaffold with dark mode shell and API client"
```

---

### Task 6: Sessions Tab — Data Layer + Top Bar

**Files:**
- Create: `web/src/hooks/useSessions.ts`
- Create: `web/src/components/TopBar.tsx`
- Create: `web/src/components/TopBar.css`
- Create: `web/src/components/FilterChips.tsx`
- Create: `web/src/pages/SessionsPage.tsx`

**Interfaces:**
- Consumes: API client from Task 5, useSSE from Task 5
- Produces: Sessions page with search, sort, filter controls, data fetching — but no session cards yet (just a count of matching sessions)

- [ ] **Step 1: Create useSessions hook**

Fetches `/api/sessions`, refetches on SSE heartbeat. Exposes:
- `sessions` — raw data
- `filteredSessions` — after search + filters
- `sortedSessions` — after sorting
- `groupedSessions` — after grouping by repo/workspace
- Search, filter, sort state and setters

- [ ] **Step 2: Create TopBar component**

- Fuzzy search input
- Group toggle
- Sort dropdown with reverse toggle
- Filter chips (Active, Idle, Dead, Needs input, Has unread, Blocked)
- Repo filter dropdown (shown when multiple repos)

All responsive — stacks on narrow screens, chips scroll horizontally.

- [ ] **Step 3: Create SessionsPage**

Wires TopBar to useSessions. Shows the top bar and a temporary "N sessions" count below it to verify filtering/sorting works.

- [ ] **Step 4: Verify**

Dev mode: apply various filters, search terms, sort options — count should update correctly.

- [ ] **Step 5: Commit**

```bash
git add web/src/
git commit --signoff -m "feat: Sessions tab data layer with search, sort, and filter controls"
```

---

### Task 7: Sessions Tab — Session Cards + Grouping

**Files:**
- Create: `web/src/components/SessionCard.tsx`
- Create: `web/src/components/SessionCard.css`
- Create: `web/src/components/SessionGroup.tsx`
- Create: `web/src/components/SessionGroup.css`
- Modify: `web/src/pages/SessionsPage.tsx`

**Interfaces:**
- Consumes: groupedSessions from Task 6, useCapabilities from Task 5
- Produces: Rendered session cards with repo/workspace grouping, state badges, switch buttons

- [ ] **Step 1: Create SessionCard component**

Medium density card:
- Header: session name (bold), state dot + label, ✋ if needs input (amber highlight on card)
- Meta: branch (if not at workspace level), last prompt relative time
- Unread badge with count (button to open inbox modal — wired in Task 8)
- Resource pills (small, showing resource IDs)
- "Switch" button (only when cmux available)

- [ ] **Step 2: Create SessionGroup component**

Handles both grouped and flat views:
- Grouped: repo header → workspace sub-header with vertical colored bar → session cards
- Flat: session cards with repo/workspace badges
- Branch shown at workspace level when all sessions share it

- [ ] **Step 3: Wire into SessionsPage**

Replace the count placeholder with rendered groups/cards. Implement sort bubble-up: sort within groups, then sort groups by their top member.

- [ ] **Step 4: Responsive testing**

Test at 400px, 600px, and 1200px widths. Cards should adapt (meta wraps, resource pills collapse to count).

- [ ] **Step 5: Commit**

```bash
git add web/src/
git commit --signoff -m "feat: session cards with repo/workspace grouping and state badges"
```

---

### Task 8: Sessions Tab — Inbox Modal + Actions

**Files:**
- Create: `web/src/components/InboxModal.tsx`
- Create: `web/src/components/InboxModal.css`
- Create: `web/src/components/ConfirmModal.tsx`
- Create: `web/src/components/Toast.tsx`
- Create: `web/src/hooks/useToast.ts`
- Modify: `web/src/components/SessionCard.tsx` (wire inbox + switch actions)

**Interfaces:**
- Consumes: API client (switch, dismiss-inbox, session inbox), useCapabilities, useToast
- Produces: Complete interactive Sessions tab with inbox modal, dismiss confirmation, switch action, toast notifications

- [ ] **Step 1: Create Toast system**

`useToast` hook + `Toast` component — shows success/error messages that auto-dismiss after 3 seconds. Positioned at bottom-right.

- [ ] **Step 2: Create ConfirmModal component**

Generic confirmation modal: title, message, Confirm/Cancel buttons. Dark-mode styled. Full-width on narrow screens.

- [ ] **Step 3: Create InboxModal component**

- Lists unread events: type icon, title, timestamp, author
- Each event expandable for full body
- Bottom: "**Go to the session** and type /inbox to deliver these..." — "Go to the session" triggers switch action
- "Dismiss all" button opens ConfirmModal: "Dismiss N unread events without delivering them to session X?"
- On confirm: POST dismiss-inbox, close modal, show toast, refetch sessions

- [ ] **Step 4: Wire actions into SessionCard**

- "Switch" button → POST switch → toast
- Unread badge click → open InboxModal
- Both actions show loading state during the POST

- [ ] **Step 5: End-to-end test**

Full dev mode test:
1. Open web UI
2. Verify session cards render with correct state
3. Click "Switch" on a session → verify cmux switches and toast appears
4. Click unread badge → inbox modal opens with events
5. Click "Dismiss all" → confirmation → dismiss → toast

- [ ] **Step 6: Production build test**

Run `make build && ./bin/handler ui` — verify the full SPA works from the embedded build.

- [ ] **Step 7: Commit**

```bash
git add web/src/
git commit --signoff -m "feat: inbox modal, dismiss confirmation, switch action, and toast notifications"
```
