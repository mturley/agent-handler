# Peek Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cache full terminal snapshots in a `peek_state` table so multiple consumers (statusline, web server, peek --list-need-input, switch) share results instead of each doing redundant `cmux capture-pane` scans.

**Architecture:** New `peek_state` table stores full terminal content + derived `needs_input` flag. A `PeekScanWithCache` helper checks freshness before scanning. Existing consumers (`findSessionsAwaitingApproval` and callers) are migrated to use the cache. `handler peek --session X` (user-invoked) remains uncached.

**Tech Stack:** Go, SQLite

## Global Constraints

- Go binary, pure-Go SQLite (`modernc.org/sqlite`)
- All timestamps ISO 8601 UTC
- Tests must pass: `go test ./...`
- Use `--signoff` on all commits
- Follow existing patterns in `db/` and `cmd/`

---

### Task 1: Peek State DB Layer

**Files:**
- Create: `db/peek.go`
- Create: `db/peek_test.go`
- Modify: `db/schema.sql`

**Interfaces:**
- Consumes: `db.DB` (existing)
- Produces:
  - `db.PeekState` struct: `SessionID string`, `Content string`, `NeedsInput bool`, `Reason string`, `UpdatedAt string`
  - `db.UpsertPeekState(sessionID, content string, needsInput bool, reason, updatedAt string) error`
  - `db.GetPeekState(sessionID string) (*PeekState, error)`
  - `db.ListPeekStates() ([]PeekState, error)`
  - `db.PeekStatesAge() (time.Duration, error)` — returns time since the newest `updated_at`
  - `db.DeletePeekStatesForSessions(sessionIDs []string) error`

- [ ] **Step 1: Write tests**

Create `db/peek_test.go`:

```go
package db

import (
	"testing"
	"time"
)

func TestUpsertAndGetPeekState(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "peek-test")

	now := time.Now().UTC().Format(time.RFC3339)
	err := d.UpsertPeekState("peek-test", "terminal content here", true, "awaiting approval", now)
	if err != nil {
		t.Fatalf("UpsertPeekState failed: %v", err)
	}

	ps, err := d.GetPeekState("peek-test")
	if err != nil {
		t.Fatalf("GetPeekState failed: %v", err)
	}
	if ps == nil {
		t.Fatal("expected non-nil PeekState")
	}
	if ps.Content != "terminal content here" {
		t.Errorf("expected content %q, got %q", "terminal content here", ps.Content)
	}
	if !ps.NeedsInput {
		t.Error("expected NeedsInput true")
	}
	if ps.Reason != "awaiting approval" {
		t.Errorf("expected reason %q, got %q", "awaiting approval", ps.Reason)
	}
}

func TestUpsertPeekStateOverwrites(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "peek-overwrite")

	now := time.Now().UTC().Format(time.RFC3339)
	d.UpsertPeekState("peek-overwrite", "old content", true, "awaiting approval", now)
	d.UpsertPeekState("peek-overwrite", "new content", false, "", now)

	ps, err := d.GetPeekState("peek-overwrite")
	if err != nil {
		t.Fatalf("GetPeekState failed: %v", err)
	}
	if ps.Content != "new content" {
		t.Errorf("expected updated content, got %q", ps.Content)
	}
	if ps.NeedsInput {
		t.Error("expected NeedsInput false after update")
	}
}

func TestGetPeekStateNotFound(t *testing.T) {
	d := testDB(t)

	ps, err := d.GetPeekState("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ps != nil {
		t.Error("expected nil for nonexistent session")
	}
}

func TestListPeekStates(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "peek-list-1")
	seedSession(t, d, "peek-list-2")

	now := time.Now().UTC().Format(time.RFC3339)
	d.UpsertPeekState("peek-list-1", "content 1", true, "awaiting approval", now)
	d.UpsertPeekState("peek-list-2", "content 2", false, "", now)

	states, err := d.ListPeekStates()
	if err != nil {
		t.Fatalf("ListPeekStates failed: %v", err)
	}
	if len(states) != 2 {
		t.Errorf("expected 2 states, got %d", len(states))
	}
}

func TestPeekStatesAge(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "peek-age")

	// With no rows, age should be very large
	age, err := d.PeekStatesAge()
	if err != nil {
		t.Fatalf("PeekStatesAge failed: %v", err)
	}
	if age < time.Hour {
		t.Errorf("expected large age for empty table, got %v", age)
	}

	// Insert a row with current timestamp
	now := time.Now().UTC().Format(time.RFC3339)
	d.UpsertPeekState("peek-age", "content", false, "", now)

	age, err = d.PeekStatesAge()
	if err != nil {
		t.Fatalf("PeekStatesAge failed: %v", err)
	}
	if age > 2*time.Second {
		t.Errorf("expected small age for fresh data, got %v", age)
	}
}

func TestDeletePeekStatesForSessions(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "peek-del-1")
	seedSession(t, d, "peek-del-2")

	now := time.Now().UTC().Format(time.RFC3339)
	d.UpsertPeekState("peek-del-1", "c1", false, "", now)
	d.UpsertPeekState("peek-del-2", "c2", false, "", now)

	err := d.DeletePeekStatesForSessions([]string{"peek-del-1"})
	if err != nil {
		t.Fatalf("DeletePeekStatesForSessions failed: %v", err)
	}

	ps, _ := d.GetPeekState("peek-del-1")
	if ps != nil {
		t.Error("expected nil after delete")
	}
	ps, _ = d.GetPeekState("peek-del-2")
	if ps == nil {
		t.Error("expected non-nil for non-deleted session")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./db/ -run TestUpsertAndGetPeekState -v`
