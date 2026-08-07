# Explicit Per-Event Dismissal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a session dismiss individual inbox events without advancing its cursor, excluding them from every unread surface (statusline, CLI, web UI, resource dots).

**Architecture:** A new `dismissed_events(session_id, event_id, dismissed_at)` join table records per-session dismissals. Every unread query in `db/events.go` gains a `NOT EXISTS` exclusion clause. Cursor-advance methods prune dismissal rows that fall behind the cursor. A new API endpoint + CLI command + enhanced web modal expose the capability.

**Tech Stack:** Go (SQLite via modernc.org/sqlite, cobra CLI, net/http), React + TanStack Query + shadcn/ui.

## Global Constraints

- Event IDs are UUIDs; all timestamps are ISO 8601 UTC (`time.RFC3339`).
- The web API server opens the DB read-only; write handlers must open a writable connection via `db.Open(db.DefaultPath())` and close it.
- Dismissal is per-session. `GlobalUnread*` and `DirectCountForSession` are NOT filtered by dismissal.
- New API routes use the `/api/actions/` prefix (matching `dismiss-inbox`).
- Commits use `--signoff` and end with the Co-Authored-By trailer for `Claude Opus 4.6 (1M context)`.

---

### Task 1: Add `dismissed_events` schema + migration

**Files:**
- Modify: `db/schema.sql` (after the `session_cursors` table, ~line 68)
- Modify: `db/db.go:60-62` (`runMigrations`)

**Interfaces:**
- Produces: `dismissed_events` table with columns `session_id TEXT`, `event_id TEXT`, `dismissed_at TEXT`, PK `(session_id, event_id)`, and index `idx_dismissed_events_session`.

- [ ] **Step 1: Add the table to schema.sql**

Insert after the `session_cursors` table definition:

```sql
CREATE TABLE IF NOT EXISTS dismissed_events (
    session_id   TEXT NOT NULL,
    event_id     TEXT NOT NULL,
    dismissed_at TEXT NOT NULL,
    PRIMARY KEY (session_id, event_id)
);

CREATE INDEX IF NOT EXISTS idx_dismissed_events_session ON dismissed_events(session_id);
```

- [ ] **Step 2: Add the migration safety net in db.go**

Replace `runMigrations`:

```go
func runMigrations(conn *sql.DB) error {
	// Safety net for existing databases created before a table was added to
	// schema.sql. CREATE TABLE IF NOT EXISTS is idempotent.
	_, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS dismissed_events (
			session_id   TEXT NOT NULL,
			event_id     TEXT NOT NULL,
			dismissed_at TEXT NOT NULL,
			PRIMARY KEY (session_id, event_id)
		);
		CREATE INDEX IF NOT EXISTS idx_dismissed_events_session ON dismissed_events(session_id);
	`)
	if err != nil {
		return fmt.Errorf("failed to create dismissed_events table: %w", err)
	}
	return nil
}
```

- [ ] **Step 3: Verify it builds and a fresh DB has the table**

Run: `go build ./... && go test ./db/ -run TestDualCursors -v`
Expected: builds; existing test passes (confirms schema applies cleanly).

- [ ] **Step 4: Commit**

```bash
git add db/schema.sql db/db.go
git commit --signoff -m "feat(db): add dismissed_events table and migration

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: `DismissEvent` DB method + exclusion in unread queries

**Files:**
- Modify: `db/events.go` (add `DismissEvent`; add exclusion clause to four queries)
- Test: `db/dismissal_test.go` (create)

**Interfaces:**
- Consumes: `dismissed_events` table (Task 1); existing `InsertEvent(Event, []EventRecipient, []EventResource)`, `AdvanceCursor(sessionID, ts)`, `UnreadForSession(sessionID)`, `UnreadCountForSession(sessionID)`; test helpers `testDB(t)`, `seedSession(t, d, id)`, `strPtr(s)`.
- Produces: `DismissEvent(sessionID, eventID string) error`. The four unread queries (`UnreadForSession`, `UnreadCountForSession`, `UnreadResourcesForSession`, `HumanUnreadCountForSession`) now exclude dismissed events.

