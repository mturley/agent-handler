# Phase 4: Session Inspection (Peek) — Design Spec

## Overview

Phase 4 adds the ability to inspect live Claude sessions from other sessions (primarily the handler) and to receive terminal notifications when new events arrive. It introduces a terminal backend abstraction that supports both cmux (primary) and tmux (fallback), a `handler claude` wrapper for ensuring sessions are peekable, and updates to the handler skill for subagent-based session interpretation.

---

## Terminal Backend Abstraction

A new `terminal/` package defines a backend interface for interacting with the terminal environment a session is running in.

### Interface

```go
type Backend interface {
    Capture(terminalID string, lines int) (string, error)
    Notify(terminalID string, title, body string) error
    Flash(terminalID string) error
    Bell(terminalID string) error
}
```

- `Capture` — read the current visible content of a terminal surface/pane
- `Notify` — send a structured notification to the terminal
- `Flash` — visual attention indicator
- `Bell` — terminal bell character (`\a`)

### Implementations

**cmux** (`terminal/cmux.go`):
- `Capture` → `cmux capture-pane --surface <id> [--lines <n>]`
- `Notify` → `cmux notify --surface <id> --title <title> --body <body>`
- `Flash` → `cmux trigger-flash --surface <id>`
- `Bell` → no-op (cmux has better notification primitives)

**tmux** (`terminal/tmux.go`):
- `Capture` → `tmux capture-pane -t <id> -p [-S -<lines>]`
- `Notify` → no-op (tmux has no native notification mechanism)
- `Flash` → no-op
- `Bell` → `tmux send-keys -t <id> ''` followed by `printf '\a'` via `tmux send-keys`

### Detection

`terminal.Detect() (backendType string, terminalID string)`:
1. If `CMUX_SURFACE_ID` is set → return `"cmux"`, the surface UUID
2. If `TMUX` is set → return `"tmux"`, the current pane target from `tmux display-message -p '#{pane_id}'`
3. Otherwise → return `""`, `""`

`terminal.NewBackend(backendType string) Backend` — factory that returns the appropriate implementation.

---

## Schema Change

Two new nullable columns on the `sessions` table:

| Column | Type | Notes |
|--------|------|-------|
| `terminal_type` | TEXT, nullable | `"cmux"`, `"tmux"`, or NULL |
| `terminal_id` | TEXT, nullable | cmux surface UUID or tmux pane target |

A migration in `db.Open()` adds these columns if they don't exist (same pattern used for `human_seen_ts`).

The `Session` struct in `db/sessions.go` gains corresponding fields. `UpsertSession` stores these on insert and updates them on conflict (terminal info can change across resumes).

`handler status` displays a `peekable` indicator for sessions with a non-empty `terminal_type`.

---

## `handler claude` Command

A new `cmd/claude.go` subcommand. An exec wrapper that ensures the claude session is peekable, then replaces itself with the `claude` process.

**Command group:** `human`.

**Usage:** `handler claude [claude-args...]`

All arguments are passed through to `claude` verbatim. No `--` separator needed — `handler claude` has no flags of its own. Examples:
- `handler claude` — start a new session
- `handler claude --resume abc-123` — resume a session
- `handler claude --print "explain this code"` — one-shot print mode

### Behavior by environment

**In cmux** (`CMUX_SURFACE_ID` is set):
1. Set `HANDLER_MANAGED=1` in the environment
2. `exec claude "$@"`

cmux already provides the surface ID. The wrapper is a thin passthrough that just marks the session as handler-managed.

**In tmux** (`TMUX` is set):
1. Get the current pane target
2. Set the pane title to `handler:pending` via `tmux select-pane -T handler:pending`
3. Set `HANDLER_MANAGED=1` in the environment
4. `exec claude "$@"`

The SessionStart hook will update the pane title to `handler:<session_id>` after registration.

**Outside both:**
1. Print: `"No tmux or cmux detected. Start a tmux session for peek support? [y/N]"`
2. If yes: create a tmux session with `tmux new-session -s handler-<random_suffix>`, run claude inside it
3. If no: set `HANDLER_MANAGED=0`, `exec claude "$@"` (session will not be peekable)

### SessionStart hook update

In `hooks/session_start.sh` (via `hooks/common.sh`), after discovering the session ID:
1. If `HANDLER_MANAGED=1`:
   - Run `terminal.Detect()` logic (check `CMUX_SURFACE_ID`, then `TMUX`)
   - Pass `--terminal-type` and `--terminal-id` to `handler register`
   - If tmux: update pane title from `handler:pending` to `handler:<session_id>`