Expected: compilation error — types don't exist yet.

- [ ] **Step 3: Add peek_state table to schema.sql**

Append to `db/schema.sql`:

```sql
CREATE TABLE IF NOT EXISTS peek_state (
    session_id TEXT PRIMARY KEY,
    content TEXT NOT NULL,
    needs_input INTEGER NOT NULL DEFAULT 0,
    reason TEXT,
    updated_at TEXT NOT NULL
);
```

- [ ] **Step 4: Implement db/peek.go**

Create `db/peek.go`:

```go
package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type PeekState struct {
	SessionID  string `json:"session_id"`
	Content    string `json:"content"`
	NeedsInput bool   `json:"needs_input"`
	Reason     string `json:"reason"`
	UpdatedAt  string `json:"updated_at"`
}

func (db *DB) UpsertPeekState(sessionID, content string, needsInput bool, reason, updatedAt string) error {
	needsInputInt := 0
	if needsInput {
		needsInputInt = 1
	}
	_, err := db.conn.Exec(`
		INSERT INTO peek_state (session_id, content, needs_input, reason, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			content = excluded.content,
			needs_input = excluded.needs_input,
			reason = excluded.reason,
			updated_at = excluded.updated_at
	`, sessionID, content, needsInputInt, reason, updatedAt)
	if err != nil {
		return fmt.Errorf("failed to upsert peek state: %w", err)
	}
	return nil
}

func (db *DB) GetPeekState(sessionID string) (*PeekState, error) {
	var ps PeekState
	var needsInputInt int
	err := db.conn.QueryRow(`
		SELECT session_id, content, needs_input, COALESCE(reason, ''), updated_at
		FROM peek_state WHERE session_id = ?
	`, sessionID).Scan(&ps.SessionID, &ps.Content, &needsInputInt, &ps.Reason, &ps.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get peek state: %w", err)
	}
	ps.NeedsInput = needsInputInt == 1
	return &ps, nil
}

func (db *DB) ListPeekStates() ([]PeekState, error) {
	rows, err := db.conn.Query(`
		SELECT session_id, content, needs_input, COALESCE(reason, ''), updated_at
		FROM peek_state ORDER BY session_id
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list peek states: %w", err)
	}
	defer rows.Close()

	var states []PeekState
	for rows.Next() {
		var ps PeekState
		var needsInputInt int
		if err := rows.Scan(&ps.SessionID, &ps.Content, &needsInputInt, &ps.Reason, &ps.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan peek state: %w", err)
		}
		ps.NeedsInput = needsInputInt == 1
		states = append(states, ps)
	}
	return states, rows.Err()
}

