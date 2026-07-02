# Phase 4: Session Inspection (Peek) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add terminal inspection (`handler peek`) and notifications so the handler session can observe what other Claude sessions are doing and get alerted when new events arrive.

**Architecture:** A `terminal/` package provides a `Backend` interface with cmux and tmux implementations. Sessions store their terminal environment at registration time. `handler peek` captures pane content via the appropriate backend. `handler claude` is a thin wrapper that ensures sessions are peekable. The statusline hook sends notifications when unread counts increase.

**Tech Stack:** Go, cobra CLI, cmux CLI, tmux CLI, SQLite

## Global Constraints

- Go binary — no external dependencies beyond cmux/tmux CLI tools (which are optional)
- All timestamps ISO 8601 UTC
- `--json` flag on all new commands
- Follow existing cobra command patterns in `cmd/`
- Follow existing test patterns using `testutil.testDB(t)` and `db.seedSession()`
- Tests must pass: `go test ./...`
- Skills use `//go:embed` via `embedded.go` glob patterns

---

### Task 1: Terminal Backend Interface and Implementations

**Files:**
- Create: `terminal/backend.go`
- Create: `terminal/cmux.go`
- Create: `terminal/tmux.go`
- Create: `terminal/terminal_test.go`

**Interfaces:**
- Consumes: nothing (foundational package)
- Produces:
  - `terminal.Backend` interface: `Capture(terminalID string, lines int) (string, error)`, `Notify(terminalID string, title, body string) error`, `Flash(terminalID string) error`, `Bell(terminalID string) error`
  - `terminal.Detect() (backendType string, terminalID string)`
  - `terminal.NewBackend(backendType string) (Backend, error)`

- [ ] **Step 1: Write tests for Detect()**

Create `terminal/terminal_test.go`:

```go
package terminal

import (
	"os"
	"testing"
)

func TestDetectCmux(t *testing.T) {
	os.Setenv("CMUX_SURFACE_ID", "test-surface-uuid")
	defer os.Unsetenv("CMUX_SURFACE_ID")
	os.Unsetenv("TMUX")

	backendType, terminalID := Detect()
	if backendType != "cmux" {
		t.Errorf("expected backendType 'cmux', got %q", backendType)
	}
	if terminalID != "test-surface-uuid" {
		t.Errorf("expected terminalID 'test-surface-uuid', got %q", terminalID)
	}
}

func TestDetectTmux(t *testing.T) {
	os.Unsetenv("CMUX_SURFACE_ID")
	os.Setenv("TMUX", "/tmp/tmux-501/default,12345,0")
	defer os.Unsetenv("TMUX")

	backendType, _ := Detect()
	if backendType != "tmux" {
		t.Errorf("expected backendType 'tmux', got %q", backendType)
	}
	// terminalID depends on tmux being available, so we only check type here
}

func TestDetectNone(t *testing.T) {
	os.Unsetenv("CMUX_SURFACE_ID")
	os.Unsetenv("TMUX")

	backendType, terminalID := Detect()
	if backendType != "" {
		t.Errorf("expected empty backendType, got %q", backendType)
	}
	if terminalID != "" {
		t.Errorf("expected empty terminalID, got %q", terminalID)
	}
}

func TestDetectCmuxPriority(t *testing.T) {
	os.Setenv("CMUX_SURFACE_ID", "test-surface-uuid")
	os.Setenv("TMUX", "/tmp/tmux-501/default,12345,0")
	defer os.Unsetenv("CMUX_SURFACE_ID")
	defer os.Unsetenv("TMUX")

	backendType, _ := Detect()
	if backendType != "cmux" {
		t.Errorf("expected cmux to take priority, got %q", backendType)
	}
}

func TestNewBackend(t *testing.T) {
	tests := []struct {
		name        string
		backendType string
		wantErr     bool
	}{
		{"cmux", "cmux", false},
		{"tmux", "tmux", false},
		{"unknown", "unknown", true},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := NewBackend(tt.backendType)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if b == nil {
				t.Error("expected non-nil backend")
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./terminal/ -v`
Expected: compilation errors — package and types don't exist yet.

- [ ] **Step 3: Implement backend.go**

Create `terminal/backend.go`:

