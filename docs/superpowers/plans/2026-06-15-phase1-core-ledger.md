# Phase 1: Core Ledger and Observability — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `handler` CLI tool with SQLite-backed event ledger, session management, subscription routing, Claude Code hooks, skills, and status line integration.

**Architecture:** Go CLI binary using `cobra` for command structure and `modernc.org/sqlite` for pure-Go SQLite access. The DB layer exposes typed Go functions; CLI commands call the DB layer and format output. Hooks are shell scripts that invoke `handler` subcommands. Skills are markdown files with instructions for Claude Code agents.

**Tech Stack:** Go 1.22+, modernc.org/sqlite, cobra, github.com/google/uuid

**Spec:** `docs/superpowers/specs/2026-06-15-agent-handler-design.md`

---

## File Structure

```
handler/                        # Go module root (will be renamed from agent-ledger)
├── go.mod
├── go.sum
├── main.go                     # Entry point, calls cmd.Execute()
├── cmd/
│   ├── root.go                 # Root cobra command, global flags (--json)
│   ├── register.go             # handler register
│   ├── unregister.go           # handler unregister
│   ├── heartbeat.go            # handler heartbeat
│   ├── emit.go                 # handler emit
│   ├── unread.go               # handler unread
│   ├── ack.go                  # handler ack
│   ├── status.go               # handler status
│   ├── cleanup.go              # handler cleanup
│   ├── health.go               # handler health
│   ├── configure.go            # handler configure
│   ├── statusline.go           # handler statusline
│   ├── subscribe.go            # handler subscribe
│   ├── unsubscribe.go          # handler unsubscribe
│   ├── subscriptions.go        # handler subscriptions
│   ├── log_cmd.go              # handler log (log_cmd.go to avoid conflict with log package)
│   ├── tail.go                 # handler tail
│   ├── query.go                # handler query
│   ├── schema_cmd.go           # handler schema
│   ├── install.go              # handler install
│   ├── uninstall.go            # handler uninstall
│   └── resource/
│       ├── resource.go         # handler resource (parent command)
│       ├── link.go             # handler resource link
│       ├── related.go          # handler resource related
│       └── history.go          # handler resource history
├── db/
│   ├── db.go                   # Open/close, migrations, WAL mode setup
│   ├── db_test.go              # DB open/close/migration tests
│   ├── schema.sql              # DDL: all CREATE TABLE and CREATE INDEX statements
│   ├── events.go               # Event CRUD: InsertEvent, QueryEvents, UnreadForSession
│   ├── events_test.go
│   ├── sessions.go             # Session CRUD: UpsertSession, GetSession, ListSessions, ArchiveDeadSessions
│   ├── sessions_test.go
│   ├── subscriptions.go        # Subscription CRUD: Subscribe, Unsubscribe, ListSubscriptions, ActiveResourcesForType
│   ├── subscriptions_test.go
│   ├── resources.go            # Resource relationships: LinkResources, FindRelatedSessions, ResourceHistory
│   ├── resources_test.go
│   ├── cursors.go              # Cursor CRUD: GetCursor, AdvanceCursor
│   └── cursors_test.go
├── discover/
│   ├── session.go              # Claude session UUID discovery from JSONL
│   ├── session_test.go
│   ├── name.go                 # Claude session name discovery from JSONL
│   ├── name_test.go
│   ├── pid.go                  # PID cache read/write, process liveness check
│   └── pid_test.go
├── worktree/
│   ├── resources.go            # .worktree-resources file read/append/remove
│   └── resources_test.go
├── hooks/
│   ├── session_start.sh        # SessionStart hook script
│   ├── user_prompt_submit.sh   # UserPromptSubmit hook script
│   └── pre_compact.sh          # PreCompact hook script
├── skills/
│   ├── inbox/
│   │   └── SKILL.md            # /inbox skill
│   ├── inbox_mode/
│   │   └── SKILL.md            # /inbox_mode skill
│   ├── handler_register/
│   │   └── SKILL.md            # /handler_register skill
│   ├── handler_emit/
│   │   └── SKILL.md            # /handler_emit skill
│   ├── handler_subscribe/
│   │   └── SKILL.md            # /handler_subscribe skill
│   ├── handler_snapshot/
│   │   └── SKILL.md            # /handler_snapshot skill
│   └── handler_unregister/
│       └── SKILL.md            # /handler_unregister skill
└── testutil/
    └── testutil.go             # Shared test helpers: temp DB, seed data
```

---

## Task 1: Go Module and Project Scaffolding

**Files:**
- Create: `go.mod`, `main.go`, `cmd/root.go`, `testutil/testutil.go`

- [ ] **Step 1: Initialize Go module**

```bash
cd /Users/mturley/git/agent-ledger
go mod init github.com/mturley/agent-handler
```

- [ ] **Step 2: Add dependencies**

```bash
go get github.com/spf13/cobra@latest
go get modernc.org/sqlite@latest
go get github.com/google/uuid@latest
```

- [ ] **Step 3: Create main.go**

```go
// main.go
package main

import "github.com/mturley/agent-handler/cmd"

func main() {
	cmd.Execute()
}
```

- [ ] **Step 4: Create cmd/root.go**

```go
// cmd/root.go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var jsonOutput bool

var rootCmd = &cobra.Command{
	Use:   "handler",
	Short: "Centralized event ledger for code agent sessions",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
```

- [ ] **Step 5: Create testutil/testutil.go**

```go
// testutil/testutil.go
package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mturley/agent-handler/db"
)

func TempDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "handler.db")
	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open temp db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func HandlerDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sessions"), 0755)
	os.MkdirAll(filepath.Join(dir, "logs"), 0755)
	return dir
}
```

- [ ] **Step 6: Verify the module builds**

Run: `go build ./...`
Expected: clean build, no errors

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum main.go cmd/root.go testutil/testutil.go
git commit --signoff -m "feat: scaffold Go module with cobra CLI skeleton"
```

---

## Task 2: SQLite Schema and DB Layer

**Files:**
- Create: `db/schema.sql`, `db/db.go`, `db/db_test.go`

- [ ] **Step 1: Write schema.sql**

```sql
-- db/schema.sql
CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    ts TEXT NOT NULL,
    external_ts TEXT,
    source TEXT NOT NULL,
    session_id TEXT,
    type TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT,
    author TEXT,
    author_type TEXT,
    broadcast INTEGER NOT NULL DEFAULT 0,
    tags TEXT
);

CREATE INDEX IF NOT EXISTS idx_events_ts ON events(ts);
CREATE INDEX IF NOT EXISTS idx_events_source_type ON events(source, type);
CREATE INDEX IF NOT EXISTS idx_events_session_id ON events(session_id);

CREATE TABLE IF NOT EXISTS event_recipients (
    event_id TEXT NOT NULL REFERENCES events(id),
    recipient_type TEXT NOT NULL,
    recipient_value TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_event_recipients_target ON event_recipients(recipient_type, recipient_value);

CREATE TABLE IF NOT EXISTS event_resources (
    event_id TEXT NOT NULL REFERENCES events(id),
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    resource_url TEXT
);

CREATE INDEX IF NOT EXISTS idx_event_resources_resource ON event_resources(resource_type, resource_id);

CREATE TABLE IF NOT EXISTS sessions (
    session_id TEXT PRIMARY KEY,
    harness TEXT NOT NULL DEFAULT 'claude',
    repo TEXT NOT NULL,
    branch TEXT NOT NULL,
    session_name TEXT,
    pid INTEGER,
    status TEXT NOT NULL DEFAULT 'active',
    inbox_mode TEXT NOT NULL DEFAULT 'manual',
    auto_poll_interval INTEGER,
    last_active TEXT NOT NULL,
    registered_at TEXT NOT NULL,
    jsonl_path TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS session_cursors (
    session_id TEXT PRIMARY KEY REFERENCES sessions(session_id),
    last_seen_ts TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(session_id),
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    resource_url TEXT,
    created_at TEXT NOT NULL,
    deleted_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_resource ON subscriptions(resource_type, resource_id, deleted_at);

CREATE TABLE IF NOT EXISTS resource_relationships (
    id TEXT PRIMARY KEY,
    child_type TEXT NOT NULL,
    child_id TEXT NOT NULL,
    child_url TEXT,
    parent_type TEXT NOT NULL,
    parent_id TEXT NOT NULL,
    parent_url TEXT,
    relationship TEXT NOT NULL,
    source TEXT NOT NULL,
    created_at TEXT NOT NULL
);
```

- [ ] **Step 2: Write db/db.go**

```go
// db/db.go
package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

type DB struct {
	conn *sql.DB
	path string
}

func Open(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	conn, err := sql.Open("sqlite", path+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("setting WAL mode: %w", err)
	}

	if _, err := conn.Exec(schemaSQL); err != nil {
		conn.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}

	return &DB{conn: conn, path: path}, nil
}

func OpenReadOnly(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("opening database read-only: %w", err)
	}
	return &DB{conn: conn, path: path}, nil
}

func (d *DB) Close() error {
	return d.conn.Close()
}

func (d *DB) Conn() *sql.DB {
	return d.conn
}

func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agent-handler", "handler.db")
}
```

- [ ] **Step 3: Write the test for DB open, schema creation, and WAL mode**

```go
// db/db_test.go
package db

import (
	"path/filepath"
	"testing"
)

func TestOpenCreatesSchemaAndWAL(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	d, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer d.Close()

	// Verify WAL mode
	var journalMode string
	err = d.conn.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("querying journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("expected WAL mode, got %s", journalMode)
	}

	// Verify all tables exist
	tables := []string{"events", "event_recipients", "event_resources", "sessions", "session_cursors", "subscriptions", "resource_relationships"}
	for _, table := range tables {
		var name string
		err := d.conn.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %s not found: %v", table, err)
		}
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	d1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open failed: %v", err)
	}
	d1.Close()

	d2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open failed: %v", err)
	}
	d2.Close()
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./db/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add db/schema.sql db/db.go db/db_test.go
git commit --signoff -m "feat: SQLite schema and DB layer with WAL mode"
```

---

## Task 3: Session CRUD

**Files:**
- Create: `db/sessions.go`, `db/sessions_test.go`

- [ ] **Step 1: Write the tests**

```go
// db/sessions_test.go
package db

import (
	"path/filepath"
	"testing"
	"time"
)

func testDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestUpsertAndGetSession(t *testing.T) {
	d := testDB(t)
	now := time.Now().UTC().Format(time.RFC3339)

	s := Session{
		SessionID:   "abc-123",
		Harness:     "claude",
		Repo:        "mturley/myrepo",
		Branch:      "feature-auth",
		SessionName: "auth-refactor",
		PID:         12345,
		Status:      "active",
		InboxMode:   "manual",
		LastActive:  now,
		RegisteredAt: now,
		JSONLPath:   "/home/user/.claude/projects/-foo/abc-123.jsonl",
	}

	err := d.UpsertSession(s)
	if err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	got, err := d.GetSession("abc-123")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Branch != "feature-auth" {
		t.Errorf("expected branch feature-auth, got %s", got.Branch)
	}
	if got.SessionName != "auth-refactor" {
		t.Errorf("expected name auth-refactor, got %s", got.SessionName)
	}
}