2. If `HANDLER_MANAGED` is not set:
   - Still check for `CMUX_SURFACE_ID` — if present, store it anyway (cmux sessions are always implicitly peekable, even without the wrapper)
   - Do not check tmux (without the wrapper, there's no `handler:*` title convention to rely on)

### `handler register` update

New optional flags: `--terminal-type <type>` and `--terminal-id <id>`. Stored in the session record on upsert.

---

## `handler peek` Command

Captures terminal content for a session.

**Command group:** `agent` (primarily used by the handler session, also usable by humans).

**Usage:** `handler peek --session <id>`

### Flow

1. Resolve the session (by ID, name, or branch)
2. Check `terminal_type` and `terminal_id` — if empty, error: `"Session is not peekable (not started via handler claude or not in a supported terminal)"`
3. Check process liveness via PID — if dead, report: `"Session process is not running (PID <pid> not found)"`
4. Instantiate the appropriate `terminal.Backend`
5. Call `backend.Capture(terminalID, lines)`
6. Output the result

### Flags

| Flag | Description |
|------|-------------|
| `--session <id>` | Session ID, name, or branch (required) |
| `--lines <n>` | Limit capture to last N lines (default: full visible pane) |
| `--json` | Structured output (see below) |

### JSON output

```json
{
  "session_id": "abc-123",
  "session_name": "auth-feature",
  "terminal_type": "cmux",
  "captured_at": "2026-07-02T14:30:00Z",
  "content": "... raw terminal text ..."
}
```

---

## Terminal Notifications

The statusline hook sends notifications when the unread count increases for a peekable session.

### State tracking

A temp file at `~/.agent-handler/sessions/<session_id>.notified_count` stores the last unread count that triggered a notification. This is ephemeral — deleted when unread count drops to 0, automatically stale when the session dies.

### Statusline hook flow (additions)

After computing the unread count (existing logic):

1. If `unreadCount > 0` AND session has a `terminal_id`:
   - Read cached count from temp file (default 0 if missing)
   - If `unreadCount > cachedCount`: send notification via backend, write new count to file
   - Otherwise: skip
2. If `unreadCount == 0`: delete the temp file if it exists

The statusline hook remains read-only with respect to the database. Only the temp file is written.

### Notification content

The notification includes a summary like `"3 unread events (2 pr_comment, 1 ci_fail)"` — the same breakdown already computed by the statusline.

---

## `/handler` Skill Updates

The `/handler` skill is updated to teach the handler agent how to use peek for session state awareness.

### Peek via subagents

When the handler agent needs to understand what sessions are doing:

1. Run `handler triage --json` to get session overview
2. For each peekable active session, spawn a subagent
3. Each subagent runs `handler peek --session <id> --json` and interprets the raw terminal content
4. The subagent returns a short summary (1-2 sentences): what the session appears to be doing, whether it's waiting for input/approval, actively working, idle, or showing an error
5. The handler agent collects summaries without raw captures entering its context

This pattern keeps the handler's context window clean — raw pane captures can be hundreds of lines, but each subagent distills it to a sentence or two.

### Updated triage flow

After triage, the handler agent:
- Identifies peekable sessions that appear stuck (active but no recent heartbeat) or blocked
- Peeks at those sessions via subagents to understand why
- Presents summaries: "Session X (feature/auth): waiting for Bash tool approval. Session Y (fix/pagination): idle at prompt for 12 minutes."

### Skill guidance

The skill teaches:
- "Use subagents for peek — don't read raw captures in your own context"
- "Peek is most useful for sessions that appear stuck, blocked, or idle"
- "Combine peek with triage for a complete picture of what's happening"
- "You can peek at a specific session on demand if the user asks about it"

---

## `handler status` Update

Add a `peekable` field to the status output for each session.

**Text output:** Shows `👁 peekable` or `peekable` indicator next to sessions that have a `terminal_type`.

**JSON output:** Adds `"peekable": true/false` and `"terminal_type": "cmux"` (or `"tmux"` or `null`) to each session object.

---

## Files to Create/Modify

**New:**
- `terminal/backend.go` — `Backend` interface, `Detect()`, `NewBackend()` factory
- `terminal/cmux.go` — cmux backend implementation
- `terminal/tmux.go` — tmux backend implementation
- `terminal/terminal_test.go` — tests for detection and backend dispatch
- `cmd/claude.go` — `handler claude` wrapper command
- `cmd/peek.go` — `handler peek` command

**Modified:**
- `db/schema.sql` — add `terminal_type`, `terminal_id` columns to `sessions`
- `db/db.go` — migration to add new columns on existing databases
- `db/sessions.go` — add fields to `Session` struct and queries
- `cmd/register.go` — accept `--terminal-type` and `--terminal-id` flags
- `cmd/status.go` — show peekable indicator
- `cmd/setup.go` — register `handler claude` awareness (if needed)
- `cmd/uninstall.go` — update skill names list if new skills added
- `hooks/common.sh` — detect terminal environment, pass to register
- `hooks/statusline.sh` — notification delta logic with temp file
- `skills/handler/SKILL.md` — peek workflow with subagent guidance
- `skills/using-handler/SKILL.md` — mention `handler claude` and `handler peek`