```go
package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Backend defines the interface for interacting with a terminal environment.
type Backend interface {
	Capture(terminalID string, lines int) (string, error)
	Notify(terminalID string, title, body string) error
	Flash(terminalID string) error
	Bell(terminalID string) error
}

// Detect checks the current environment and returns the terminal backend type
// and terminal ID. Checks cmux first, then tmux.
func Detect() (backendType string, terminalID string) {
	if surfaceID := os.Getenv("CMUX_SURFACE_ID"); surfaceID != "" {
		return "cmux", surfaceID
	}

	if os.Getenv("TMUX") != "" {
		out, err := exec.Command("tmux", "display-message", "-p", "#{pane_id}").Output()
		if err == nil {
			paneID := strings.TrimSpace(string(out))
			if paneID != "" {
				return "tmux", paneID
			}
		}
	}

	return "", ""
}

// NewBackend returns a Backend implementation for the given type.
func NewBackend(backendType string) (Backend, error) {
	switch backendType {
	case "cmux":
		return &CmuxBackend{}, nil
	case "tmux":
		return &TmuxBackend{}, nil
	default:
		return nil, fmt.Errorf("unsupported terminal backend: %q", backendType)
	}
}
```

- [ ] **Step 4: Implement cmux.go**

Create `terminal/cmux.go`:

```go
package terminal

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// CmuxBackend implements Backend for cmux terminals.
type CmuxBackend struct{}

func (b *CmuxBackend) Capture(terminalID string, lines int) (string, error) {
	args := []string{"capture-pane", "--surface", terminalID}
	if lines > 0 {
		args = append(args, "--lines", strconv.Itoa(lines))
	}
	out, err := exec.Command("cmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("cmux capture-pane failed: %w", err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func (b *CmuxBackend) Notify(terminalID string, title, body string) error {
	args := []string{"notify", "--surface", terminalID, "--title", title}
	if body != "" {
		args = append(args, "--body", body)
	}
	if err := exec.Command("cmux", args...).Run(); err != nil {
		return fmt.Errorf("cmux notify failed: %w", err)
	}
	return nil
}

func (b *CmuxBackend) Flash(terminalID string) error {
	if err := exec.Command("cmux", "trigger-flash", "--surface", terminalID).Run(); err != nil {
		return fmt.Errorf("cmux trigger-flash failed: %w", err)
	}
	return nil
}

func (b *CmuxBackend) Bell(terminalID string) error {
	return nil // cmux has better notification primitives
}
```

- [ ] **Step 5: Implement tmux.go**

Create `terminal/tmux.go`:

```go
package terminal

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// TmuxBackend implements Backend for tmux terminals.
type TmuxBackend struct{}

func (b *TmuxBackend) Capture(terminalID string, lines int) (string, error) {
	args := []string{"capture-pane", "-t", terminalID, "-p"}
	if lines > 0 {
		args = append(args, "-S", "-"+strconv.Itoa(lines))
	}
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane failed: %w", err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func (b *TmuxBackend) Notify(terminalID string, title, body string) error {
	return nil // tmux has no native notification mechanism
}

func (b *TmuxBackend) Flash(terminalID string) error {
	return nil // tmux has no flash equivalent
}

func (b *TmuxBackend) Bell(terminalID string) error {
	err := exec.Command("tmux", "send-keys", "-t", terminalID, "printf", "'\\a'", "Enter").Run()
	if err != nil {
		return fmt.Errorf("tmux bell failed: %w", err)
	}
	return nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./terminal/ -v`
Expected: all tests pass.

- [ ] **Step 7: Run full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 8: Commit**

```bash
git add terminal/backend.go terminal/cmux.go terminal/tmux.go terminal/terminal_test.go
git commit --signoff -m "feat: terminal backend abstraction with cmux and tmux implementations"
```

---

### Task 2: Schema Migration and Session Struct Changes