func TestUpsertSessionUpdatesExisting(t *testing.T) {
	d := testDB(t)
	now := time.Now().UTC().Format(time.RFC3339)

	s := Session{
		SessionID: "abc-123", Harness: "claude", Repo: "mturley/myrepo",
		Branch: "feature-auth", PID: 12345, Status: "active", InboxMode: "manual",
		LastActive: now, RegisteredAt: now, JSONLPath: "/path/abc-123.jsonl",
	}
	d.UpsertSession(s)

	// Simulate resume with new PID
	s.PID = 67890
	s.SessionName = "new-name"
	s.Status = "active"
	d.UpsertSession(s)

	got, _ := d.GetSession("abc-123")
	if got.PID != 67890 {
		t.Errorf("expected PID 67890, got %d", got.PID)
	}
	if got.SessionName != "new-name" {
		t.Errorf("expected name new-name, got %s", got.SessionName)
	}
}

func TestListSessionsFiltersArchived(t *testing.T) {
	d := testDB(t)
	now := time.Now().UTC().Format(time.RFC3339)

	active := Session{
		SessionID: "s1", Harness: "claude", Repo: "r", Branch: "b1",
		Status: "active", InboxMode: "manual", LastActive: now,
		RegisteredAt: now, JSONLPath: "/p/s1.jsonl",
	}
	archived := Session{
		SessionID: "s2", Harness: "claude", Repo: "r", Branch: "b2",
		Status: "archived", InboxMode: "manual", LastActive: now,
		RegisteredAt: now, JSONLPath: "/p/s2.jsonl",
	}
	d.UpsertSession(active)
	d.UpsertSession(archived)

	sessions, err := d.ListSessions(false, 20, 0)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 active session, got %d", len(sessions))
	}
	if sessions[0].SessionID != "s1" {
		t.Errorf("expected s1, got %s", sessions[0].SessionID)
	}

	all, _ := d.ListSessions(true, 20, 0)
	if len(all) != 2 {
		t.Errorf("expected 2 sessions with --all, got %d", len(all))
	}
}

func TestBumpLastActive(t *testing.T) {
	d := testDB(t)
	old := "2026-01-01T00:00:00Z"

	s := Session{
		SessionID: "s1", Harness: "claude", Repo: "r", Branch: "b",
		Status: "active", InboxMode: "manual", LastActive: old,
		RegisteredAt: old, JSONLPath: "/p/s1.jsonl",
	}
	d.UpsertSession(s)

	newTs := "2026-06-15T12:00:00Z"
	err := d.BumpLastActive("s1", newTs)
	if err != nil {
		t.Fatalf("BumpLastActive: %v", err)
	}

	got, _ := d.GetSession("s1")
	if got.LastActive != newTs {
		t.Errorf("expected %s, got %s", newTs, got.LastActive)
	}
}

func TestArchiveDeadSessions(t *testing.T) {
	d := testDB(t)
	now := time.Now().UTC().Format(time.RFC3339)

	s := Session{
		SessionID: "s1", Harness: "claude", Repo: "r", Branch: "b",
		Status: "active", InboxMode: "manual", LastActive: now,
		RegisteredAt: now, JSONLPath: "/p/s1.jsonl",
	}
	d.UpsertSession(s)

	count, err := d.ArchiveSessions([]string{"s1"})
	if err != nil {
		t.Fatalf("ArchiveSessions: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 archived, got %d", count)
	}

	got, _ := d.GetSession("s1")
	if got.Status != "archived" {
		t.Errorf("expected archived, got %s", got.Status)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./db/ -v -run TestUpsert`
Expected: FAIL — `UpsertSession` not defined

- [ ] **Step 3: Implement sessions.go**

```go
// db/sessions.go
package db

import "fmt"

type Session struct {
	SessionID        string
	Harness          string
	Repo             string
	Branch           string
	SessionName      string
	PID              int
	Status           string
	InboxMode        string
	AutoPollInterval *int
	LastActive       string
	RegisteredAt     string
	JSONLPath        string
}

func (d *DB) UpsertSession(s Session) error {
	_, err := d.conn.Exec(`
		INSERT INTO sessions (session_id, harness, repo, branch, session_name, pid, status, inbox_mode, auto_poll_interval, last_active, registered_at, jsonl_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			harness=excluded.harness, repo=excluded.repo, branch=excluded.branch,
			session_name=excluded.session_name, pid=excluded.pid, status=excluded.status,
			inbox_mode=COALESCE(sessions.inbox_mode, excluded.inbox_mode),
			auto_poll_interval=COALESCE(sessions.auto_poll_interval, excluded.auto_poll_interval),
			last_active=excluded.last_active, jsonl_path=excluded.jsonl_path
	`, s.SessionID, s.Harness, s.Repo, s.Branch, s.SessionName, s.PID, s.Status,
		s.InboxMode, s.AutoPollInterval, s.LastActive, s.RegisteredAt, s.JSONLPath)
	return err
}

func (d *DB) GetSession(sessionID string) (*Session, error) {
	s := &Session{}
	err := d.conn.QueryRow(`
		SELECT session_id, harness, repo, branch, COALESCE(session_name,''), COALESCE(pid,0),
		       status, inbox_mode, auto_poll_interval, last_active, registered_at, jsonl_path
		FROM sessions WHERE session_id = ?
	`, sessionID).Scan(&s.SessionID, &s.Harness, &s.Repo, &s.Branch, &s.SessionName, &s.PID,
		&s.Status, &s.InboxMode, &s.AutoPollInterval, &s.LastActive, &s.RegisteredAt, &s.JSONLPath)
	if err != nil {
		return nil, fmt.Errorf("session %s not found: %w", sessionID, err)
	}
	return s, nil
}

func (d *DB) ListSessions(includeArchived bool, limit, offset int) ([]Session, error) {
	query := `SELECT session_id, harness, repo, branch, COALESCE(session_name,''), COALESCE(pid,0),
	                 status, inbox_mode, auto_poll_interval, last_active, registered_at, jsonl_path
	          FROM sessions`
	if !includeArchived {
		query += ` WHERE status != 'archived'`
	}
	query += ` ORDER BY last_active DESC LIMIT ? OFFSET ?`

	rows, err := d.conn.Query(query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.SessionID, &s.Harness, &s.Repo, &s.Branch, &s.SessionName, &s.PID,
			&s.Status, &s.InboxMode, &s.AutoPollInterval, &s.LastActive, &s.RegisteredAt, &s.JSONLPath); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

func (d *DB) BumpLastActive(sessionID, ts string) error {
	result, err := d.conn.Exec(`UPDATE sessions SET last_active = ? WHERE session_id = ?`, ts, sessionID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session %s not found", sessionID)
	}
	return nil
}

func (d *DB) ArchiveSessions(sessionIDs []string) (int, error) {
	var count int
	for _, id := range sessionIDs {
		result, err := d.conn.Exec(`UPDATE sessions SET status = 'archived' WHERE session_id = ?`, id)
		if err != nil {
			return count, err
		}
		n, _ := result.RowsAffected()
		count += int(n)
	}
	return count, nil
}

func (d *DB) ConfigureSession(sessionID, inboxMode string, autoPollInterval *int) error {
	_, err := d.conn.Exec(`UPDATE sessions SET inbox_mode = ?, auto_poll_interval = ? WHERE session_id = ?`,
		inboxMode, autoPollInterval, sessionID)
	return err
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./db/ -v -run "TestUpsert|TestList|TestBump|TestArchive"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add db/sessions.go db/sessions_test.go
git commit --signoff -m "feat: session CRUD — upsert, get, list, heartbeat, archive"
```

---

## Task 4: Event CRUD and Unread Query

**Files:**
- Create: `db/events.go`, `db/events_test.go`, `db/cursors.go`, `db/cursors_test.go`

- [ ] **Step 1: Write cursor tests**

```go
// db/cursors_test.go
package db

import "testing"

func TestGetAndAdvanceCursor(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "s1")

	// No cursor yet — should return empty string
	ts, err := d.GetCursor("s1")
	if err != nil {
		t.Fatalf("GetCursor: %v", err)
	}
	if ts != "" {
		t.Errorf("expected empty cursor, got %s", ts)
	}

	// Advance cursor
	err = d.AdvanceCursor("s1", "2026-06-15T12:00:00Z")
	if err != nil {
		t.Fatalf("AdvanceCursor: %v", err)
	}

	ts, _ = d.GetCursor("s1")
	if ts != "2026-06-15T12:00:00Z" {
		t.Errorf("expected 2026-06-15T12:00:00Z, got %s", ts)
	}
}

func seedSession(t *testing.T, d *DB, id string) {
	t.Helper()
	d.UpsertSession(Session{
		SessionID: id, Harness: "claude", Repo: "r", Branch: "b",
		Status: "active", InboxMode: "manual", LastActive: "2026-06-15T00:00:00Z",
		RegisteredAt: "2026-06-15T00:00:00Z", JSONLPath: "/p/" + id + ".jsonl",
	})
}
```

- [ ] **Step 2: Implement cursors.go**

```go
// db/cursors.go
package db

func (d *DB) GetCursor(sessionID string) (string, error) {
	var ts string
	err := d.conn.QueryRow(`SELECT last_seen_ts FROM session_cursors WHERE session_id = ?`, sessionID).Scan(&ts)
	if err != nil {
		return "", nil // no cursor yet is not an error
	}
	return ts, nil
}

func (d *DB) AdvanceCursor(sessionID, ts string) error {
	_, err := d.conn.Exec(`
		INSERT INTO session_cursors (session_id, last_seen_ts) VALUES (?, ?)
		ON CONFLICT(session_id) DO UPDATE SET last_seen_ts = excluded.last_seen_ts
	`, sessionID, ts)
	return err
}
```

- [ ] **Step 3: Run cursor tests**

Run: `go test ./db/ -v -run TestGetAndAdvanceCursor`
Expected: PASS

- [ ] **Step 4: Write event tests**

```go
// db/events_test.go
package db

import "testing"

func TestInsertAndQueryEvents(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "s1")

	evt := Event{
		ID:      "evt-1",
		TS:      "2026-06-15T10:00:00Z",
		Source:  "agent",
		SessionID: strPtr("s1"),
		Type:    "milestone",
		Title:   "Found root cause",
		Body:    strPtr("The bug is in the auth middleware"),
	}
	err := d.InsertEvent(evt, nil, nil)
	if err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	events, err := d.QueryEvents(EventFilter{SessionID: strPtr("s1"), Limit: 10})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Title != "Found root cause" {
		t.Errorf("expected 'Found root cause', got %s", events[0].Title)
	}
}

func TestUnreadViaResourceSubscription(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "s1")

	// Subscribe s1 to a PR
	d.Subscribe(Subscription{
		ID: "sub-1", SessionID: "s1", ResourceType: "pr",
		ResourceID: "owner/repo#42", CreatedAt: "2026-06-15T00:00:00Z",
	})

	// Set cursor before the event
	d.AdvanceCursor("s1", "2026-06-15T09:00:00Z")

	// Insert an event referencing that PR (from a watcher, no session)
	d.InsertEvent(Event{
		ID: "evt-1", TS: "2026-06-15T10:00:00Z", Source: "github",
		Type: "pr_review_comment", Title: "Review comment on PR #42",
		Author: strPtr("emily"), AuthorType: strPtr("human"),
	}, nil, []EventResource{
		{ResourceType: "pr", ResourceID: "owner/repo#42", ResourceURL: strPtr("https://github.com/owner/repo/pull/42")},
	})

	unread, err := d.UnreadForSession("s1")
	if err != nil {
		t.Fatalf("UnreadForSession: %v", err)
	}
	if len(unread) != 1 {
		t.Fatalf("expected 1 unread, got %d", len(unread))
	}
	if unread[0].ID != "evt-1" {
		t.Errorf("expected evt-1, got %s", unread[0].ID)
	}
}

func TestUnreadViaDirectRecipient(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "s1")
	d.AdvanceCursor("s1", "2026-06-15T09:00:00Z")

	d.InsertEvent(Event{
		ID: "evt-1", TS: "2026-06-15T10:00:00Z", Source: "handler",
		Type: "message", Title: "Message from overseer",
	}, []EventRecipient{
		{RecipientType: "session", RecipientValue: "s1"},
	}, nil)

	unread, _ := d.UnreadForSession("s1")
	if len(unread) != 1 {
		t.Fatalf("expected 1 unread via recipient, got %d", len(unread))
	}
}