- [ ] **Step 1: Write the failing test**

Create `db/dismissal_test.go`:

```go
package db

import (
	"testing"
	"time"
)

func TestDismissEventExcludesFromUnread(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "sess-dismiss")

	// Cursor at epoch so all events are "after" it.
	if err := d.AdvanceCursor("sess-dismiss", "1970-01-01T00:00:00Z"); err != nil {
		t.Fatalf("AdvanceCursor: %v", err)
	}

	// Two broadcast events targeting everyone.
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range []string{"evt-keep", "evt-drop"} {
		e := Event{
			ID: id, TS: now, Source: "test", Type: "status",
			Title: "t", Broadcast: true,
		}
		if err := d.InsertEvent(e, nil, nil); err != nil {
			t.Fatalf("InsertEvent %s: %v", id, err)
		}
	}

	total, _, err := d.UnreadCountForSession("sess-dismiss")
	if err != nil {
		t.Fatalf("UnreadCountForSession: %v", err)
	}
	if total != 2 {
		t.Fatalf("before dismiss: got %d unread, want 2", total)
	}

	if err := d.DismissEvent("sess-dismiss", "evt-drop"); err != nil {
		t.Fatalf("DismissEvent: %v", err)
	}

	total, _, err = d.UnreadCountForSession("sess-dismiss")
	if err != nil {
		t.Fatalf("UnreadCountForSession after dismiss: %v", err)
	}
	if total != 1 {
		t.Fatalf("after dismiss: got %d unread, want 1", total)
	}

	unread, err := d.UnreadForSession("sess-dismiss")
	if err != nil {
		t.Fatalf("UnreadForSession: %v", err)
	}
	if len(unread) != 1 || unread[0].ID != "evt-keep" {
		t.Fatalf("expected only evt-keep, got %+v", unread)
	}
}

func TestDismissEventIsPerSession(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "sess-a")
	seedSession(t, d, "sess-b")
	d.AdvanceCursor("sess-a", "1970-01-01T00:00:00Z")
	d.AdvanceCursor("sess-b", "1970-01-01T00:00:00Z")

	now := time.Now().UTC().Format(time.RFC3339)
	e := Event{ID: "shared", TS: now, Source: "test", Type: "status", Title: "t", Broadcast: true}
	if err := d.InsertEvent(e, nil, nil); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	if err := d.DismissEvent("sess-a", "shared"); err != nil {
		t.Fatalf("DismissEvent: %v", err)
	}

	aCount, _, _ := d.UnreadCountForSession("sess-a")
	bCount, _, _ := d.UnreadCountForSession("sess-b")
	if aCount != 0 {
		t.Errorf("sess-a: got %d, want 0", aCount)
	}
	if bCount != 1 {
		t.Errorf("sess-b: got %d, want 1", bCount)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./db/ -run TestDismissEvent -v`
Expected: FAIL — `DismissEvent` undefined (compile error).

- [ ] **Step 3: Add `DismissEvent` to db/events.go**

Append to `db/events.go`:

```go
// DismissEvent records that a session has explicitly dismissed an event.
// Dismissed events are excluded from all unread queries for that session,
// independent of the cursor. Idempotent.
func (db *DB) DismissEvent(sessionID, eventID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(`
		INSERT INTO dismissed_events (session_id, event_id, dismissed_at)
		VALUES (?, ?, ?)
		ON CONFLICT(session_id, event_id) DO NOTHING
	`, sessionID, eventID, now)
	if err != nil {
		return fmt.Errorf("failed to dismiss event %q for session %q: %w", eventID, sessionID, err)
	}
	return nil
}
```

Add `"time"` to the import block in `db/events.go` (currently imports only
`database/sql` and `fmt`).

- [ ] **Step 4: Add the exclusion clause to the four unread queries**

Define a shared constant near the top of `db/events.go` (below
`inboxExcludedTypesSQL`):

```go
// dismissedExclusionSQL excludes events explicitly dismissed by the session.
// The bound parameter is the session ID; place it immediately before the
// existing session-ID parameter in each query's arg list.
const dismissedExclusionSQL = `AND NOT EXISTS (
		SELECT 1 FROM dismissed_events d WHERE d.session_id = ? AND d.event_id = e.id
	)`
```