**Files:**
- Modify: `db/schema.sql`
- Modify: `db/db.go`
- Modify: `db/sessions.go`
- Modify: `db/sessions_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `Session.TerminalType` field (string)
  - `Session.TerminalID` field (string)
  - Updated `UpsertSession` that stores/updates terminal fields

- [ ] **Step 1: Write tests for terminal fields on Session**

Add to `db/sessions_test.go`:

```go
func TestSessionTerminalFields(t *testing.T) {
	d := testDB(t)

	now := "2026-07-02T10:00:00Z"
	err := d.UpsertSession(Session{
		SessionID:    "terminal-test",
		Harness:      "claude-code",
		Repo:         "test/repo",
		Branch:       "main",
		PID:          1234,
		Status:       "active",
		InboxMode:    "manual",
		LastActive:   now,
		RegisteredAt: now,
		JSONLPath:    "/tmp/test.jsonl",
		TerminalType: "cmux",
		TerminalID:   "surface-uuid-123",
	})
	if err != nil {
		t.Fatalf("UpsertSession failed: %v", err)
	}

	session, err := d.GetSession("terminal-test")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if session.TerminalType != "cmux" {
		t.Errorf("expected TerminalType 'cmux', got %q", session.TerminalType)
	}
	if session.TerminalID != "surface-uuid-123" {
		t.Errorf("expected TerminalID 'surface-uuid-123', got %q", session.TerminalID)
	}

	// Upsert should update terminal fields on re-registration
	err = d.UpsertSession(Session{
		SessionID:    "terminal-test",
		Harness:      "claude-code",
		Repo:         "test/repo",
		Branch:       "main",
		PID:          5678,
		Status:       "active",
		InboxMode:    "manual",
		LastActive:   now,
		RegisteredAt: now,
		JSONLPath:    "/tmp/test.jsonl",
		TerminalType: "tmux",
		TerminalID:   "%42",
	})
	if err != nil {
		t.Fatalf("UpsertSession (update) failed: %v", err)
	}

	session, err = d.GetSession("terminal-test")
	if err != nil {
		t.Fatalf("GetSession (after update) failed: %v", err)
	}
	if session.TerminalType != "tmux" {
		t.Errorf("expected TerminalType 'tmux', got %q", session.TerminalType)
	}
	if session.TerminalID != "%42" {
		t.Errorf("expected TerminalID '%%42', got %q", session.TerminalID)
	}
}