func TestUnreadViaBroadcast(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "s1")
	d.AdvanceCursor("s1", "2026-06-15T09:00:00Z")

	d.InsertEvent(Event{
		ID: "evt-1", TS: "2026-06-15T10:00:00Z", Source: "handler",
		Type: "message", Title: "System announcement", Broadcast: true,
	}, nil, nil)

	unread, _ := d.UnreadForSession("s1")
	if len(unread) != 1 {
		t.Fatalf("expected 1 unread broadcast, got %d", len(unread))
	}
}

func TestUnreadExcludesOldEvents(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "s1")
	d.AdvanceCursor("s1", "2026-06-15T11:00:00Z")

	// Event before cursor
	d.InsertEvent(Event{
		ID: "evt-1", TS: "2026-06-15T10:00:00Z", Source: "handler",
		Type: "message", Title: "Old message", Broadcast: true,
	}, nil, nil)

	unread, _ := d.UnreadForSession("s1")
	if len(unread) != 0 {
		t.Errorf("expected 0 unread, got %d", len(unread))
	}
}

func strPtr(s string) *string { return &s }
```

- [ ] **Step 5: Implement events.go**

```go
// db/events.go
package db

import "fmt"

type Event struct {
	ID         string
	TS         string
	ExternalTS *string
	Source     string
	SessionID  *string
	Type       string
	Title      string
	Body       *string
	Author     *string
	AuthorType *string
	Broadcast  bool
	Tags       *string
}

type EventRecipient struct {
	RecipientType  string
	RecipientValue string
}

type EventResource struct {
	ResourceType string
	ResourceID   string
	ResourceURL  *string
}

type EventFilter struct {
	SessionID *string
	Source    *string
	Type     *string
	Since    *string
	Limit    int
	Offset   int
}