// PeekStatesAge returns the time since the newest updated_at in peek_state.
// Returns a very large duration if the table is empty.
func (db *DB) PeekStatesAge() (time.Duration, error) {
	var newest string
	err := db.conn.QueryRow(`SELECT MAX(updated_at) FROM peek_state`).Scan(&newest)
	if err != nil || newest == "" {
		return 24 * time.Hour, nil
	}
	t, err := time.Parse(time.RFC3339, newest)
	if err != nil {
		return 24 * time.Hour, nil
	}
	return time.Since(t), nil
}

func (db *DB) DeletePeekStatesForSessions(sessionIDs []string) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	placeholders := make([]string, len(sessionIDs))
	args := make([]interface{}, len(sessionIDs))
	for i, id := range sessionIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf("DELETE FROM peek_state WHERE session_id IN (%s)",
		strings.Join(placeholders, ", "))
	_, err := db.conn.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to delete peek states: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./db/ -v`
Expected: all tests pass.

- [ ] **Step 6: Run full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 7: Commit**

```bash
git add db/peek.go db/peek_test.go db/schema.sql
git commit --signoff -m "feat: peek_state table for caching terminal snapshots"
```

---

### Task 2: PeekScanWithCache Helper

**Files:**
- Create: `cmd/peek_cache.go`
- Modify: `cmd/root.go` (remove `findSessionsAwaitingApproval`)

**Interfaces:**
- Consumes: `db.PeekState`, `db.UpsertPeekState`, `db.ListPeekStates`, `db.PeekStatesAge` from Task 1; `terminal.NewBackend`, `terminal.NeedsInput` (existing); `discover.IsSessionProcess` (existing)
- Produces:
  - `PeekScanWithCache(d *db.DB, maxAge time.Duration) []db.PeekState` — returns cached peek states if fresh, otherwise does a full scan and updates the cache
  - `findSessionsAwaitingApproval(d *db.DB) []db.Session` — moved here, now uses the cache

- [ ] **Step 1: Create cmd/peek_cache.go**

```go
package cmd

import (
	"time"

	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
	"github.com/mturley/agent-handler/terminal"
)