func TestSessionTerminalFieldsEmpty(t *testing.T) {
	d := testDB(t)

	now := "2026-07-02T10:00:00Z"
	err := d.UpsertSession(Session{
		SessionID:    "no-terminal-test",
		Harness:      "claude-code",
		Repo:         "test/repo",
		Branch:       "main",
		PID:          1234,
		Status:       "active",
		InboxMode:    "manual",
		LastActive:   now,
		RegisteredAt: now,
		JSONLPath:    "/tmp/test.jsonl",
	})
	if err != nil {
		t.Fatalf("UpsertSession failed: %v", err)
	}

	session, err := d.GetSession("no-terminal-test")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if session.TerminalType != "" {
		t.Errorf("expected empty TerminalType, got %q", session.TerminalType)
	}
	if session.TerminalID != "" {
		t.Errorf("expected empty TerminalID, got %q", session.TerminalID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./db/ -run TestSessionTerminal -v`
Expected: compilation error — `TerminalType` and `TerminalID` fields don't exist on `Session`.

- [ ] **Step 3: Update schema.sql**

In `db/schema.sql`, modify the `sessions` table to add the two columns:

```sql
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
    role TEXT,
    terminal_type TEXT,
    terminal_id TEXT,
    last_active TEXT NOT NULL,
    registered_at TEXT NOT NULL,
    jsonl_path TEXT NOT NULL
);
```

- [ ] **Step 4: Add migration in db.go**

In `db/db.go`, restore the `runMigrations` function (removed in commit `0737f59`) and add the terminal column migrations. Call it from `Open()` after schema application:

```go
// In Open(), after applySchema:
if err := runMigrations(conn); err != nil {
    conn.Close()
    return nil, fmt.Errorf("failed to run migrations: %w", err)
}
```

```go
func runMigrations(conn *sql.DB) error {
	// Add human_seen_ts column to session_cursors if it doesn't exist
	if err := addColumnIfMissing(conn, "session_cursors", "human_seen_ts", "TEXT"); err != nil {
		return err
	}
	// Add terminal_type column to sessions if it doesn't exist
	if err := addColumnIfMissing(conn, "sessions", "terminal_type", "TEXT"); err != nil {
		return err
	}
	// Add terminal_id column to sessions if it doesn't exist
	if err := addColumnIfMissing(conn, "sessions", "terminal_id", "TEXT"); err != nil {
		return err
	}
	return nil
}

func addColumnIfMissing(conn *sql.DB, table, column, colType string) error {
	var count int
	err := conn.QueryRow(
		fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = '%s'`, table, column),
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check for %s.%s column: %w", table, column, err)
	}
	if count == 0 {
		if _, err := conn.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, colType)); err != nil {
			return fmt.Errorf("failed to add %s.%s column: %w", table, column, err)
		}
	}
	return nil
}
```

- [ ] **Step 5: Update Session struct and queries in sessions.go**

Add fields to `Session`:

```go
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
	Role             string
	TerminalType     string
	TerminalID       string
	LastActive       string
	RegisteredAt     string
	JSONLPath        string
}
```

Update `UpsertSession` query to include the new columns. The ON CONFLICT clause should update terminal fields from the excluded (new) values:

```sql
INSERT INTO sessions (
    session_id, harness, repo, branch, session_name, pid, status,
    inbox_mode, auto_poll_interval, role, terminal_type, terminal_id,
    last_active, registered_at, jsonl_path
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
    harness = excluded.harness,
    repo = excluded.repo,
    branch = excluded.branch,
    session_name = excluded.session_name,
    pid = excluded.pid,
    status = excluded.status,
    inbox_mode = sessions.inbox_mode,
    auto_poll_interval = COALESCE(excluded.auto_poll_interval, sessions.auto_poll_interval),
    role = sessions.role,
    terminal_type = excluded.terminal_type,
    terminal_id = excluded.terminal_id,
    last_active = excluded.last_active,
    registered_at = excluded.registered_at,
    jsonl_path = excluded.jsonl_path
```

Update the Exec args to include `s.TerminalType, s.TerminalID`.

Update `GetSession` SELECT and Scan to include:
```sql
COALESCE(terminal_type, '') as terminal_type,
COALESCE(terminal_id, '') as terminal_id,
```

Update `ListSessions` the same way — add both columns to SELECT and Scan.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./db/ -v`
Expected: all tests pass, including the new terminal field tests.

- [ ] **Step 7: Run full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 8: Commit**

```bash
git add db/schema.sql db/db.go db/sessions.go db/sessions_test.go
git commit --signoff -m "feat: add terminal_type and terminal_id columns to sessions table"
```

---

### Task 3: `handler register` Terminal Flags and Hook Updates

**Files:**
- Modify: `cmd/register.go`
- Modify: `hooks/common.sh`

**Interfaces:**
- Consumes: `Session.TerminalType`, `Session.TerminalID` from Task 2
- Produces:
  - `handler register --terminal-type <type> --terminal-id <id>` flags
  - Updated `discover_and_register()` shell function that detects terminal env

- [ ] **Step 1: Add flags to register command**

In `cmd/register.go`, add two new flag variables and register them:

```go
var (
	regSessionID    string
	regBranch       string
	regRepo         string
	regPID          int
	regJSONLPath    string
	regTerminalType string
	regTerminalID   string
)
```

In `init()`:
```go
registerCmd.Flags().StringVar(&regTerminalType, "terminal-type", "", "terminal backend type (cmux, tmux)")
registerCmd.Flags().StringVar(&regTerminalID, "terminal-id", "", "terminal surface/pane ID")
```

In `runRegister`, pass the values to `UpsertSession`:
```go
err = d.UpsertSession(db.Session{
    // ... existing fields ...
    TerminalType: regTerminalType,
    TerminalID:   regTerminalID,
})
```

- [ ] **Step 2: Update hooks/common.sh**

In the `discover_and_register()` function, after discovering SESSION_ID, BRANCH, and REPO, add terminal detection:

```bash
# Detect terminal environment
TERMINAL_TYPE=""
TERMINAL_ID=""
if [ -n "${CMUX_SURFACE_ID:-}" ]; then
    TERMINAL_TYPE="cmux"
    TERMINAL_ID="$CMUX_SURFACE_ID"
elif [ -n "${TMUX:-}" ] && [ "${HANDLER_MANAGED:-}" = "1" ]; then
    TERMINAL_TYPE="tmux"
    TERMINAL_ID=$(tmux display-message -p '#{pane_id}' 2>/dev/null || true)
fi

TERMINAL_FLAGS=""
if [ -n "$TERMINAL_TYPE" ]; then
    TERMINAL_FLAGS="--terminal-type $TERMINAL_TYPE --terminal-id $TERMINAL_ID"

    # If tmux and handler-managed, update pane title to handler:<session_id>
    if [ "$TERMINAL_TYPE" = "tmux" ]; then
        tmux select-pane -T "handler:${SESSION_ID}" 2>/dev/null || true
    fi
fi
```

Then pass `$TERMINAL_FLAGS` to the `handler register` call:

```bash
handler register \
    --session-id "$SESSION_ID" \
    --branch "$BRANCH" \
    --repo "$REPO" \
    --pid "$CLAUDE_PID" \
    --jsonl-path "$JSONL_PATH" \
    $TERMINAL_FLAGS
```

- [ ] **Step 3: Test manually**

Run: `handler register --help` and verify the new flags appear.
Then run: `echo $CMUX_SURFACE_ID` to confirm the env var is available, and manually test:
```bash
handler register --session-id test-peek --branch main --repo test/repo --pid $$ --jsonl-path /tmp/test.jsonl --terminal-type cmux --terminal-id "$CMUX_SURFACE_ID"
handler status --json | grep -A2 terminal
handler query "DELETE FROM sessions WHERE session_id = 'test-peek'"
```

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/register.go hooks/common.sh
git commit --signoff -m "feat: detect and store terminal environment at registration time"
```

---

### Task 4: `handler peek` Command

**Files:**
- Create: `cmd/peek.go`

**Interfaces:**
- Consumes: `terminal.NewBackend()`, `terminal.Backend.Capture()` from Task 1; `Session.TerminalType`, `Session.TerminalID` from Task 2
- Produces: `handler peek --session <id> [--lines <n>] [--json]`

- [ ] **Step 1: Implement cmd/peek.go**

```go
package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mturley/agent-handler/discover"
	"github.com/mturley/agent-handler/terminal"
	"github.com/spf13/cobra"
)

var peekCmd = &cobra.Command{
	Use:   "peek",
	Short: "Capture terminal content for a session",
	RunE:  runPeek,
}

var (
	peekSessionID string
	peekLines     int
)

func init() {
	peekCmd.GroupID = "agent"
	rootCmd.AddCommand(peekCmd)
	peekCmd.Flags().StringVar(&peekSessionID, "session", "", "session ID, name, or branch")
	peekCmd.Flags().IntVar(&peekLines, "lines", 0, "limit capture to last N lines (0 = full pane)")
	peekCmd.MarkFlagRequired("session")
}

func runPeek(cmd *cobra.Command, args []string) error {
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	session, err := d.GetSession(peekSessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	if session == nil {
		return fmt.Errorf("session %q not found", peekSessionID)
	}

	if session.TerminalType == "" || session.TerminalID == "" {
		return fmt.Errorf("session is not peekable (not started via handler claude or not in a supported terminal)")
	}

	if !discover.IsProcessAlive(session.PID) {
		return fmt.Errorf("session process is not running (PID %d not found)", session.PID)
	}

	backend, err := terminal.NewBackend(session.TerminalType)
	if err != nil {
		return fmt.Errorf("failed to create terminal backend: %w", err)
	}

	content, err := backend.Capture(session.TerminalID, peekLines)
	if err != nil {
		return fmt.Errorf("failed to capture terminal: %w", err)
	}

	if jsonOutput {
		output := map[string]interface{}{
			"session_id":    session.SessionID,
			"session_name":  session.SessionName,
			"terminal_type": session.TerminalType,
			"captured_at":   time.Now().UTC().Format(time.RFC3339),
			"content":       content,
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Print(content)
		if len(content) > 0 && content[len(content)-1] != '\n' {
			fmt.Println()
		}
	}

	return nil
}
```

- [ ] **Step 2: Build and test the help output**

Run: `make build && ./bin/handler peek --help`
Expected: shows usage with `--session`, `--lines`, and `--json` flags.

- [ ] **Step 3: Test with a live session (manual)**

Find a peekable session and test:
```bash
handler status --json | python3 -c "import sys,json; [print(s['session_id'], s.get('terminal_type','')) for s in json.load(sys.stdin)]"
```

If a session with terminal info exists:
```bash
./bin/handler peek --session <id>
./bin/handler peek --session <id> --json
./bin/handler peek --session <id> --lines 10
```

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/peek.go
git commit --signoff -m "feat: handler peek command for terminal inspection"
```

---

### Task 5: `handler claude` Wrapper Command

**Files:**
- Create: `cmd/claude.go`

**Interfaces:**
- Consumes: `terminal.Detect()` from Task 1
- Produces: `handler claude [claude-args...]` command

- [ ] **Step 1: Implement cmd/claude.go**

```go
package cmd

import (
	"bufio"
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/mturley/agent-handler/terminal"
	"github.com/spf13/cobra"
)

var claudeCmd = &cobra.Command{
	Use:                "claude [claude-args...]",
	Short:              "Start a peekable Claude session",
	Long:               "Wrapper that ensures the Claude session is peekable via handler peek, then passes all arguments through to claude.",
	DisableFlagParsing: true,
	RunE:               runClaude,
}

func init() {
	claudeCmd.GroupID = "human"
	rootCmd.AddCommand(claudeCmd)
}

func runClaude(cmd *cobra.Command, args []string) error {
	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude not found on PATH: %w", err)
	}

	backendType, _ := terminal.Detect()

	switch backendType {
	case "cmux":
		os.Setenv("HANDLER_MANAGED", "1")
		return syscall.Exec(claudeBin, append([]string{"claude"}, args...), os.Environ())

	case "tmux":
		// Set pane title to handler:pending
		exec.Command("tmux", "select-pane", "-T", "handler:pending").Run()
		os.Setenv("HANDLER_MANAGED", "1")
		return syscall.Exec(claudeBin, append([]string{"claude"}, args...), os.Environ())

	default:
		// Outside both — prompt user
		fmt.Println("No tmux or cmux detected. Start a tmux session for peek support? [y/N]")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))

		if answer == "y" || answer == "yes" {
			suffix, _ := rand.Int(rand.Reader, big.NewInt(99999))
			sessionName := fmt.Sprintf("handler-%05d", suffix.Int64())
			// Build the claude command string for tmux
			claudeArgs := strings.Join(args, " ")
			claudeCommand := claudeBin
			if claudeArgs != "" {
				claudeCommand = claudeBin + " " + claudeArgs
			}
			tmuxCmd := exec.Command("tmux", "new-session", "-s", sessionName,
				"-e", "HANDLER_MANAGED=1",
				claudeCommand)
			tmuxCmd.Stdin = os.Stdin
			tmuxCmd.Stdout = os.Stdout
			tmuxCmd.Stderr = os.Stderr
			// Set the pane title after session creation
			exec.Command("tmux", "select-pane", "-t", sessionName, "-T", "handler:pending").Run()
			return tmuxCmd.Run()
		}

		// User declined — run without peek support
		return syscall.Exec(claudeBin, append([]string{"claude"}, args...), os.Environ())
	}
}
```

- [ ] **Step 2: Update root.go PersistentPreRunE**

In `cmd/root.go`, add `"claude"` to the skip list so `handler claude` works without the DB being set up:

```go
if cmd.Name() == "setup" || cmd.Name() == "help" || cmd.Name() == "completion" || cmd.Name() == "claude" {
    return nil
}
```

- [ ] **Step 3: Build and test help output**

Run: `make build && ./bin/handler claude --help`
Expected: shows usage indicating args are passed through to claude.

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/claude.go cmd/root.go
git commit --signoff -m "feat: handler claude wrapper for peekable sessions"
```

---

### Task 6: `handler status` Peekable Indicator

**Files:**
- Modify: `cmd/status.go`

**Interfaces:**
- Consumes: `Session.TerminalType` from Task 2
- Produces: `peekable` field in status output

- [ ] **Step 1: Update the sessionStatus struct and JSON output**

In `cmd/status.go`, add fields to `sessionStatus`:

```go
type sessionStatus struct {
	SessionID    string         `json:"session_id"`
	SessionName  string         `json:"session_name"`
	Branch       string         `json:"branch"`
	PID          int            `json:"pid"`
	Status       string         `json:"status"`
	DisplayState string         `json:"display_state"`
	InboxMode    string         `json:"inbox_mode"`
	Peekable     bool           `json:"peekable"`
	TerminalType string         `json:"terminal_type,omitempty"`
	UnreadCount  int            `json:"unread_count"`
	LastActive   string         `json:"last_active"`
	Breakdown    map[string]int `json:"unread_breakdown,omitempty"`
}
```

When populating `statuses`, set:
```go
Peekable:     s.TerminalType != "",
TerminalType: s.TerminalType,
```

- [ ] **Step 2: Update the text output**

In the text output loop, after the display state indicator, add a peekable indicator:

```go
peekableStr := ""
if st.Peekable {
	peekableStr = fmt.Sprintf(" %s👁%s", dim, reset)
}

fmt.Printf("  %s%s%s %s%s%s%s\n", bold, name, reset, stateColor, st.DisplayState, reset, peekableStr)
```

- [ ] **Step 3: Build and test**

Run: `make build && ./bin/handler status`
Verify the peekable indicator appears for sessions with terminal info.

Run: `./bin/handler status --json | python3 -m json.tool | grep peekable`
Verify the `peekable` field appears in JSON output.

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/status.go
git commit --signoff -m "feat: show peekable indicator in handler status"
```

---

### Task 7: Statusline Notifications via Temp File

**Files:**
- Modify: `hooks/statusline.sh`
- Modify: `cmd/statusline.go`

**Interfaces:**
- Consumes: `Session.TerminalType`, `Session.TerminalID` from Task 2; `terminal.NewBackend()`, `terminal.Backend.Notify()`, `terminal.Backend.Flash()` from Task 1
- Produces: notifications sent to peekable sessions when unread count increases

- [ ] **Step 1: Add a `handler notify` subcommand for the statusline hook to call**

The statusline hook is a shell script, so it can't call Go functions directly. Add a small `cmd/notify.go` command that the hook can shell out to:

```go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/terminal"
	"github.com/spf13/cobra"
)