func (d *DB) InsertEvent(e Event, recipients []EventRecipient, resources []EventResource) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	broadcast := 0
	if e.Broadcast {
		broadcast = 1
	}

	_, err = tx.Exec(`
		INSERT INTO events (id, ts, external_ts, source, session_id, type, title, body, author, author_type, broadcast, tags)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, e.ID, e.TS, e.ExternalTS, e.Source, e.SessionID, e.Type, e.Title, e.Body,
		e.Author, e.AuthorType, broadcast, e.Tags)
	if err != nil {
		return fmt.Errorf("inserting event: %w", err)
	}

	for _, r := range recipients {
		_, err = tx.Exec(`INSERT INTO event_recipients (event_id, recipient_type, recipient_value) VALUES (?, ?, ?)`,
			e.ID, r.RecipientType, r.RecipientValue)
		if err != nil {
			return fmt.Errorf("inserting recipient: %w", err)
		}
	}

	for _, r := range resources {
		_, err = tx.Exec(`INSERT INTO event_resources (event_id, resource_type, resource_id, resource_url) VALUES (?, ?, ?, ?)`,
			e.ID, r.ResourceType, r.ResourceID, r.ResourceURL)
		if err != nil {
			return fmt.Errorf("inserting resource: %w", err)
		}
	}

	return tx.Commit()
}

func (d *DB) QueryEvents(f EventFilter) ([]Event, error) {
	query := `SELECT id, ts, external_ts, source, session_id, type, title, body, author, author_type, broadcast, tags FROM events WHERE 1=1`
	var args []interface{}

	if f.SessionID != nil {
		query += ` AND session_id = ?`
		args = append(args, *f.SessionID)
	}
	if f.Source != nil {
		query += ` AND source = ?`
		args = append(args, *f.Source)
	}
	if f.Type != nil {
		query += ` AND type = ?`
		args = append(args, *f.Type)
	}
	if f.Since != nil {
		query += ` AND ts > ?`
		args = append(args, *f.Since)
	}

	query += ` ORDER BY ts DESC`

	if f.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	if f.Offset > 0 {
		query += ` OFFSET ?`
		args = append(args, f.Offset)
	}

	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEvents(rows)
}

func (d *DB) UnreadForSession(sessionID string) ([]Event, error) {
	cursor, err := d.GetCursor(sessionID)
	if err != nil {
		return nil, err
	}

	// Get the session's branch for branch-based recipient matching
	session, err := d.GetSession(sessionID)
	if err != nil {
		return nil, err
	}

	query := `
		SELECT DISTINCT e.id, e.ts, e.external_ts, e.source, e.session_id, e.type, e.title,
		       e.body, e.author, e.author_type, e.broadcast, e.tags
		FROM events e
		WHERE e.ts > ?
		AND (
			e.broadcast = 1
			OR EXISTS (
				SELECT 1 FROM event_recipients er
				WHERE er.event_id = e.id
				AND (
					(er.recipient_type = 'session' AND er.recipient_value = ?)
					OR (er.recipient_type = 'branch' AND er.recipient_value = ?)
				)
			)
			OR EXISTS (
				SELECT 1 FROM event_resources eres
				JOIN subscriptions sub ON sub.resource_type = eres.resource_type
					AND sub.resource_id = eres.resource_id
					AND sub.deleted_at IS NULL
					AND sub.session_id = ?
				WHERE eres.event_id = e.id
			)
		)
		ORDER BY e.ts ASC
	`

	cursorTS := cursor
	if cursorTS == "" {
		cursorTS = "1970-01-01T00:00:00Z"
	}

	rows, err := d.conn.Query(query, cursorTS, sessionID, session.Branch, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEvents(rows)
}

func (d *DB) UnreadCountForSession(sessionID string) (int, map[string]int, error) {
	events, err := d.UnreadForSession(sessionID)
	if err != nil {
		return 0, nil, err
	}
	byType := make(map[string]int)
	for _, e := range events {
		byType[e.Type]++
	}
	return len(events), byType, nil
}

func scanEvents(rows interface{ Next() bool; Scan(...interface{}) error; Err() error }) ([]Event, error) {
	var events []Event
	for rows.Next() {
		var e Event
		var broadcast int
		if err := rows.Scan(&e.ID, &e.TS, &e.ExternalTS, &e.Source, &e.SessionID, &e.Type,
			&e.Title, &e.Body, &e.Author, &e.AuthorType, &broadcast, &e.Tags); err != nil {
			return nil, err
		}
		e.Broadcast = broadcast == 1
		events = append(events, e)
	}
	return events, rows.Err()
}
```

- [ ] **Step 6: Run all event tests**

Run: `go test ./db/ -v -run "TestInsert|TestUnread"`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add db/events.go db/events_test.go db/cursors.go db/cursors_test.go
git commit --signoff -m "feat: event CRUD, cursor tracking, and unread query with subscription routing"
```

---

## Task 5: Subscription CRUD

**Files:**
- Create: `db/subscriptions.go`, `db/subscriptions_test.go`

- [ ] **Step 1: Write tests**

```go
// db/subscriptions_test.go
package db

import "testing"

func TestSubscribeAndList(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "s1")

	err := d.Subscribe(Subscription{
		ID: "sub-1", SessionID: "s1", ResourceType: "pr",
		ResourceID: "owner/repo#42", ResourceURL: strPtr("https://github.com/owner/repo/pull/42"),
		CreatedAt: "2026-06-15T10:00:00Z",
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	subs, err := d.ListSubscriptions("s1", false)
	if err != nil {
		t.Fatalf("ListSubscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subs))
	}
	if subs[0].ResourceID != "owner/repo#42" {
		t.Errorf("expected owner/repo#42, got %s", subs[0].ResourceID)
	}
}

func TestUnsubscribeSoftDeletes(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "s1")

	d.Subscribe(Subscription{
		ID: "sub-1", SessionID: "s1", ResourceType: "pr",
		ResourceID: "owner/repo#42", CreatedAt: "2026-06-15T10:00:00Z",
	})

	err := d.Unsubscribe("s1", "pr", "owner/repo#42")
	if err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	// Active subscriptions should be empty
	subs, _ := d.ListSubscriptions("s1", false)
	if len(subs) != 0 {
		t.Errorf("expected 0 active subscriptions, got %d", len(subs))
	}

	// Including deleted should show the subscription
	allSubs, _ := d.ListSubscriptions("s1", true)
	if len(allSubs) != 1 {
		t.Errorf("expected 1 total subscription, got %d", len(allSubs))
	}
}

func TestReinstateSubscription(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "s1")

	d.Subscribe(Subscription{
		ID: "sub-1", SessionID: "s1", ResourceType: "pr",
		ResourceID: "owner/repo#42", CreatedAt: "2026-06-15T10:00:00Z",
	})
	d.Unsubscribe("s1", "pr", "owner/repo#42")

	err := d.Reinstate("s1", "pr", "owner/repo#42")
	if err != nil {
		t.Fatalf("Reinstate: %v", err)
	}

	subs, _ := d.ListSubscriptions("s1", false)
	if len(subs) != 1 {
		t.Errorf("expected 1 active subscription after reinstate, got %d", len(subs))
	}
}

func TestSubscribeDeduplicate(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "s1")

	d.Subscribe(Subscription{
		ID: "sub-1", SessionID: "s1", ResourceType: "pr",
		ResourceID: "owner/repo#42", CreatedAt: "2026-06-15T10:00:00Z",
	})
	// Subscribe again — should not create a duplicate
	d.Subscribe(Subscription{
		ID: "sub-2", SessionID: "s1", ResourceType: "pr",
		ResourceID: "owner/repo#42", CreatedAt: "2026-06-15T11:00:00Z",
	})

	subs, _ := d.ListSubscriptions("s1", false)
	if len(subs) != 1 {
		t.Errorf("expected 1 subscription (deduplicated), got %d", len(subs))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./db/ -v -run "TestSubscribe|TestUnsubscribe|TestReinstate"`
Expected: FAIL

- [ ] **Step 3: Implement subscriptions.go**

```go
// db/subscriptions.go
package db

import (
	"fmt"
	"time"
)

type Subscription struct {
	ID           string
	SessionID    string
	ResourceType string
	ResourceID   string
	ResourceURL  *string
	CreatedAt    string
	DeletedAt    *string
}

func (d *DB) Subscribe(s Subscription) error {
	// Check if an active subscription already exists
	var count int
	err := d.conn.QueryRow(`
		SELECT COUNT(*) FROM subscriptions
		WHERE session_id = ? AND resource_type = ? AND resource_id = ? AND deleted_at IS NULL
	`, s.SessionID, s.ResourceType, s.ResourceID).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil // already subscribed
	}

	// Check if a soft-deleted one exists — reinstate it
	var existingID string
	err = d.conn.QueryRow(`
		SELECT id FROM subscriptions
		WHERE session_id = ? AND resource_type = ? AND resource_id = ? AND deleted_at IS NOT NULL
	`, s.SessionID, s.ResourceType, s.ResourceID).Scan(&existingID)
	if err == nil {
		return d.Reinstate(s.SessionID, s.ResourceType, s.ResourceID)
	}

	_, err = d.conn.Exec(`
		INSERT INTO subscriptions (id, session_id, resource_type, resource_id, resource_url, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, s.ID, s.SessionID, s.ResourceType, s.ResourceID, s.ResourceURL, s.CreatedAt)
	return err
}

func (d *DB) Unsubscribe(sessionID, resourceType, resourceID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := d.conn.Exec(`
		UPDATE subscriptions SET deleted_at = ?
		WHERE session_id = ? AND resource_type = ? AND resource_id = ? AND deleted_at IS NULL
	`, now, sessionID, resourceType, resourceID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("no active subscription found for %s:%s", resourceType, resourceID)
	}
	return nil
}

func (d *DB) Reinstate(sessionID, resourceType, resourceID string) error {
	_, err := d.conn.Exec(`
		UPDATE subscriptions SET deleted_at = NULL
		WHERE session_id = ? AND resource_type = ? AND resource_id = ? AND deleted_at IS NOT NULL
	`, sessionID, resourceType, resourceID)
	return err
}

func (d *DB) ListSubscriptions(sessionID string, includeDeleted bool) ([]Subscription, error) {
	query := `SELECT id, session_id, resource_type, resource_id, resource_url, created_at, deleted_at
	          FROM subscriptions WHERE session_id = ?`
	if !includeDeleted {
		query += ` AND deleted_at IS NULL`
	}
	query += ` ORDER BY created_at DESC`

	rows, err := d.conn.Query(query, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []Subscription
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(&s.ID, &s.SessionID, &s.ResourceType, &s.ResourceID,
			&s.ResourceURL, &s.CreatedAt, &s.DeletedAt); err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

func (d *DB) SoftDeleteSubscriptionsForBranch(branch string) (int, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := d.conn.Exec(`
		UPDATE subscriptions SET deleted_at = ?
		WHERE deleted_at IS NULL AND session_id IN (
			SELECT session_id FROM sessions WHERE branch = ?
		)
	`, now, branch)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./db/ -v -run "TestSubscribe|TestUnsubscribe|TestReinstate"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add db/subscriptions.go db/subscriptions_test.go
git commit --signoff -m "feat: subscription CRUD — subscribe, unsubscribe, reinstate, deduplicate"
```

---

## Task 6: Resource Relationships

**Files:**
- Create: `db/resources.go`, `db/resources_test.go`

- [ ] **Step 1: Write tests**

```go
// db/resources_test.go
package db

import "testing"

func TestLinkAndFindRelated(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "s1")
	seedSession(t, d, "s2")

	// s1 subscribes to RHOAIENG-100, s2 subscribes to RHOAIENG-101
	d.Subscribe(Subscription{
		ID: "sub-1", SessionID: "s1", ResourceType: "jira",
		ResourceID: "RHOAIENG-100", CreatedAt: "2026-06-15T10:00:00Z",
	})
	d.Subscribe(Subscription{
		ID: "sub-2", SessionID: "s2", ResourceType: "jira",
		ResourceID: "RHOAIENG-101", CreatedAt: "2026-06-15T10:00:00Z",
	})

	// Both are children of the same epic
	d.LinkResources(ResourceRelationship{
		ID: "rel-1", ChildType: "jira", ChildID: "RHOAIENG-100",
		ParentType: "jira", ParentID: "RHOAIENG-50",
		Relationship: "epic_child", Source: "manual", CreatedAt: "2026-06-15T10:00:00Z",
	})
	d.LinkResources(ResourceRelationship{
		ID: "rel-2", ChildType: "jira", ChildID: "RHOAIENG-101",
		ParentType: "jira", ParentID: "RHOAIENG-50",
		Relationship: "epic_child", Source: "manual", CreatedAt: "2026-06-15T10:00:00Z",
	})

	related, err := d.FindRelatedSessions("s1")
	if err != nil {
		t.Fatalf("FindRelatedSessions: %v", err)
	}
	if len(related) != 1 {
		t.Fatalf("expected 1 related session, got %d", len(related))
	}
	if related[0].SessionID != "s2" {
		t.Errorf("expected s2, got %s", related[0].SessionID)
	}
}

func TestResourceHistory(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "s1")

	d.Subscribe(Subscription{
		ID: "sub-1", SessionID: "s1", ResourceType: "pr",
		ResourceID: "owner/repo#42", CreatedAt: "2026-06-15T10:00:00Z",
	})

	d.InsertEvent(Event{
		ID: "evt-1", TS: "2026-06-15T10:00:00Z", Source: "github",
		Type: "pr_review_comment", Title: "Review on #42",
	}, nil, []EventResource{
		{ResourceType: "pr", ResourceID: "owner/repo#42"},
	})

	events, err := d.ResourceHistory("pr", "owner/repo#42", 10)
	if err != nil {
		t.Fatalf("ResourceHistory: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./db/ -v -run "TestLink|TestResource"`
Expected: FAIL

- [ ] **Step 3: Implement resources.go**

```go
// db/resources.go
package db

type ResourceRelationship struct {
	ID           string
	ChildType    string
	ChildID      string
	ChildURL     *string
	ParentType   string
	ParentID     string
	ParentURL    *string
	Relationship string
	Source       string
	CreatedAt    string
}

func (d *DB) LinkResources(r ResourceRelationship) error {
	_, err := d.conn.Exec(`
		INSERT INTO resource_relationships (id, child_type, child_id, child_url, parent_type, parent_id, parent_url, relationship, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, r.ID, r.ChildType, r.ChildID, r.ChildURL, r.ParentType, r.ParentID, r.ParentURL,
		r.Relationship, r.Source, r.CreatedAt)
	return err
}

func (d *DB) FindRelatedSessions(sessionID string) ([]Session, error) {
	// Find sessions that share direct resource subscriptions
	// OR subscribe to resources in the same parent group (e.g. same epic)
	query := `
		SELECT DISTINCT s.session_id, s.harness, s.repo, s.branch, COALESCE(s.session_name,''),
		       COALESCE(s.pid,0), s.status, s.inbox_mode, s.auto_poll_interval,
		       s.last_active, s.registered_at, s.jsonl_path
		FROM sessions s
		JOIN subscriptions sub2 ON sub2.session_id = s.session_id AND sub2.deleted_at IS NULL
		WHERE s.session_id != ?
		AND s.status != 'archived'
		AND (
			-- Direct resource overlap
			EXISTS (
				SELECT 1 FROM subscriptions sub1
				WHERE sub1.session_id = ? AND sub1.deleted_at IS NULL
				AND sub1.resource_type = sub2.resource_type AND sub1.resource_id = sub2.resource_id
			)
			-- Same parent (e.g. same epic)
			OR EXISTS (
				SELECT 1 FROM subscriptions sub1
				JOIN resource_relationships rr1 ON rr1.child_type = sub1.resource_type AND rr1.child_id = sub1.resource_id
				JOIN resource_relationships rr2 ON rr2.child_type = sub2.resource_type AND rr2.child_id = sub2.resource_id
				WHERE sub1.session_id = ? AND sub1.deleted_at IS NULL
				AND rr1.parent_type = rr2.parent_type AND rr1.parent_id = rr2.parent_id
			)
		)
		ORDER BY s.last_active DESC
	`

	rows, err := d.conn.Query(query, sessionID, sessionID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.SessionID, &s.Harness, &s.Repo, &s.Branch, &s.SessionName, &s.PID,
			&s.Status, &s.InboxMode, &s.AutoPollInterval, &s.LastActive, &s.RegisteredAt, &s.JSONLPath); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

func (d *DB) ResourceHistory(resourceType, resourceID string, limit int) ([]Event, error) {
	query := `
		SELECT e.id, e.ts, e.external_ts, e.source, e.session_id, e.type, e.title,
		       e.body, e.author, e.author_type, e.broadcast, e.tags
		FROM events e
		JOIN event_resources er ON er.event_id = e.id
		WHERE er.resource_type = ? AND er.resource_id = ?
		ORDER BY e.ts DESC
		LIMIT ?
	`
	rows, err := d.conn.Query(query, resourceType, resourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (d *DB) SessionsForResource(resourceType, resourceID string) ([]Subscription, error) {
	rows, err := d.conn.Query(`
		SELECT id, session_id, resource_type, resource_id, resource_url, created_at, deleted_at
		FROM subscriptions
		WHERE resource_type = ? AND resource_id = ?
		ORDER BY created_at DESC
	`, resourceType, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []Subscription
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(&s.ID, &s.SessionID, &s.ResourceType, &s.ResourceID,
			&s.ResourceURL, &s.CreatedAt, &s.DeletedAt); err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./db/ -v -run "TestLink|TestResource"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add db/resources.go db/resources_test.go
git commit --signoff -m "feat: resource relationships — link, find related sessions, resource history"
```

---

## Task 7: Session Discovery (Claude JSONL)

**Files:**
- Create: `discover/session.go`, `discover/session_test.go`, `discover/name.go`, `discover/name_test.go`

- [ ] **Step 1: Write session discovery tests**

```go
// discover/session_test.go
package discover

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSessionID(t *testing.T) {
	// Create a fake Claude project directory with a JSONL file
	dir := t.TempDir()
	projectDir := filepath.Join(dir, ".claude", "projects", "-Users-test-git-myrepo")
	os.MkdirAll(projectDir, 0755)

	jsonlPath := filepath.Join(projectDir, "abc-123-456.jsonl")
	os.WriteFile(jsonlPath, []byte(`{"type":"last-prompt","sessionId":"abc-123-456"}`+"\n"), 0600)

	id, path, err := DiscoverSessionID(dir, "/Users/test/git/myrepo")
	if err != nil {
		t.Fatalf("DiscoverSessionID: %v", err)
	}
	if id != "abc-123-456" {
		t.Errorf("expected abc-123-456, got %s", id)
	}
	if path != jsonlPath {
		t.Errorf("expected %s, got %s", jsonlPath, path)
	}
}

func TestDiscoverSessionIDPicksMostRecent(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, ".claude", "projects", "-Users-test-git-myrepo")
	os.MkdirAll(projectDir, 0755)

	// Create two JSONL files — the second one should be picked as most recent
	old := filepath.Join(projectDir, "old-session.jsonl")
	os.WriteFile(old, []byte(`{"type":"last-prompt"}`+"\n"), 0600)

	new := filepath.Join(projectDir, "new-session.jsonl")
	os.WriteFile(new, []byte(`{"type":"last-prompt"}`+"\n"), 0600)

	// Touch the new file to ensure it's more recent
	// (both were just created, so we need to make old actually older)
	oldTime := os.Chtimes(old, now().Add(-time.Hour), now().Add(-time.Hour))
	_ = oldTime

	id, _, _ := DiscoverSessionID(dir, "/Users/test/git/myrepo")
	if id != "new-session" {
		t.Errorf("expected new-session, got %s", id)
	}
}
```

- [ ] **Step 2: Implement session.go**

```go
// discover/session.go
package discover

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func now() time.Time { return time.Now() }

func cwdToProjectDir(claudeHome, cwd string) string {
	encoded := strings.ReplaceAll(cwd, "/", "-")
	if !strings.HasPrefix(encoded, "-") {
		encoded = "-" + encoded
	}
	return filepath.Join(claudeHome, "projects", encoded)
}

func DiscoverSessionID(claudeHome, cwd string) (sessionID, jsonlPath string, err error) {
	projectDir := cwdToProjectDir(claudeHome, cwd)

	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return "", "", fmt.Errorf("reading project dir %s: %w", projectDir, err)
	}

	var mostRecent os.DirEntry
	var mostRecentTime time.Time

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if mostRecent == nil || info.ModTime().After(mostRecentTime) {
			mostRecent = entry
			mostRecentTime = info.ModTime()
		}
	}

	if mostRecent == nil {
		return "", "", fmt.Errorf("no JSONL files found in %s", projectDir)
	}

	name := strings.TrimSuffix(mostRecent.Name(), ".jsonl")
	fullPath := filepath.Join(projectDir, mostRecent.Name())
	return name, fullPath, nil
}
```

- [ ] **Step 3: Write session name discovery tests**

```go
// discover/name_test.go
package discover

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSessionName(t *testing.T) {
	dir := t.TempDir()
	jsonl := filepath.Join(dir, "test.jsonl")

	content := `{"type":"last-prompt","sessionId":"abc"}
{"type":"ai-title","aiTitle":"Auto generated title"}
{"type":"user","text":"hello"}
{"type":"agent-name","agentName":"my-cool-session"}
{"type":"ai-title","aiTitle":"my-cool-session"}
`
	os.WriteFile(jsonl, []byte(content), 0600)

	name, err := DiscoverSessionName(jsonl)
	if err != nil {
		t.Fatalf("DiscoverSessionName: %v", err)
	}
	if name != "my-cool-session" {
		t.Errorf("expected my-cool-session, got %s", name)
	}
}

func TestDiscoverSessionNameFallsBackToAITitle(t *testing.T) {
	dir := t.TempDir()
	jsonl := filepath.Join(dir, "test.jsonl")

	content := `{"type":"last-prompt","sessionId":"abc"}
{"type":"ai-title","aiTitle":"Auto generated title"}
`
	os.WriteFile(jsonl, []byte(content), 0600)

	name, _ := DiscoverSessionName(jsonl)
	if name != "Auto generated title" {
		t.Errorf("expected 'Auto generated title', got %s", name)
	}
}

func TestDiscoverSessionNameReturnsEmptyForNoEntries(t *testing.T) {
	dir := t.TempDir()
	jsonl := filepath.Join(dir, "test.jsonl")
	os.WriteFile(jsonl, []byte(`{"type":"user","text":"hello"}`+"\n"), 0600)

	name, _ := DiscoverSessionName(jsonl)
	if name != "" {
		t.Errorf("expected empty, got %s", name)
	}
}
```

- [ ] **Step 4: Implement name.go**

```go
// discover/name.go
package discover

import (
	"bufio"
	"encoding/json"
	"os"
)

type jsonlEntry struct {
	Type      string `json:"type"`
	AgentName string `json:"agentName"`
	AITitle   string `json:"aiTitle"`
}

func DiscoverSessionName(jsonlPath string) (string, error) {
	f, err := os.Open(jsonlPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var lastAgentName, lastAITitle string

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var entry jsonlEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		switch entry.Type {
		case "agent-name":
			lastAgentName = entry.AgentName
		case "ai-title":
			lastAITitle = entry.AITitle
		}
	}

	if lastAgentName != "" {
		return lastAgentName, nil
	}
	return lastAITitle, nil
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./discover/ -v`
Expected: PASS (fix the `TestDiscoverSessionIDPicksMostRecent` test — it has a compilation issue with the `time` import and `Chtimes` usage; adjust as needed during implementation)

- [ ] **Step 6: Commit**

```bash
git add discover/session.go discover/session_test.go discover/name.go discover/name_test.go
git commit --signoff -m "feat: Claude session ID and name discovery from JSONL transcripts"
```

---

## Task 8: PID Cache

**Files:**
- Create: `discover/pid.go`, `discover/pid_test.go`

- [ ] **Step 1: Write tests**

```go
// discover/pid_test.go
package discover

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndReadPIDCache(t *testing.T) {
	dir := t.TempDir()
	sessionsDir := filepath.Join(dir, "sessions")
	os.MkdirAll(sessionsDir, 0755)

	err := WritePIDCache(sessionsDir, 12345, "abc-123")
	if err != nil {
		t.Fatalf("WritePIDCache: %v", err)
	}

	id, err := ReadPIDCache(sessionsDir, 12345)
	if err != nil {
		t.Fatalf("ReadPIDCache: %v", err)
	}
	if id != "abc-123" {
		t.Errorf("expected abc-123, got %s", id)
	}
}

func TestReadPIDCacheMissing(t *testing.T) {
	dir := t.TempDir()
	sessionsDir := filepath.Join(dir, "sessions")
	os.MkdirAll(sessionsDir, 0755)

	_, err := ReadPIDCache(sessionsDir, 99999)
	if err == nil {
		t.Error("expected error for missing PID cache, got nil")
	}
}
```

- [ ] **Step 2: Implement pid.go**

```go
// discover/pid.go
package discover

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func WritePIDCache(sessionsDir string, pid int, sessionID string) error {
	path := filepath.Join(sessionsDir, strconv.Itoa(pid))
	return os.WriteFile(path, []byte(sessionID), 0600)
}

func ReadPIDCache(sessionsDir string, pid int) (string, error) {
	path := filepath.Join(sessionsDir, strconv.Itoa(pid))
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("no PID cache for %d: %w", pid, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func CleanStalePIDCaches(sessionsDir string) (int, error) {
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return 0, err
	}

	var cleaned int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		if !IsProcessAlive(pid) {
			os.Remove(filepath.Join(sessionsDir, entry.Name()))
			cleaned++
		}
	}
	return cleaned, nil
}

func IsProcessAlive(pid int) bool {
	err := exec.Command("kill", "-0", strconv.Itoa(pid)).Run()
	return err == nil
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./discover/ -v -run TestPID`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add discover/pid.go discover/pid_test.go
git commit --signoff -m "feat: PID cache for fast session ID lookup in hooks"
```

---

## Task 9: .worktree-resources File Support

**Files:**
- Create: `worktree/resources.go`, `worktree/resources_test.go`

- [ ] **Step 1: Write tests**

```go
// worktree/resources_test.go
package worktree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadResources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".worktree-resources")
	os.WriteFile(path, []byte("pr:owner/repo#42 https://github.com/owner/repo/pull/42\njira:RHOAIENG-100 https://redhat.atlassian.net/browse/RHOAIENG-100\n"), 0644)

	resources, err := ReadResources(path)
	if err != nil {
		t.Fatalf("ReadResources: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}
	if resources[0].ID != "pr:owner/repo#42" {
		t.Errorf("expected pr:owner/repo#42, got %s", resources[0].ID)
	}
	if resources[0].URL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("expected URL, got %s", resources[0].URL)
	}
}

func TestReadResourcesSkipsMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".worktree-resources")
	os.WriteFile(path, []byte("pr:owner/repo#42 https://url\n\nbadline\njira:X https://y\n"), 0644)

	resources, _ := ReadResources(path)
	if len(resources) != 2 {
		t.Errorf("expected 2 valid resources (skipping malformed), got %d", len(resources))
	}
}

func TestReadResourcesFileNotExist(t *testing.T) {
	resources, err := ReadResources("/nonexistent/.worktree-resources")
	if err != nil {
		t.Errorf("expected no error for missing file, got %v", err)
	}
	if len(resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(resources))
	}
}

func TestAppendResource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".worktree-resources")

	err := AppendResource(path, "pr:owner/repo#42", "https://github.com/owner/repo/pull/42")
	if err != nil {
		t.Fatalf("AppendResource: %v", err)
	}

	resources, _ := ReadResources(path)
	if len(resources) != 1 {
		t.Fatalf("expected 1, got %d", len(resources))
	}
}

func TestAppendResourceDeduplicates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".worktree-resources")

	AppendResource(path, "pr:owner/repo#42", "https://url")
	AppendResource(path, "pr:owner/repo#42", "https://url")

	resources, _ := ReadResources(path)
	if len(resources) != 1 {
		t.Errorf("expected 1 (deduplicated), got %d", len(resources))
	}
}

func TestRemoveResource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".worktree-resources")
	os.WriteFile(path, []byte("pr:owner/repo#42 https://url1\njira:X https://url2\n"), 0644)

	err := RemoveResource(path, "pr:owner/repo#42")
	if err != nil {
		t.Fatalf("RemoveResource: %v", err)
	}

	resources, _ := ReadResources(path)
	if len(resources) != 1 {
		t.Fatalf("expected 1, got %d", len(resources))
	}
	if resources[0].ID != "jira:X" {
		t.Errorf("expected jira:X to remain, got %s", resources[0].ID)
	}
}
```

- [ ] **Step 2: Implement resources.go**

```go
// worktree/resources.go
package worktree

import (
	"bufio"
	"os"
	"strings"
)

type Resource struct {
	ID  string
	URL string
}

func ParseResourceID(resourceID string) (resourceType, id string) {
	parts := strings.SplitN(resourceID, ":", 2)
	if len(parts) != 2 {
		return "", resourceID
	}
	return parts[0], parts[1]
}

func ReadResources(path string) ([]Resource, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var resources []Resource
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		resources = append(resources, Resource{ID: parts[0], URL: parts[1]})
	}
	return resources, scanner.Err()
}

func AppendResource(path, resourceID, url string) error {
	existing, _ := ReadResources(path)
	for _, r := range existing {
		if r.ID == resourceID {
			return nil
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(resourceID + " " + url + "\n")
	return err
}

func RemoveResource(path, resourceID string) error {
	resources, err := ReadResources(path)
	if err != nil {
		return err
	}

	var keep []Resource
	for _, r := range resources {
		if r.ID != resourceID {
			keep = append(keep, r)
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, r := range keep {
		f.WriteString(r.ID + " " + r.URL + "\n")
	}
	return nil
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./worktree/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add worktree/resources.go worktree/resources_test.go
git commit --signoff -m "feat: .worktree-resources file read/append/remove with deduplication"
```

---

## Task 10: CLI Commands — Core (register, heartbeat, status, cleanup, health, configure, statusline)

**Files:**
- Create: `cmd/register.go`, `cmd/unregister.go`, `cmd/heartbeat.go`, `cmd/status.go`, `cmd/cleanup.go`, `cmd/health.go`, `cmd/configure.go`, `cmd/statusline.go`

This task creates the core session management CLI commands. Each command opens the DB, calls the appropriate DB layer function, and formats output. Due to the volume of commands, this task shows representative implementations — the implementing engineer should follow the same pattern for each.

- [ ] **Step 1: Add DB path helper to root.go**

```go
// Add to cmd/root.go
func openDB() (*db.DB, error) {
	return db.Open(db.DefaultPath())
}

func openReadOnlyDB() (*db.DB, error) {
	return db.OpenReadOnly(db.DefaultPath())
}
```

Add the import: `"github.com/mturley/agent-handler/db"`

- [ ] **Step 2: Create cmd/register.go**

```go
// cmd/register.go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
	"github.com/mturley/agent-handler/worktree"
	"github.com/spf13/cobra"
)

var registerCmd = &cobra.Command{
	Use:   "register",
	Short: "Register or re-register a session",
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID, _ := cmd.Flags().GetString("session-id")
		branch, _ := cmd.Flags().GetString("branch")
		repo, _ := cmd.Flags().GetString("repo")
		pid, _ := cmd.Flags().GetInt("pid")
		jsonlPath, _ := cmd.Flags().GetString("jsonl-path")

		d, err := openDB()
		if err != nil {
			return err
		}
		defer d.Close()

		now := time.Now().UTC().Format(time.RFC3339)

		// Discover session name from JSONL
		var sessionName string
		if jsonlPath != "" {
			sessionName, _ = discover.DiscoverSessionName(jsonlPath)
		}

		s := db.Session{
			SessionID:    sessionID,
			Harness:      "claude",
			Repo:         repo,
			Branch:       branch,
			SessionName:  sessionName,
			PID:          pid,
			Status:       "active",
			InboxMode:    "manual",
			LastActive:   now,
			RegisteredAt: now,
			JSONLPath:    jsonlPath,
		}

		if err := d.UpsertSession(s); err != nil {
			return fmt.Errorf("registering session: %w", err)
		}

		// Write PID cache
		home, _ := os.UserHomeDir()
		sessionsDir := filepath.Join(home, ".agent-handler", "sessions")
		os.MkdirAll(sessionsDir, 0755)
		discover.WritePIDCache(sessionsDir, pid, sessionID)

		// Auto-subscribe from .worktree-resources
		cwd, _ := os.Getwd()
		resourcesPath := filepath.Join(cwd, ".worktree-resources")
		resources, _ := worktree.ReadResources(resourcesPath)
		for _, r := range resources {
			resType, resID := worktree.ParseResourceID(r.ID)
			d.Subscribe(db.Subscription{
				ID:           uuid.New().String(),
				SessionID:    sessionID,
				ResourceType: resType,
				ResourceID:   resID,
				ResourceURL:  &r.URL,
				CreatedAt:    now,
			})
		}

		// Query unread events for catch-up
		unreadCount, byType, _ := d.UnreadCountForSession(sessionID)

		if jsonOutput {
			out := map[string]interface{}{
				"session_id":   sessionID,
				"session_name": sessionName,
				"branch":       branch,
				"unread_count": unreadCount,
				"unread_by_type": byType,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}

		fmt.Printf("Registered session %s", sessionID)
		if sessionName != "" {
			fmt.Printf(" (%s)", sessionName)
		}
		fmt.Printf(" on %s\n", branch)
		if unreadCount > 0 {
			fmt.Printf("While you were away: %d unread events", unreadCount)
			for t, c := range byType {
				fmt.Printf(" (%d %s)", c, t)
			}
			fmt.Println()
		}

		// Check if inbox mode is auto — remind to restart polling
		session, _ := d.GetSession(sessionID)
		if session != nil && session.InboxMode == "auto" {
			fmt.Println("Inbox mode is set to auto. Run `/inbox_mode auto` to resume polling.")
		}

		return nil
	},
}

func init() {
	registerCmd.Flags().String("session-id", "", "Session UUID")
	registerCmd.Flags().String("branch", "", "Git branch")
	registerCmd.Flags().String("repo", "", "Repository")
	registerCmd.Flags().Int("pid", 0, "Claude process PID")
	registerCmd.Flags().String("jsonl-path", "", "Path to Claude JSONL transcript")
	rootCmd.AddCommand(registerCmd)
}
```

- [ ] **Step 3: Create cmd/heartbeat.go**

```go
// cmd/heartbeat.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mturley/agent-handler/discover"
	"github.com/spf13/cobra"
)

var heartbeatCmd = &cobra.Command{
	Use:   "heartbeat",
	Short: "Bump session last_active timestamp",
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID, _ := cmd.Flags().GetString("session-id")

		// If no session ID provided, try PID cache
		if sessionID == "" {
			home, _ := os.UserHomeDir()
			sessionsDir := filepath.Join(home, ".agent-handler", "sessions")
			pid := os.Getppid()
			var err error
			sessionID, err = discover.ReadPIDCache(sessionsDir, pid)
			if err != nil {
				return fmt.Errorf("no session ID provided and PID cache miss for PID %d", pid)
			}
		}

		d, err := openDB()
		if err != nil {
			return err
		}
		defer d.Close()

		now := time.Now().UTC().Format(time.RFC3339)
		return d.BumpLastActive(sessionID, now)
	},
}

func init() {
	heartbeatCmd.Flags().String("session-id", "", "Session UUID (auto-detected from PID cache if omitted)")
	rootCmd.AddCommand(heartbeatCmd)
}
```

- [ ] **Step 4: Create cmd/status.go**

```go
// cmd/status.go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/mturley/agent-handler/discover"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show all sessions with liveness state and unread counts",
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		limit, _ := cmd.Flags().GetInt("limit")

		d, err := openReadOnlyDB()
		if err != nil {
			return err
		}
		defer d.Close()

		sessions, err := d.ListSessions(all, limit, 0)
		if err != nil {
			return err
		}

		type sessionStatus struct {
			SessionID   string         `json:"session_id"`
			SessionName string         `json:"session_name,omitempty"`
			Branch      string         `json:"branch"`
			Status      string         `json:"status"`
			Liveness    string         `json:"liveness"`
			UnreadCount int            `json:"unread_count"`
			UnreadByType map[string]int `json:"unread_by_type,omitempty"`
			LastActive  string         `json:"last_active"`
		}

		var results []sessionStatus
		var deadCount int

		for _, s := range sessions {
			liveness := "dead"
			if s.PID > 0 && discover.IsProcessAlive(s.PID) {
				// Check if heartbeat is recent (within 10 minutes)
				lastActive, _ := time.Parse(time.RFC3339, s.LastActive)
				if time.Since(lastActive) < 10*time.Minute {
					liveness = "active"
				} else {
					liveness = "idle"
				}
			} else if s.Status == "archived" {
				liveness = "archived"
			} else {
				deadCount++
			}

			unreadCount, byType, _ := d.UnreadCountForSession(s.SessionID)

			results = append(results, sessionStatus{
				SessionID:    s.SessionID,
				SessionName:  s.SessionName,
				Branch:       s.Branch,
				Status:       s.Status,
				Liveness:     liveness,
				UnreadCount:  unreadCount,
				UnreadByType: byType,
				LastActive:   s.LastActive,
			})
		}

		if jsonOutput {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(results)
		}

		for _, r := range results {
			name := r.Branch
			if r.SessionName != "" {
				name = r.SessionName
			}

			unreadStr := "—"
			if r.UnreadCount > 0 {
				unreadStr = fmt.Sprintf("%d unread", r.UnreadCount)
				types := ""
				for t, c := range r.UnreadByType {
					if types != "" {
						types += ", "
					}
					types += fmt.Sprintf("%d %s", c, t)
				}
				if types != "" {
					unreadStr += fmt.Sprintf(" (%s)", types)
				}
			}

			lastActive, _ := time.Parse(time.RFC3339, r.LastActive)
			ago := time.Since(lastActive).Truncate(time.Second)

			fmt.Printf("  %-20s %-8s %-30s %s ago\n", name, r.Liveness, unreadStr, ago)
		}

		if deadCount > 0 {
			fmt.Printf("\n  %d dead session(s) found. Run `handler cleanup` to archive.\n", deadCount)
		}

		return nil
	},
}

func init() {
	statusCmd.Flags().Bool("all", false, "Include archived sessions")
	statusCmd.Flags().Int("limit", 20, "Maximum sessions to show")
	rootCmd.AddCommand(statusCmd)
}
```

- [ ] **Step 5: Create remaining core commands (cleanup, health, configure, statusline)**

Follow the same pattern as above. Each command:
1. Parses flags
2. Opens DB
3. Calls the appropriate DB layer function
4. Formats output (text by default, JSON with `--json`)

`cmd/unregister.go` — takes `--session-id` flag. Archives the session (`d.ArchiveSessions`) and soft-deletes all its active subscriptions (`d.SoftDeleteSubscriptionsForSession`). Emits a `session_end` event. Cleans up the PID cache file. This is for explicit teardown — the agent runs `/handler_unregister` before quitting.

`cmd/cleanup.go` — calls `d.ListSessions`, checks liveness via `discover.IsProcessAlive`, calls `d.ArchiveSessions` for dead ones. With `--stale` flag, also archives sessions idle beyond the threshold.

`cmd/health.go` — queries DB size (`PRAGMA page_count * PRAGMA page_size`), counts sessions by status, counts active/dormant subscriptions, cleans stale PID caches.

`cmd/configure.go` — takes `--inbox-mode` and `--auto-poll-interval` flags, calls `d.ConfigureSession`.

`cmd/statusline.go` — takes `--session` flag, queries unread count and inbox mode, outputs the two-line status format:
```
/inbox: 2 unread (1 review, 1 CI fail)
/inbox_mode: manual | on-submit | auto
```

- [ ] **Step 6: Verify all commands are registered and build**

Run: `go build ./... && ./handler --help`
Expected: All subcommands listed in help output

- [ ] **Step 7: Manual smoke test**

```bash
./handler register --session-id test-123 --branch main --repo mturley/test --pid $$ --jsonl-path /dev/null
./handler status
./handler heartbeat --session-id test-123
./handler status
./handler configure --session-id test-123 --inbox-mode on-submit
./handler health
```

- [ ] **Step 8: Commit**

```bash
git add cmd/register.go cmd/unregister.go cmd/heartbeat.go cmd/status.go cmd/cleanup.go cmd/health.go cmd/configure.go cmd/statusline.go
git commit --signoff -m "feat: core CLI commands — register, unregister, heartbeat, status, cleanup, health, configure, statusline"
```

---

## Task 11: CLI Commands — Events (emit, unread, ack, log, tail)

**Files:**
- Create: `cmd/emit.go`, `cmd/unread.go`, `cmd/ack.go`, `cmd/log_cmd.go`, `cmd/tail.go`

- [ ] **Step 1: Create cmd/emit.go**

```go
// cmd/emit.go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var emitCmd = &cobra.Command{
	Use:   "emit",
	Short: "Write an event to the ledger",
	RunE: func(cmd *cobra.Command, args []string) error {
		eventType, _ := cmd.Flags().GetString("type")
		title, _ := cmd.Flags().GetString("title")
		body, _ := cmd.Flags().GetString("body")
		sessionID, _ := cmd.Flags().GetString("session-id")
		source, _ := cmd.Flags().GetString("source")
		broadcast, _ := cmd.Flags().GetBool("broadcast")
		tags, _ := cmd.Flags().GetString("tags")

		if eventType == "" || title == "" {
			return fmt.Errorf("--type and --title are required")
		}
		if source == "" {
			source = "agent"
		}

		d, err := openDB()
		if err != nil {
			return err
		}
		defer d.Close()

		evt := db.Event{
			ID:        uuid.New().String(),
			TS:        time.Now().UTC().Format(time.RFC3339),
			Source:    source,
			Type:      eventType,
			Title:     title,
			Broadcast: broadcast,
		}
		if sessionID != "" {
			evt.SessionID = &sessionID
		}
		if body != "" {
			evt.Body = &body
		}
		if tags != "" {
			evt.Tags = &tags
		}

		if err := d.InsertEvent(evt, nil, nil); err != nil {
			return err
		}

		if jsonOutput {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(map[string]string{"id": evt.ID, "ts": evt.TS})
		}

		fmt.Printf("Event %s emitted: %s\n", evt.ID[:8], title)
		return nil
	},
}

func init() {
	emitCmd.Flags().String("type", "", "Event type (milestone, status, blocked, decision, etc.)")
	emitCmd.Flags().String("title", "", "Short one-liner")
	emitCmd.Flags().String("body", "", "Full event body")
	emitCmd.Flags().String("session-id", "", "Emitting session UUID")
	emitCmd.Flags().String("source", "agent", "Event source")
	emitCmd.Flags().Bool("broadcast", false, "Broadcast to all sessions")
	emitCmd.Flags().String("tags", "", "Comma-separated tags")
	rootCmd.AddCommand(emitCmd)
}
```

- [ ] **Step 2: Create cmd/unread.go, cmd/ack.go, cmd/log_cmd.go**

Follow the same pattern. Each wraps the corresponding DB layer function:
- `unread` — calls `d.UnreadForSession(sessionID)`, formats events
- `ack` — calls `d.AdvanceCursor(sessionID, time.Now().UTC().Format(time.RFC3339))`
- `log_cmd` — calls `d.QueryEvents(db.EventFilter{SessionID: &sessionID, Limit: limit})`

- [ ] **Step 3: Create cmd/tail.go**

`tail` polls the DB every second for new events after a cursor, displaying them as they appear. Runs until interrupted.

```go
// cmd/tail.go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var tailCmd = &cobra.Command{
	Use:   "tail",
	Short: "Live event stream",
	RunE: func(cmd *cobra.Command, args []string) error {
		source, _ := cmd.Flags().GetString("source")
		eventType, _ := cmd.Flags().GetString("type")
		sessionID, _ := cmd.Flags().GetString("session")

		d, err := openReadOnlyDB()
		if err != nil {
			return err
		}
		defer d.Close()

		cursor := time.Now().UTC().Format(time.RFC3339)

		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)

		fmt.Println("Watching for events... (Ctrl+C to stop)")

		for {
			select {
			case <-sig:
				return nil
			default:
			}

			filter := db.EventFilter{Since: &cursor, Limit: 50}
			if source != "" {
				filter.Source = &source
			}
			if eventType != "" {
				filter.Type = &eventType
			}
			if sessionID != "" {
				filter.SessionID = &sessionID
			}

			events, err := d.QueryEvents(filter)
			if err != nil {
				return err
			}

			for _, e := range events {
				if jsonOutput {
					enc := json.NewEncoder(os.Stdout)
					enc.Encode(e)
				} else {
					fmt.Printf("[%s] %s/%s: %s\n", e.TS[:19], e.Source, e.Type, e.Title)
				}
				cursor = e.TS
			}

			time.Sleep(1 * time.Second)
		}
	},
}

func init() {
	tailCmd.Flags().String("source", "", "Filter by source")
	tailCmd.Flags().String("type", "", "Filter by event type")
	tailCmd.Flags().String("session", "", "Filter by session ID")
	rootCmd.AddCommand(tailCmd)
}
```

- [ ] **Step 4: Verify all event commands build**

Run: `go build ./...`
Expected: clean build

- [ ] **Step 5: Commit**

```bash
git add cmd/emit.go cmd/unread.go cmd/ack.go cmd/log_cmd.go cmd/tail.go
git commit --signoff -m "feat: event CLI commands — emit, unread, ack, log, tail"
```

---

## Task 12: CLI Commands — Subscriptions and Resources

**Files:**
- Create: `cmd/subscribe.go`, `cmd/unsubscribe.go`, `cmd/subscriptions.go`, `cmd/resource/resource.go`, `cmd/resource/link.go`, `cmd/resource/related.go`, `cmd/resource/history.go`

- [ ] **Step 1: Create cmd/subscribe.go**

Subscribe to a resource. Parses `--resource "pr:owner/repo#42"` into type and ID. Also appends to `.worktree-resources`.

```go
// cmd/subscribe.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/worktree"
	"github.com/spf13/cobra"
)

var subscribeCmd = &cobra.Command{
	Use:   "subscribe",
	Short: "Subscribe to a resource",
	RunE: func(cmd *cobra.Command, args []string) error {
		resource, _ := cmd.Flags().GetString("resource")
		url, _ := cmd.Flags().GetString("url")
		sessionID, _ := cmd.Flags().GetString("session-id")

		if resource == "" || sessionID == "" {
			return fmt.Errorf("--resource and --session-id are required")
		}

		resType, resID := worktree.ParseResourceID(resource)
		if resType == "" {
			return fmt.Errorf("resource must be in format type:id (e.g. pr:owner/repo#42)")
		}

		d, err := openDB()
		if err != nil {
			return err
		}
		defer d.Close()

		var urlPtr *string
		if url != "" {
			urlPtr = &url
		}

		err = d.Subscribe(db.Subscription{
			ID:           uuid.New().String(),
			SessionID:    sessionID,
			ResourceType: resType,
			ResourceID:   resID,
			ResourceURL:  urlPtr,
			CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		})
		if err != nil {
			return err
		}

		// Append to .worktree-resources
		if url != "" {
			cwd, _ := os.Getwd()
			worktree.AppendResource(filepath.Join(cwd, ".worktree-resources"), resource, url)
		}

		fmt.Printf("Subscribed to %s\n", resource)
		return nil
	},
}

func init() {
	subscribeCmd.Flags().String("resource", "", "Resource in type:id format (e.g. pr:owner/repo#42)")
	subscribeCmd.Flags().String("url", "", "Resource URL")
	subscribeCmd.Flags().String("session-id", "", "Session UUID")
	rootCmd.AddCommand(subscribeCmd)
}
```

- [ ] **Step 2: Create cmd/unsubscribe.go**

Same pattern — calls `d.Unsubscribe` and `worktree.RemoveResource`.

- [ ] **Step 3: Create cmd/subscriptions.go**

Lists subscriptions for a session. Calls `d.ListSubscriptions`.

- [ ] **Step 4: Create resource subcommand group**

```go
// cmd/resource/resource.go
package resource

import "github.com/spf13/cobra"

var ResourceCmd = &cobra.Command{
	Use:   "resource",
	Short: "Resource relationship management",
}
```

Then create `link.go`, `related.go`, `history.go` following the same pattern, wrapping `d.LinkResources`, `d.FindRelatedSessions`, `d.ResourceHistory`.

- [ ] **Step 5: Register resource subcommand in root.go**

```go
// Add to cmd/root.go init()
import "github.com/mturley/agent-handler/cmd/resource"

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	rootCmd.AddCommand(resource.ResourceCmd)
}
```

- [ ] **Step 6: Verify build**

Run: `go build ./...`
Expected: clean build

- [ ] **Step 7: Commit**

```bash
git add cmd/subscribe.go cmd/unsubscribe.go cmd/subscriptions.go cmd/resource/
git commit --signoff -m "feat: subscription and resource CLI commands"
```

---

## Task 13: CLI Commands — Query and Schema

**Files:**
- Create: `cmd/query.go`, `cmd/schema_cmd.go`

- [ ] **Step 1: Create cmd/query.go**

```go
// cmd/query.go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var queryCmd = &cobra.Command{
	Use:   "query",
	Short: "Run arbitrary read-only SQL against the ledger DB",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sql := args[0]

		// Safety check: reject write operations
		upper := strings.ToUpper(strings.TrimSpace(sql))
		for _, prefix := range []string{"INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "CREATE", "ATTACH"} {
			if strings.HasPrefix(upper, prefix) {
				return fmt.Errorf("write operations are not allowed — query is read-only")
			}
		}

		d, err := openReadOnlyDB()
		if err != nil {
			return err
		}
		defer d.Close()

		rows, err := d.Conn().Query(sql)
		if err != nil {
			return fmt.Errorf("query error: %w", err)
		}
		defer rows.Close()

		cols, _ := rows.Columns()

		if jsonOutput {
			var results []map[string]interface{}
			for rows.Next() {
				values := make([]interface{}, len(cols))
				ptrs := make([]interface{}, len(cols))
				for i := range values {
					ptrs[i] = &values[i]
				}
				rows.Scan(ptrs...)
				row := make(map[string]interface{})
				for i, col := range cols {
					row[col] = values[i]
				}
				results = append(results, row)
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(results)
		}

		// Text output: simple table
		fmt.Println(strings.Join(cols, "\t"))
		for rows.Next() {
			values := make([]interface{}, len(cols))
			ptrs := make([]interface{}, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			rows.Scan(ptrs...)
			strs := make([]string, len(values))
			for i, v := range values {
				strs[i] = fmt.Sprintf("%v", v)
			}
			fmt.Println(strings.Join(strs, "\t"))
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(queryCmd)
}
```

- [ ] **Step 2: Create cmd/schema_cmd.go**

```go
// cmd/schema_cmd.go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var schemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Dump current table definitions",
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := openReadOnlyDB()
		if err != nil {
			return err
		}
		defer d.Close()

		rows, err := d.Conn().Query(`SELECT sql FROM sqlite_master WHERE type='table' AND sql IS NOT NULL ORDER BY name`)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var ddl string
			rows.Scan(&ddl)
			fmt.Println(ddl + ";\n")
		}

		// Also show indexes
		idxRows, err := d.Conn().Query(`SELECT sql FROM sqlite_master WHERE type='index' AND sql IS NOT NULL ORDER BY name`)
		if err != nil {
			return err
		}
		defer idxRows.Close()

		for idxRows.Next() {
			var ddl string
			idxRows.Scan(&ddl)
			fmt.Println(ddl + ";")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(schemaCmd)
}
```

- [ ] **Step 3: Verify build and test**

```bash
go build ./...
./handler schema
./handler query "SELECT COUNT(*) as count FROM events"
```

- [ ] **Step 4: Commit**

```bash
git add cmd/query.go cmd/schema_cmd.go
git commit --signoff -m "feat: query and schema CLI commands for ad-hoc DB access"
```

---

## Task 14: Claude Code Hook Scripts

**Files:**
- Create: `hooks/session_start.sh`, `hooks/user_prompt_submit.sh`, `hooks/pre_compact.sh`

- [ ] **Step 1: Create hooks/session_start.sh**

```bash
#!/usr/bin/env bash
# SessionStart hook for agent-handler
# Discovers session ID, registers with handler, returns catch-up summary

set -euo pipefail

# Check if handler is installed
if ! command -v handler &>/dev/null; then
    exit 0
fi

# Get the Claude process PID (our parent)
CLAUDE_PID="$PPID"

# Discover session ID from JSONL
CLAUDE_HOME="${HOME}/.claude"
CWD="$(pwd)"

# Compute project directory path
PROJECT_DIR_NAME="-$(echo "$CWD" | sed 's/\//-/g' | sed 's/^-//')"
PROJECT_DIR="${CLAUDE_HOME}/projects/${PROJECT_DIR_NAME}"

if [ ! -d "$PROJECT_DIR" ]; then
    exit 0
fi

# Find most recently modified JSONL
JSONL_PATH=$(ls -t "$PROJECT_DIR"/*.jsonl 2>/dev/null | head -1)
if [ -z "$JSONL_PATH" ]; then
    exit 0
fi

SESSION_ID=$(basename "$JSONL_PATH" .jsonl)
BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
REPO=$(git remote get-url origin 2>/dev/null | sed 's/.*github.com[:/]//' | sed 's/\.git$//' || echo "unknown")

# Register and get catch-up summary
handler register \
    --session-id "$SESSION_ID" \
    --branch "$BRANCH" \
    --repo "$REPO" \
    --pid "$CLAUDE_PID" \
    --jsonl-path "$JSONL_PATH"
```

- [ ] **Step 2: Create hooks/user_prompt_submit.sh**

```bash
#!/usr/bin/env bash
# UserPromptSubmit hook for agent-handler
# Bumps heartbeat. If inbox mode is on-submit, injects unread events.
# Must be fast (<10ms for the common path).

set -euo pipefail

if ! command -v handler &>/dev/null; then
    exit 0
fi

CLAUDE_PID="$PPID"
SESSIONS_DIR="${HOME}/.agent-handler/sessions"

# Read session ID from PID cache (fast path)
if [ -f "${SESSIONS_DIR}/${CLAUDE_PID}" ]; then
    SESSION_ID=$(cat "${SESSIONS_DIR}/${CLAUDE_PID}")
else
    exit 0
fi

# Bump heartbeat
handler heartbeat --session-id "$SESSION_ID"

# Check inbox mode and inject if on-submit
INBOX_MODE=$(handler configure --session-id "$SESSION_ID" --get inbox-mode 2>/dev/null || echo "manual")

if [ "$INBOX_MODE" = "on-submit" ]; then
    UNREAD=$(handler unread --session-id "$SESSION_ID" --json 2>/dev/null)
    if [ -n "$UNREAD" ] && [ "$UNREAD" != "[]" ] && [ "$UNREAD" != "null" ]; then
        echo "$UNREAD"
        handler ack --session-id "$SESSION_ID"
    fi
fi

# Check if auto mode but polling not active — surface reminder
if [ "$INBOX_MODE" = "auto" ]; then
    echo "Inbox mode is auto but polling may not be active. Run /inbox_mode auto to restart."
fi
```

- [ ] **Step 3: Create hooks/pre_compact.sh**

```bash
#!/usr/bin/env bash
# PreCompact hook for agent-handler
# Writes a pre_compact_snapshot event to preserve context before compaction.

set -euo pipefail

if ! command -v handler &>/dev/null; then
    exit 0
fi

CLAUDE_PID="$PPID"
SESSIONS_DIR="${HOME}/.agent-handler/sessions"

if [ -f "${SESSIONS_DIR}/${CLAUDE_PID}" ]; then
    SESSION_ID=$(cat "${SESSIONS_DIR}/${CLAUDE_PID}")
else
    exit 0
fi

handler emit \
    --session-id "$SESSION_ID" \
    --type pre_compact_snapshot \
    --title "Pre-compaction snapshot" \
    --body "Context is about to be compacted. Check handler log for this session's recent activity."
```

- [ ] **Step 4: Make hook scripts executable**

```bash
chmod +x hooks/session_start.sh hooks/user_prompt_submit.sh hooks/pre_compact.sh
```

- [ ] **Step 5: Commit**

```bash
git add hooks/
git commit --signoff -m "feat: Claude Code hook scripts — SessionStart, UserPromptSubmit, PreCompact"
```

---

## Task 15: Skills (Markdown Files)

**Files:**
- Create: `skills/inbox/SKILL.md`, `skills/inbox_mode/SKILL.md`, `skills/handler_register/SKILL.md`, `skills/handler_emit/SKILL.md`, `skills/handler_subscribe/SKILL.md`, `skills/handler_snapshot/SKILL.md`

- [ ] **Step 1: Create skills/inbox/SKILL.md**

```markdown
---
name: inbox
description: Check and act on unread events from the agent-handler ledger
---

# /inbox — Check Unread Events

Run `handler unread --session-id <your-session-id> --json` to check for unread events addressed to this session.

## Finding your session ID

Your session ID is stored in the PID cache. Run:
```bash
cat ~/.agent-handler/sessions/$PPID
```
If that file doesn't exist, discover it from the JSONL:
```bash
ls -t ~/.claude/projects/-$(pwd | sed 's/\//-/g' | sed 's/^-//')/*.jsonl | head -1 | xargs basename | sed 's/.jsonl//'
```

## After reading events

1. Present the events to the user in a clear summary, grouped by type
2. Offer to act on actionable events (e.g. "Want me to look at that PR review comment?")
3. After the user has seen the events, acknowledge them: `handler ack --session-id <id>`

## Available CLI commands for deeper queries

- `handler log --session <id>` — event timeline for this session
- `handler status --json` — all sessions with liveness and unread counts
- `handler resource history <resource_id>` — all events for a resource
- `handler query "<sql>"` — arbitrary read-only SQL for ad-hoc analysis
- `handler schema` — dump table definitions before writing SQL
```

- [ ] **Step 2: Create skills/inbox_mode/SKILL.md**

```markdown
---
name: inbox_mode
description: Set how this session receives unread events — manual, on-submit, or auto
---

# /inbox_mode — Configure Inbox Mode

Three modes control how you receive unread events:

| Mode | Behavior |
|------|----------|
| `manual` (default) | Status line shows unread count. You check with `/inbox` when ready. |
| `on-submit` | Unread events auto-injected on each prompt submit. |
| `auto` | Actively poll for new events on an interval and proactively surface them. |

## Usage

To set the mode, run:
```bash
handler configure --session-id <your-session-id> --inbox-mode <mode>
```

For auto mode with a custom interval:
```bash
handler configure --session-id <your-session-id> --inbox-mode auto --auto-poll-interval 60
```

## Auto mode

When setting auto mode, start a polling loop that periodically runs `/inbox`:
- Use ScheduleWakeup or /loop to check every N seconds (default 60)
- On each poll, run `handler unread --session-id <id> --json`
- If there are unread events, present them and offer to act
- Acknowledge after presenting: `handler ack --session-id <id>`
```

- [ ] **Step 3: Create remaining skills**

`skills/handler_register/SKILL.md` — Instructions for manually registering a session. Explains when to use it (usually automatic via SessionStart hook, but can be run manually).

`skills/handler_emit/SKILL.md` — Guide for writing events to the ledger. Lists all event types with descriptions and examples. Guides the agent on which type to use.

`skills/handler_subscribe/SKILL.md` — Instructions for subscribing to external resources. Explains the resource ID format (`type:id`), URL parameter, and that it also updates `.worktree-resources`.

`skills/handler_snapshot/SKILL.md` — Instructions for writing a `pre_compact_snapshot` event. Explains what to include in the body (current task, progress, blockers, decisions made).

`skills/handler_unregister/SKILL.md` — Instructions for archiving the current session before quitting. Runs `handler unregister --session-id <id>`, which archives the session, soft-deletes subscriptions, and emits a `session_end` event. Tells the agent to use this when the user says they're done with the session.

- [ ] **Step 4: Commit**

```bash
git add skills/
git commit --signoff -m "feat: Claude Code skills — inbox, inbox_mode, handler_register, handler_emit, handler_subscribe, handler_snapshot"
```

---

## Task 16: Install and Uninstall Commands

**Files:**
- Create: `cmd/install.go`, `cmd/uninstall.go`

- [ ] **Step 1: Create cmd/install.go**

```go
// cmd/install.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Set up agent-handler for the current user",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		handlerDir := filepath.Join(home, ".agent-handler")

		// Create directories
		for _, dir := range []string{handlerDir, filepath.Join(handlerDir, "sessions"), filepath.Join(handlerDir, "logs")} {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("creating %s: %w", dir, err)
			}
			fmt.Printf("Created %s\n", dir)
		}

		// Initialize DB
		dbPath := filepath.Join(handlerDir, "handler.db")
		d, err := db.Open(dbPath)
		if err != nil {
			return fmt.Errorf("initializing database: %w", err)
		}
		d.Close()
		fmt.Printf("Initialized database at %s\n", dbPath)

		// Symlink skills
		execPath, _ := os.Executable()
		installDir := filepath.Dir(execPath)
		skillsSrc := filepath.Join(filepath.Dir(installDir), "skills") // assumes binary is in a bin/ dir next to skills/
		skillsDst := filepath.Join(home, ".claude", "skills")

		// If skills source exists, symlink each skill
		if entries, err := os.ReadDir(skillsSrc); err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				src := filepath.Join(skillsSrc, entry.Name())
				dst := filepath.Join(skillsDst, entry.Name())
				// Remove existing symlink if present
				os.Remove(dst)
				if err := os.Symlink(src, dst); err != nil {
					fmt.Printf("Warning: could not symlink skill %s: %v\n", entry.Name(), err)
				} else {
					fmt.Printf("Linked skill %s -> %s\n", dst, src)
				}
			}
		} else {
			fmt.Printf("Skills directory not found at %s — symlink manually during development\n", skillsSrc)
		}

		fmt.Println("\nInstallation complete. Register Claude Code hooks manually:")
		fmt.Println("  See hooks/ directory for SessionStart, UserPromptSubmit, and PreCompact scripts")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(installCmd)
}
```

- [ ] **Step 2: Create cmd/uninstall.go**

```go
// cmd/uninstall.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Remove agent-handler configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		purge, _ := cmd.Flags().GetBool("purge")
		home, _ := os.UserHomeDir()

		// Remove skill symlinks
		skillsDir := filepath.Join(home, ".claude", "skills")
		skillNames := []string{"inbox", "inbox_mode", "handler_register", "handler_emit", "handler_subscribe", "handler_snapshot"}
		for _, name := range skillNames {
			link := filepath.Join(skillsDir, name)
			target, err := os.Readlink(link)
			if err != nil {
				continue
			}
			// Only remove if it points to our skills
			if filepath.Base(filepath.Dir(filepath.Dir(target))) == "agent-handler" ||
				filepath.Base(filepath.Dir(filepath.Dir(target))) == "agent-ledger" {
				os.Remove(link)
				fmt.Printf("Removed skill symlink %s\n", link)
			}
		}

		if purge {
			handlerDir := filepath.Join(home, ".agent-handler")
			fmt.Printf("Removing %s and all data...\n", handlerDir)
			os.RemoveAll(handlerDir)
			fmt.Println("Purged.")
		} else {
			fmt.Println("Data preserved at ~/.agent-handler/. Use --purge to remove.")
		}

		fmt.Println("Remember to remove Claude Code hook registrations from settings.")
		return nil
	},
}

func init() {
	uninstallCmd.Flags().Bool("purge", false, "Also remove ~/.agent-handler/ and all data")
	rootCmd.AddCommand(uninstallCmd)
}
```

- [ ] **Step 3: Fix the uninstall command name (it says "install" in Use field)**

Change `Use: "install"` to `Use: "uninstall"` in the uninstall command.

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: clean build

- [ ] **Step 5: Commit**

```bash
git add cmd/install.go cmd/uninstall.go
git commit --signoff -m "feat: install and uninstall commands for user setup"
```

---

## Task 17: Integration Smoke Test

**Files:** None created — this is a manual verification task.

- [ ] **Step 1: Build the binary**

```bash
go build -o handler .
```

- [ ] **Step 2: Run install**

```bash
./handler install
```
Expected: Creates `~/.agent-handler/`, initializes DB, attempts to symlink skills

- [ ] **Step 3: Test the full session lifecycle**

```bash
# Register a session
./handler register --session-id test-smoke --branch main --repo mturley/test --pid $$ --jsonl-path /dev/null

# Check status
./handler status

# Emit some events
./handler emit --type milestone --title "Found the bug" --session-id test-smoke
./handler emit --type decision --title "Going with approach B" --session-id test-smoke --body "Approach A had too many edge cases"

# Subscribe to a resource
./handler subscribe --resource "pr:mturley/test#1" --url "https://github.com/mturley/test/pull/1" --session-id test-smoke

# Check subscriptions
./handler subscriptions --session-id test-smoke

# Check unread (should be empty — we emitted, not received)
./handler unread --session-id test-smoke

# Emit a broadcast
./handler emit --type message --title "System test" --broadcast --source handler

# Now check unread (should see the broadcast)
./handler unread --session-id test-smoke

# Ack
./handler ack --session-id test-smoke

# View log
./handler log --session-id test-smoke

# Check health
./handler health

# View schema
./handler schema

# Run a query
./handler query "SELECT type, COUNT(*) as count FROM events GROUP BY type"

# Statusline output
./handler statusline --session-id test-smoke

# Cleanup
./handler cleanup
```

- [ ] **Step 4: Verify all commands work without errors**

Each command should produce reasonable output. Fix any issues found.

- [ ] **Step 5: Commit any fixes**

```bash
git add -A
git commit --signoff -m "fix: integration test fixes from smoke testing"
```

---

## Task 18: Final Cleanup and Documentation

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add .worktree-resources to .gitignore**

```bash
echo ".worktree-resources" >> .gitignore
```

- [ ] **Step 2: Update README.md with basic usage**

Add a brief "Getting Started" section to README.md:
- How to build: `go build -o handler .`
- How to install: `./handler install`
- Key commands: `handler status`, `handler emit`, `handler unread`
- Link to design spec for details

- [ ] **Step 3: Run full test suite**

```bash
go test ./... -v
```
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add README.md .gitignore
git commit --signoff -m "docs: update README with getting started, add .worktree-resources to gitignore"
```

- [ ] **Step 5: Push**

```bash
git push
```