// PeekScanWithCache returns cached peek states if the cache is fresh (within maxAge),
// otherwise performs a full cmux capture-pane scan on all peekable sessions and
// updates the cache.
func PeekScanWithCache(d *db.DB, maxAge time.Duration) []db.PeekState {
	age, err := d.PeekStatesAge()
	if err == nil && age <= maxAge {
		states, err := d.ListPeekStates()
		if err == nil {
			return states
		}
	}

	// Cache is stale — do a fresh scan
	sessions, err := d.ListSessions(false, 1000, 0)
	if err != nil {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	var results []db.PeekState

	for _, s := range sessions {
		if s.TerminalType == "" || s.TerminalID == "" || s.Role == "handler" {
			continue
		}
		if s.PID > 0 && !discover.IsSessionProcess(s.PID, s.SessionID) {
			continue
		}

		backend, err := terminal.NewBackend(s.TerminalType)
		if err != nil {
			continue
		}

		content, err := backend.Capture(s.TerminalID, 0)
		if err != nil {
			continue
		}

		needsInput, reason := terminal.NeedsInput(content)

		d.UpsertPeekState(s.SessionID, content, needsInput, reason, now)

		results = append(results, db.PeekState{
			SessionID:  s.SessionID,
			Content:    content,
			NeedsInput: needsInput,
			Reason:     reason,
			UpdatedAt:  now,
		})
	}

	return results
}

// findSessionsAwaitingApproval returns sessions that need input, using the peek cache.
func findSessionsAwaitingApproval(d *db.DB) []db.Session {
	states := PeekScanWithCache(d, 5*time.Second)

	sessions, err := d.ListSessions(false, 1000, 0)
	if err != nil {
		return nil
	}

	sessionMap := make(map[string]db.Session)
	for _, s := range sessions {
		sessionMap[s.SessionID] = s
	}

	var awaiting []db.Session
	for _, ps := range states {
		if ps.NeedsInput {
			if s, ok := sessionMap[ps.SessionID]; ok {
				awaiting = append(awaiting, s)
			}
		}
	}
	return awaiting
}
```

- [ ] **Step 2: Remove findSessionsAwaitingApproval from cmd/root.go**

In `cmd/root.go`, delete the `findSessionsAwaitingApproval` function (lines 126-154) and remove the `terminal` import if it's no longer used. The function is now in `cmd/peek_cache.go`.

Check which imports are still needed in root.go after the removal — `discover` and `terminal` may have been used only by that function.

- [ ] **Step 3: Build and test**

Run: `go build ./...`
Expected: compiles successfully — all callers of `findSessionsAwaitingApproval` find it in `peek_cache.go` (same package).

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add cmd/peek_cache.go cmd/root.go
git commit --signoff -m "feat: PeekScanWithCache helper with 5-second freshness threshold"
```

---

### Task 3: Migrate Consumers and Cleanup Integration

**Files:**
- Modify: `cmd/statusline.go` (scanAwaitingApproval now uses cache via findSessionsAwaitingApproval — already the case, no change needed if Task 2 is correct)
- Modify: `cmd/peek.go` (--list-need-input uses cache)
- Modify: `cmd/cleanup.go` (delete peek_state for archived sessions)

**Interfaces:**
- Consumes: `PeekScanWithCache`, `findSessionsAwaitingApproval` from Task 2; `db.DeletePeekStatesForSessions` from Task 1
- Produces: updated consumer behavior

- [ ] **Step 1: Verify statusline and switch already work**

The statusline calls `scanAwaitingApproval` → `findSessionsAwaitingApproval` → now uses cache (from Task 2). The switch command calls `findSessionsAwaitingApproval` directly → also uses cache. No changes needed to these files.

Verify: `grep -n "findSessionsAwaitingApproval" cmd/statusline.go cmd/switch.go`
Expected: both reference the function, which now lives in `peek_cache.go` and uses the cache.

- [ ] **Step 2: Update peek.go --list-need-input to use cache**

In `cmd/peek.go`, the `runListNeedInput` function calls `findSessionsAwaitingApproval(d)` directly — this already uses the cache after Task 2. Verify no changes needed.

Run: `grep -n "findSessionsAwaitingApproval" cmd/peek.go`
Expected: one reference, which calls the cache-backed version.

- [ ] **Step 3: Add peek_state cleanup to handler cleanup**

In `cmd/cleanup.go`, after the `d.ArchiveSessions(toArchive)` call, add:

```go
// Clean up peek_state for archived sessions
d.DeletePeekStatesForSessions(toArchive)
```

- [ ] **Step 4: Build and test**

Run: `go build ./... && go test ./...`
Expected: all tests pass.

- [ ] **Step 5: Test manually**

```bash
make build-cli
# Run statusline to trigger a peek cache write
./bin/handler statusline --session $(handler session-name 2>/dev/null || echo "unknown") 2>/dev/null
# Check that peek_state has data
handler query "SELECT session_id, needs_input, length(content), updated_at FROM peek_state" 2>/dev/null
# Run peek --list-need-input (should use cache)
./bin/handler peek --list-need-input --json 2>/dev/null
```

- [ ] **Step 6: Commit**

```bash
git add cmd/cleanup.go
git commit --signoff -m "feat: peek cache cleanup on handler cleanup"
```