var notifyCmd = &cobra.Command{
	Use:    "notify",
	Short:  "Send notification if unread count increased (used by statusline hook)",
	Hidden: true,
	RunE:   runNotify,
}

var (
	notifySessionID string
	notifyCount     int
	notifyMessage   string
)

func init() {
	rootCmd.AddCommand(notifyCmd)
	notifyCmd.Flags().StringVar(&notifySessionID, "session", "", "session ID")
	notifyCmd.Flags().IntVar(&notifyCount, "count", 0, "current unread count")
	notifyCmd.Flags().StringVar(&notifyMessage, "message", "", "notification body")
	notifyCmd.MarkFlagRequired("session")
	notifyCmd.MarkFlagRequired("count")
}

func runNotify(cmd *cobra.Command, args []string) error {
	if notifyCount == 0 {
		// Clean up temp file if count dropped to 0
		countFile := notifiedCountPath(notifySessionID)
		os.Remove(countFile)
		return nil
	}

	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	session, err := d.GetSession(notifySessionID)
	if err != nil || session == nil {
		return nil
	}

	if session.TerminalType == "" || session.TerminalID == "" {
		return nil
	}

	// Check cached count
	countFile := notifiedCountPath(notifySessionID)
	cachedCount := 0
	if data, err := os.ReadFile(countFile); err == nil {
		cachedCount, _ = strconv.Atoi(string(data))
	}

	if notifyCount <= cachedCount {
		return nil
	}

	// Send notification
	backend, err := terminal.NewBackend(session.TerminalType)
	if err != nil {
		return nil // silently fail — don't break the statusline
	}

	title := "handler"
	body := notifyMessage
	if body == "" {
		body = fmt.Sprintf("%d unread event(s)", notifyCount)
	}

	backend.Notify(session.TerminalID, title, body)
	backend.Flash(session.TerminalID)

	// Update cached count
	os.MkdirAll(filepath.Dir(countFile), 0755)
	os.WriteFile(countFile, []byte(strconv.Itoa(notifyCount)), 0644)

	return nil
}