In each of the four queries below, insert `dismissedExclusionSQL` immediately
after the `inboxExcludedTypesSQL` line, and add one more `sessionID` argument
**at the front of the existing arg list** (the exclusion's `?` is the first `?`
in the query since the subscription join's `?` already precedes `WHERE`).

IMPORTANT — parameter ordering: each query currently starts its args with the
subscription-join `sessionID`, then `cursor`. The `dismissedExclusionSQL` `?`
appears in the WHERE clause, which comes AFTER the subscription join's `?` and
AFTER the `e.ts > ?` cursor `?`. So the new `sessionID` arg goes in position 3
(after the join `sessionID` and the `cursor`). Concretely, change each arg list
from `(sessionID, cursor, ...)` to `(sessionID, cursor, sessionID, ...)`.

**4a. `UnreadForSession`** — after `` ` + inboxExcludedTypesSQL + ` `` add
`` + dismissedExclusionSQL + ` ``. Change the query call from:

```go
rows, err := db.conn.Query(query, sessionID, cursor, sessionID, session.Branch, repoBranch, session.Role)
```
to:
```go
rows, err := db.conn.Query(query, sessionID, cursor, sessionID, sessionID, session.Branch, repoBranch, session.Role)
```

**4b. `UnreadCountForSession`** — same insertion in the query string; change:
```go
rows, err := db.conn.Query(query, sessionID, cursor, sessionID, session.Branch, repoBranch, session.Role)
```
to:
```go
rows, err := db.conn.Query(query, sessionID, cursor, sessionID, sessionID, session.Branch, repoBranch, session.Role)
```

**4c. `UnreadResourcesForSession`** — same insertion; change:
```go
rows, err := db.conn.Query(query, sessionID, cursor, sessionID, session.Branch, repoBranch, session.Role)
```
to:
```go
rows, err := db.conn.Query(query, sessionID, cursor, sessionID, sessionID, session.Branch, repoBranch, session.Role)
```

**4d. `HumanUnreadCountForSession`** — same insertion; change:
```go
err = db.conn.QueryRow(query, sessionID, cursor, sessionID, session.Branch, repoBranch, session.Role).Scan(&count)
```
to:
```go
err = db.conn.QueryRow(query, sessionID, cursor, sessionID, sessionID, session.Branch, repoBranch, session.Role).Scan(&count)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./db/ -run TestDismiss -v`
Expected: PASS for both `TestDismissEventExcludesFromUnread` and
`TestDismissEventIsPerSession`.

- [ ] **Step 6: Run the full db test suite (no regressions)**

Run: `go test ./db/ -v`
Expected: all existing tests still PASS (the extra `?` params are wired correctly).

- [ ] **Step 7: Commit**

```bash
git add db/events.go db/dismissal_test.go
git commit --signoff -m "feat(db): DismissEvent method and unread-query exclusion

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Prune dismissed rows on cursor advance

**Files:**
- Modify: `db/cursors.go` (`AdvanceCursor`, `AdvanceBothCursors`, `CatchUpHumanCursor`)
- Test: `db/dismissal_test.go` (add case)

**Interfaces:**
- Consumes: `dismissed_events` (Task 1), `DismissEvent` (Task 2), `AdvanceBothCursors`, `GetCursor`, `GetHumanCursor`.
- Produces: private helper `pruneDismissedBehindCursor(sessionID string) error` and its invocation from cursor-advance methods.

- [ ] **Step 1: Write the failing test**

Add to `db/dismissal_test.go`:

```go
func TestPruneDismissedBehindCursor(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "sess-prune")
	d.AdvanceCursor("sess-prune", "1970-01-01T00:00:00Z")

	e := Event{ID: "evt-old", TS: "2026-01-01T00:00:00Z", Source: "test", Type: "status", Title: "t", Broadcast: true}
	if err := d.InsertEvent(e, nil, nil); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
	if err := d.DismissEvent("sess-prune", "evt-old"); err != nil {
		t.Fatalf("DismissEvent: %v", err)
	}

	// Row exists before advancing.
	var before int
	d.conn.QueryRow(`SELECT COUNT(*) FROM dismissed_events WHERE session_id = ?`, "sess-prune").Scan(&before)
	if before != 1 {
		t.Fatalf("before advance: got %d rows, want 1", before)
	}

	// Advance cursor past the event.
	if err := d.AdvanceBothCursors("sess-prune", "2026-06-01T00:00:00Z"); err != nil {
		t.Fatalf("AdvanceBothCursors: %v", err)
	}

	var after int
	d.conn.QueryRow(`SELECT COUNT(*) FROM dismissed_events WHERE session_id = ?`, "sess-prune").Scan(&after)
	if after != 0 {
		t.Fatalf("after advance: got %d rows, want 0 (pruned)", after)
	}
}

func TestPruneKeepsRowsAheadOfCursor(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "sess-keep")
	d.AdvanceCursor("sess-keep", "1970-01-01T00:00:00Z")

	e := Event{ID: "evt-future", TS: "2027-01-01T00:00:00Z", Source: "test", Type: "status", Title: "t", Broadcast: true}
	d.InsertEvent(e, nil, nil)
	d.DismissEvent("sess-keep", "evt-future")

	// Advance to a time BEFORE the event.
	if err := d.AdvanceBothCursors("sess-keep", "2026-06-01T00:00:00Z"); err != nil {
		t.Fatalf("AdvanceBothCursors: %v", err)
	}

	var after int
	d.conn.QueryRow(`SELECT COUNT(*) FROM dismissed_events WHERE session_id = ?`, "sess-keep").Scan(&after)
	if after != 1 {
		t.Fatalf("row for still-future event should be kept, got %d", after)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./db/ -run TestPrune -v`
Expected: FAIL — `TestPruneDismissedBehindCursor` gets 1 (not pruned yet).

- [ ] **Step 3: Add the prune helper and wire it in**

Add to `db/cursors.go`:

```go
// pruneDismissedBehindCursor removes dismissed_events rows whose event has
// scrolled behind BOTH cursors for the session. Those rows are redundant
// because the event is already excluded by the ts > cursor filter.
func (db *DB) pruneDismissedBehindCursor(sessionID string) error {
	// Use the lower of the two cursors so a row is only pruned once the event
	// is behind both. GetHumanCursor coalesces to last_seen_ts when NULL.
	agent, err := db.GetCursor(sessionID)
	if err != nil {
		return err
	}
	human, err := db.GetHumanCursor(sessionID)
	if err != nil {
		return err
	}
	threshold := agent
	if human != "" && human < threshold {
		threshold = human
	}
	if threshold == "" {
		return nil
	}
	_, err = db.conn.Exec(`
		DELETE FROM dismissed_events
		WHERE session_id = ?
		  AND event_id IN (SELECT id FROM events WHERE ts <= ?)
	`, sessionID, threshold)
	if err != nil {
		return fmt.Errorf("failed to prune dismissed events for %q: %w", sessionID, err)
	}
	return nil
}
```

In `AdvanceCursor`, `AdvanceBothCursors`, and `CatchUpHumanCursor`, after the
successful `Exec`/before `return nil`, call the prune (ignore its error so a
prune failure never breaks a cursor advance, but log-free is fine here — return
it only if you prefer strictness; to keep cursor advance robust, swallow it):

```go
	_ = db.pruneDismissedBehindCursor(sessionID)
	return nil
```

Apply to all three methods (insert the two lines just before their existing
`return nil`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./db/ -run TestPrune -v`
Expected: PASS for both prune tests.

- [ ] **Step 5: Run full db suite**

Run: `go test ./db/ -v`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add db/cursors.go db/dismissal_test.go
git commit --signoff -m "feat(db): prune dismissed_events behind cursor on advance

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: API endpoint `POST /api/actions/dismiss-event`

**Files:**
- Modify: `cmd/api/actions.go` (add request type + handler)
- Modify: `cmd/api/server.go:41` (register route)

**Interfaces:**
- Consumes: `DismissEvent(sessionID, eventID)` (Task 2); `db.Open(db.DefaultPath())`, `writeError`, `writeJSON`.
- Produces: route `POST /api/actions/dismiss-event` accepting `{ "session_id": "...", "event_id": "..." }`, returning `{ "success": true }`.

- [ ] **Step 1: Add request type and handler to actions.go**

After `dismissInboxRequest` (line ~22) add:

```go
type dismissEventRequest struct {
	SessionID string `json:"session_id"`
	EventID   string `json:"event_id"`
}
```

After `handleDismissInbox` add:

```go
func (s *Server) handleDismissEvent(w http.ResponseWriter, r *http.Request) {
	var req dismissEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	if req.SessionID == "" || req.EventID == "" {
		writeError(w, http.StatusBadRequest, "session_id and event_id are required")
		return
	}

	writableDB, err := db.Open(db.DefaultPath())
	if err != nil {
		s.Logger.Printf("Error opening writable DB: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer writableDB.Close()

	if err := writableDB.DismissEvent(req.SessionID, req.EventID); err != nil {
		s.Logger.Printf("Error dismissing event %s for %s: %v", req.EventID, req.SessionID, err)
		writeError(w, http.StatusInternalServerError, "Failed to dismiss event")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
```

- [ ] **Step 2: Register the route**

In `cmd/api/server.go`, after the `dismiss-inbox` line (41):

```go
	mux.HandleFunc("POST /api/actions/dismiss-event", s.handleDismissEvent)
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: builds clean.

- [ ] **Step 4: Commit**

```bash
git add cmd/api/actions.go cmd/api/server.go
git commit --signoff -m "feat(api): dismiss-event endpoint

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: CLI `handler dismiss-event`

**Files:**
- Create: `cmd/dismiss_event.go`

**Interfaces:**
- Consumes: `openDB()`, `resolveSessionID(cmd)`, `DismissEvent(sessionID, eventID)`.
- Produces: `handler dismiss-event --session-id <id> --event <id>` command.

- [ ] **Step 1: Create the command**

```go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var dismissEventCmd = &cobra.Command{
	Use:   "dismiss-event",
	Short: "Dismiss a single event from a session's inbox without advancing the cursor",
	RunE:  runDismissEvent,
}

func init() {
	dismissEventCmd.GroupID = "agent"
	rootCmd.AddCommand(dismissEventCmd)
	dismissEventCmd.Flags().String("session-id", "", "session ID (auto-detected if omitted)")
	dismissEventCmd.Flags().String("event", "", "event ID to dismiss (required)")
}

func runDismissEvent(cmd *cobra.Command, args []string) error {
	eventID, _ := cmd.Flags().GetString("event")
	if eventID == "" {
		return fmt.Errorf("--event is required")
	}

	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	sessionID, err := resolveSessionID(cmd)
	if err != nil {
		return fmt.Errorf("could not determine session: %w", err)
	}

	if err := d.DismissEvent(sessionID, eventID); err != nil {
		return fmt.Errorf("failed to dismiss event: %w", err)
	}

	fmt.Printf("✓ Dismissed event %s from session %s\n", eventID, sessionID)
	return nil
}
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: builds clean.

- [ ] **Step 3: Manual smoke test**

Run: `go run . dismiss-event --event nonexistent-id --session-id nonexistent-session`
Expected: prints the ✓ line (DismissEvent is idempotent / no FK constraint on
event_id, so it succeeds even for unknown IDs — acceptable; the row is harmless
and pruned later).

- [ ] **Step 4: Commit**

```bash
git add cmd/dismiss_event.go
git commit --signoff -m "feat(cli): dismiss-event command

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Frontend — API client `dismissEvent`

**Files:**
- Modify: `ui/src/api/client.ts` (after `dismissInbox`, ~line 118)

**Interfaces:**
- Consumes: `fetchJSON`, `ActionResponse` (existing).
- Produces: `dismissEvent(sessionId, eventId): Promise<ActionResponse>`.

- [ ] **Step 1: Add the client function**

After `dismissInbox`:

```ts
export async function dismissEvent(sessionId: string, eventId: string): Promise<ActionResponse> {
  return fetchJSON<ActionResponse>("/api/actions/dismiss-event", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_id: sessionId, event_id: eventId }),
  })
}
```

- [ ] **Step 2: Type check**

Run: `cd ui && npx tsc -b`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add ui/src/api/client.ts
git commit --signoff -m "feat(ui): dismissEvent API client

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Frontend — enhance InboxDialog

**Files:**
- Modify: `ui/src/components/InboxDialog.tsx` (full rewrite of the render + state)

**Interfaces:**
- Consumes: `dismissEvent(sessionId, eventId)` (Task 6), `formatEventType(type)` from `@/utils/formatLabel`, existing `getSessionInbox`, `dismissInbox`, `switchSession`, `queryKeys`, `timeAgo`, shadcn `Button`/`Badge`/`ScrollArea`/`Separator`/`Dialog*`, lucide `ChevronRight`/`ChevronDown`/`Trash2`.

- [ ] **Step 1: Add imports**

At the top of `InboxDialog.tsx`, add to the existing imports:

```ts
import { ChevronRight, ChevronDown, Trash2 } from "lucide-react"
import { formatEventType } from "@/utils/formatLabel"
import { dismissEvent } from "@/api/client"
```

(`dismissEvent` joins the existing `getSessionInbox, dismissInbox, switchSession`
import from `@/api/client` — either extend that line or add a new one.)

- [ ] **Step 2: Add per-event dismiss state + mutation**

Inside the component, after the existing `confirmDismiss` state:

```ts
  const [confirmDismissEvent, setConfirmDismissEvent] = useState<string | null>(null)

  const dismissEventMutation = useMutation({
    mutationFn: (eventId: string) => dismissEvent(sessionId!, eventId),
    onSuccess: () => {
      toast.success("Event dismissed")
      queryClient.invalidateQueries({ queryKey: queryKeys.inbox(sessionId!) })
      queryClient.invalidateQueries({ queryKey: ["sessions"] })
    },
    onError: (e) => {
      console.error(e)
      toast.error("Failed to dismiss event")
    },
    onSettled: () => setConfirmDismissEvent(null),
  })
```

- [ ] **Step 3: Widen the modal and grow the scroll area**

Change:
```tsx
      <DialogContent className="max-w-2xl">
```
to:
```tsx
      <DialogContent className="max-w-4xl">
```
and change:
```tsx
        <ScrollArea className="max-h-[400px]">
```
to:
```tsx
        <ScrollArea className="max-h-[600px]">
```

- [ ] **Step 4: Replace the event row rendering**

Replace the `{events.map((ev) => ( ... ))}` block with:

```tsx
          {events.map((ev) => {
            const isExpanded = expanded.has(ev.id)
            const confirming = confirmDismissEvent === ev.id
            return (
              <div key={ev.id} className="py-2">
                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    className="flex items-center gap-2 flex-1 min-w-0 text-left select-none"
                    onClick={() => ev.body && toggleExpanded(ev.id)}
                  >
                    {ev.body ? (
                      isExpanded
                        ? <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" />
                        : <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />
                    ) : (
                      <span className="w-4 shrink-0" />
                    )}
                    <Badge variant="outline" className="text-xs shrink-0">
                      {formatEventType(ev.type)}
                    </Badge>
                    <span className="text-sm truncate flex-1">{ev.title}</span>
                  </button>
                  <span className="text-xs text-muted-foreground shrink-0">
                    {timeAgo(ev.ts)}
                  </span>
                  {ev.author && (
                    <span className="text-xs text-muted-foreground shrink-0">
                      {ev.author}
                    </span>
                  )}
                  {!confirming ? (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 w-7 p-0 shrink-0"
                      title="Dismiss this event"
                      onClick={() => setConfirmDismissEvent(ev.id)}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  ) : (
                    <div className="flex items-center gap-1 shrink-0">
                      <span className="text-xs text-muted-foreground">Dismiss?</span>
                      <Button
                        variant="destructive"
                        size="sm"
                        className="h-7"
                        onClick={() => dismissEventMutation.mutate(ev.id)}
                      >
                        Confirm
                      </Button>
                      <Button
                        variant="outline"
                        size="sm"
                        className="h-7"
                        onClick={() => setConfirmDismissEvent(null)}
                      >
                        Cancel
                      </Button>
                    </div>
                  )}
                </div>
                {isExpanded && ev.body && (
                  <pre className="mt-1 ml-6 text-xs text-muted-foreground whitespace-pre-wrap bg-muted/50 rounded p-2">
                    {ev.body}
                  </pre>
                )}
                <Separator className="mt-2" />
              </div>
            )
          })}
```

- [ ] **Step 5: Type check**

Run: `cd ui && npx tsc -b`
Expected: no errors.

- [ ] **Step 6: Manual verification (ask user to have `make dev` running)**

Open the UI at http://localhost:5173, open a session inbox with multiple events:
- Each row shows a chevron (right when collapsed, down when expanded); rows
  without a body show no chevron and don't expand.
- Event type badges are pretty-printed (e.g. "CI Passed", not "ci_passed").
- Modal is visibly wider and taller.
- Clicking the trash icon swaps to "Dismiss? Confirm / Cancel"; Confirm removes
  the row, decrements the session's unread count, and clears the resource dot if
  it was the only unread for that resource. Cancel restores the trash icon.
- "Dismiss all" still works and closes the modal.

- [ ] **Step 7: Commit**

```bash
git add ui/src/components/InboxDialog.tsx
git commit --signoff -m "feat(ui): per-event dismiss, chevrons, pretty types, larger inbox modal

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Install, migrate existing DB, and end-to-end verification

**Files:** none (operational)

**Interfaces:** Consumes everything above.

- [ ] **Step 1: Add the column/table to the running DB**

The migration in Task 1 handles this automatically on next `db.Open`, but to be
explicit for the dev's already-running DB:

Run: `sqlite3 ~/.agent-handler/data/handler.db "CREATE TABLE IF NOT EXISTS dismissed_events (session_id TEXT NOT NULL, event_id TEXT NOT NULL, dismissed_at TEXT NOT NULL, PRIMARY KEY (session_id, event_id)); CREATE INDEX IF NOT EXISTS idx_dismissed_events_session ON dismissed_events(session_id);"`
Expected: no output (success).

- [ ] **Step 2: Build web + install binary**

Run: `go build -o bin/handler . && cd ui && npx tsc -b && cd .. && NONINTERACTIVE=1 make install`
Expected: builds and installs cleanly.

- [ ] **Step 3: CLI round-trip**

Pick a real session with unreads:
Run: `handler status` (find a session id + an event id via `handler log --global --json | head`)
Run: `handler dismiss-event --session-id <id> --event <event-id>`
Then: `handler unread --session-id <id>` (or the statusline) shows the count
dropped by one and the dismissed event absent.

- [ ] **Step 4: Confirm global timeline unaffected**

Run: `handler log --global --json` — the dismissed event still appears in the
global timeline (dismissal is inbox-scoped only).

- [ ] **Step 5: Final commit if any tracked files changed**

Only `bin/`/`ui/dist` artifacts should differ; do not commit build artifacts
that are gitignored. If `ui/tsconfig.app.tsbuildinfo` or `package-lock.json`
changed and are tracked, add them:

```bash
git add ui/tsconfig.app.tsbuildinfo
git commit --signoff -m "chore: rebuild ui after dismissal feature

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Notes for the implementer

- The trickiest part is Task 2 Step 4: the SQL parameter ordering. The exclusion
  subquery's `?` lands in the WHERE clause. Each affected query's arg list must
  become `(joinSessionID, cursor, dismissSessionID, sessionID, branch, repoBranch, role)`
  — i.e. insert one more `sessionID` right after `cursor`. Run `go test ./db/ -v`
  after wiring; a wrong order silently returns wrong counts, which the Task 2
  tests catch.
- `rules/agent-handler.md` documents skills/CLI commands. `dismiss-event` is an
  operator/internal command, not a user-facing skill, so it does NOT need a
  rules-file entry (the rules file lists slash commands and emit types, not every
  cobra subcommand). Leave it out.