func notifiedCountPath(sessionID string) string {
	return filepath.Join(db.HandlerHome(), "sessions", sessionID+".notified_count")
}
```

- [ ] **Step 2: Update hooks/statusline.sh**

After the `handler statusline` call outputs, add a notification check. Append to the end of the script, before the final `echo "$OUTPUT"`:

```bash
# Extract unread count from output for notification
if [ -n "$SESSION_ID" ]; then
    UNREAD_COUNT=$(echo "$OUTPUT" | grep -oP '● \K\d+(?= unread)' 2>/dev/null || echo "0")
    if [ "$UNREAD_COUNT" -gt 0 ] 2>/dev/null; then
        # Build notification message from the output
        NOTIFY_MSG=$(echo "$OUTPUT" | head -1 | sed 's/.*● //' | sed 's/\x1b\[[0-9;]*m//g')
        handler notify --session "$SESSION_ID" --count "$UNREAD_COUNT" --message "$NOTIFY_MSG" 2>/dev/null &
    else
        handler notify --session "$SESSION_ID" --count 0 2>/dev/null &
    fi
fi
```

- [ ] **Step 3: Build and test manually**

Run: `make build`
Test the notify command directly:
```bash
./bin/handler notify --session "$SESSION_ID" --count 5 --message "5 unread (3 pr_comment, 2 ci_fail)"
# Should trigger a cmux notification if in cmux
./bin/handler notify --session "$SESSION_ID" --count 0
# Should clean up the temp file
ls ~/.agent-handler/sessions/*.notified_count 2>/dev/null
```

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/notify.go hooks/statusline.sh
git commit --signoff -m "feat: terminal notifications when unread count increases"
```

---

### Task 8: Skill Updates

**Files:**
- Modify: `skills/handler/SKILL.md`
- Modify: `skills/using-handler/SKILL.md`

**Interfaces:**
- Consumes: `handler peek` from Task 4, `handler claude` from Task 5
- Produces: updated skill documentation

- [ ] **Step 1: Update /handler skill**

In `skills/handler/SKILL.md`, add a peek section after the "What the user can ask" section:

```markdown
## Peeking at sessions

When you need to understand what a session is doing (stuck, waiting for approval, idle):

1. Check `handler status --json` — look for peekable sessions (`"peekable": true`)
2. For each session you want to inspect, spawn a subagent
3. The subagent runs `handler peek --session <id> --json` and interprets the raw terminal content
4. The subagent returns a 1-2 sentence summary: what the session appears to be doing

**Important:** Always use subagents for peek — raw captures can be hundreds of lines and will flood your context. Each subagent distills the capture to a short summary.

**When to peek:**
- Sessions that appear stuck (active but no recent heartbeat)
- Sessions that are blocked
- When the user asks "what's session X doing?"
- During triage, for sessions with unread events they haven't processed
```

Add peek to the "What the user can ask" list:

```markdown
- "What is session X doing?" → spawn subagent with `handler peek --session <id> --json`
- "Check on all sessions" → peek at each peekable session via subagents, summarize
```

- [ ] **Step 2: Update /using-handler skill**

In `skills/using-handler/SKILL.md`, add to the "Key commands" section:

```markdown
- `handler claude` — start Claude in a peekable terminal (use instead of bare `claude`)
- `handler peek --session <id>` — capture terminal content of another session
```

- [ ] **Step 3: Commit**

```bash
git add skills/handler/SKILL.md skills/using-handler/SKILL.md
git commit --signoff -m "docs: update handler and using-handler skills for peek support"
```

---

### Task 9: Update setup and uninstall

**Files:**
- Modify: `cmd/uninstall.go`

**Interfaces:**
- Consumes: nothing new
- Produces: updated uninstall cleanup

- [ ] **Step 1: Verify embedded.go globs**

Check that the existing `//go:embed` patterns in `embedded.go` already cover the new/modified skills:

```go
//go:embed skills/*/SKILL.md
```

This glob already picks up all skills. No changes needed to `embedded.go`.

- [ ] **Step 2: Verify uninstall.go skillNames**

The `skillNames` slice in `cmd/uninstall.go` should already include `"handler"` and `"using-handler"`. Verify — no new skills are created in this phase, only existing ones are modified. No changes needed.

- [ ] **Step 3: Build and verify setup extracts correctly**

Run: `make build && ./bin/handler setup`
Verify the updated skill files are extracted to `~/.agent-handler/skills/`.

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 5: Commit (if any changes were needed)**

Only commit if changes were required. If verification passed with no changes, skip this step.
