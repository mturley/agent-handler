# Phase 3: Handler Session — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a handler session mode with global event visibility, triage, role-based messaging, direct message indicators, and the `/handler` skill.

**Architecture:** Session `role` column drives statusline and skill behavior. `handler log --global` queries all events. `handler triage` aggregates what needs attention. Role-based recipient routing lets sessions message `--to handler`. The `/handler` skill sets up the role, runs triage, and starts a polling loop.

**Tech Stack:** Go, existing cobra/SQLite stack, Claude Code skills.

**Spec:** `docs/superpowers/specs/2026-07-01-phase3-handler-session.md`

## Global Constraints

- All timestamps ISO 8601 UTC
- Event IDs are UUIDs
- `--json` on all new commands for machine-readable output
- `--signoff` on all commits
- Update `skills/using-handler/SKILL.md` when adding commands or capabilities

---

## File Structure

```
New:
  cmd/triage.go                    # handler triage command
  skills/handler/SKILL.md          # /handler skill

Modified:
  db/schema.sql                    # add role column to sessions
  db/sessions.go                   # Role field in Session struct, ConfigureSession
  db/events.go                     # UnreadForSession adds role recipient, DirectCountForSession
  cmd/configure.go                 # --role flag
  cmd/log_cmd.go                   # --global, --since-cursor flags
  cmd/emit.go                      # resolveRecipient handles roles
  cmd/statusline.go                # direct message indicator + handler layout
  cmd/uninstall.go                 # add handler to skillNames
  skills/watching/SKILL.md         # use global when handler
  skills/using-handler/SKILL.md    # mention /handler and --to handler
```

---

## Task 1: Session Role — Schema, Struct, Configure

**Files:**
- Modify: `db/schema.sql`, `db/sessions.go`, `cmd/configure.go`

**Produces:**
- `Session.Role` field (string)
- `db.ConfigureSession` accepts role parameter
- `handler configure --role handler` and `--get role`

- [ ] **Step 1: Add role column to schema.sql**

In `db/schema.sql`, add `role TEXT` after `auto_poll_interval`:
```sql
    auto_poll_interval INTEGER,
    role TEXT,
    last_active TEXT NOT NULL,
```

- [ ] **Step 2: Add role to Session struct in db/sessions.go**

Add `Role string` after `AutoPollInterval *int`.

- [ ] **Step 3: Update all SQL queries in sessions.go**

Update UpsertSession INSERT/ON CONFLICT, GetSession SELECT, and ListSessions SELECT to include the `role` column. In the upsert ON CONFLICT, preserve role like inbox_mode: `role = sessions.role`.

GetSession and ListSessions need `COALESCE(role, '') as role` and the corresponding Scan field.

- [ ] **Step 4: Update ConfigureSession to accept role**

Change signature to `ConfigureSession(sessionID, inboxMode string, autoPollInterval *int, role *string) error`. When role is non-nil, update it. Update the SQL to `role = COALESCE(?, role)`.

Fix all callers of ConfigureSession (cmd/configure.go, cmd/unregister.go) to pass `nil` for role where not changing it.

- [ ] **Step 5: Add --role flag and --get role to cmd/configure.go**

Add flag: `configureCmd.Flags().String("role", "", "session role (handler, or empty to clear)")`

In the `--get` switch, add `case "role": fmt.Println(session.Role)`.

In the set section, read the role flag and pass it to ConfigureSession.

Update the validation: allow the command to run if any of inbox-mode, auto-poll-interval, or role is provided.

- [ ] **Step 6: Alter existing database**

```bash
sqlite3 ~/.agent-handler/data/handler.db "ALTER TABLE sessions ADD COLUMN role TEXT"
```

- [ ] **Step 7: Build and test**

```bash
go build ./... && go test ./...
handler configure --role handler
handler configure --get role  # should print "handler"
handler configure --role ""   # clear it
```

- [ ] **Step 8: Commit**

```bash
git add db/schema.sql db/sessions.go cmd/configure.go cmd/unregister.go
git commit --signoff -m "feat: session role flag — handler configure --role"
```

---

## Task 2: Role-Based Message Routing

**Files:**
- Modify: `cmd/emit.go`, `db/events.go`

**Consumes:** `Session.Role` from Task 1

**Produces:**
- `--to handler` resolves to `{recipient_type: "role", recipient_value: "handler"}`
- Unread query matches role-based recipients
- `db.DirectCountForSession` returns count of directly-addressed unread events

- [ ] **Step 1: Update resolveRecipient in cmd/emit.go**

Before the session name lookup, add a role check:
```go
// Check if target matches a known role
var roleCount int
d.Conn().QueryRow(`SELECT COUNT(*) FROM sessions WHERE role = ? AND status = 'active'`, to).Scan(&roleCount)
if roleCount > 0 {
    return "role", to, nil
}
```

- [ ] **Step 2: Update UnreadForSession in db/events.go**

Add a clause to the WHERE for role-based recipients. After the branch matching line:
```sql
OR (er.recipient_type = 'role' AND er.recipient_value = ?)
```
Pass `session.Role` as the additional parameter. If role is empty, this clause won't match anything.

Do the same for UnreadCountForSession.

- [ ] **Step 3: Add DirectCountForSession to db/events.go**

New function that counts unread events that are directly addressed (via event_recipients) rather than subscription-routed:
```go
func (db *DB) DirectCountForSession(sessionID string) (int, error) {
    cursor, _ := db.GetCursor(sessionID)
    if cursor == "" { cursor = "1970-01-01T00:00:00Z" }
    session, _ := db.GetSession(sessionID)
    if session == nil { return 0, nil }
    
    var count int
    err := db.conn.QueryRow(`
        SELECT COUNT(DISTINCT e.id) FROM events e
        JOIN event_recipients er ON er.event_id = e.id
        WHERE e.ts > ?
          AND (
            (er.recipient_type = 'session' AND er.recipient_value = ?)
            OR (er.recipient_type = 'branch' AND (er.recipient_value = ? OR er.recipient_value = ?))
            OR (er.recipient_type = 'role' AND er.recipient_value = ?)
          )
    `, cursor, sessionID, session.Branch, session.Repo+":"+session.Branch, session.Role).Scan(&count)
    return count, err
}
```

- [ ] **Step 4: Build and test**

```bash
go build ./... && go test ./...
handler configure --role handler
handler emit --type message --title "test role routing" --to handler
handler unread --count  # should show 1
```

- [ ] **Step 5: Commit**

```bash
git add cmd/emit.go db/events.go
git commit --signoff -m "feat: role-based message routing — --to handler targets sessions by role"
```

---

## Task 3: handler log --global

**Files:**
- Modify: `cmd/log_cmd.go`

**Consumes:** `db.QueryEvents`, `db.GetCursor`, `db.AdvanceCursor`, `resolveSessionID`

- [ ] **Step 1: Add flags to log command**

Add `--global` (bool) and `--since-cursor` (bool) flags in init().

- [ ] **Step 2: Implement global log**

When `--global` is set, query all events without filtering by session_id. Use `db.QueryEvents` with a filter where `SessionID` is nil (or modify QueryEvents to accept a `Global bool` in the filter).

If `--since-cursor` is set, get the current session's cursor, use it as the `Since` filter, and advance the cursor after output.

If `--since` is provided (existing flag), use it as the Since filter.

Each event in the output should show the source/session attribution. In text mode, prefix each event with the session name or watcher source.

- [ ] **Step 3: Build and test**

```bash
go build ./...
handler log --global --limit 10
handler log --global --since-cursor
handler log --global --since-cursor  # second time should show fewer/no events
```

- [ ] **Step 4: Commit**

```bash
git add cmd/log_cmd.go
git commit --signoff -m "feat: handler log --global with --since-cursor for cross-session timeline"
```

---

## Task 4: handler triage

**Files:**
- Create: `cmd/triage.go`

**Consumes:** `db.ListSessions`, `db.UnreadCountForSession`, `db.HasWatcherError`, `db.GetWatcherStatus`, `db.GetCursor`, `resolveSessionID`, `discover.IsProcessAlive`

- [ ] **Step 1: Create cmd/triage.go**

A `human` group command that aggregates:
1. Count active/blocked/dead sessions
2. Blocked sessions: query for sessions with `blocked` event and no subsequent `unblocked`
3. Sessions with unread: for each active session, call `UnreadCountForSession`
4. Watcher errors: check `HasWatcherError` for each watcher
5. Events since handler's last check: count events after handler session's cursor
6. Dead sessions: list sessions where process is not alive

Output as JSON with `--json`, or human-readable text summary.

The implementing engineer should read the spec's JSON schema for the exact output structure and the blocked session SQL query.

- [ ] **Step 2: Build and test**

```bash
go build ./...
handler triage
handler triage --json
```

- [ ] **Step 3: Commit**

```bash
git add cmd/triage.go
git commit --signoff -m "feat: handler triage — aggregates what needs attention across sessions"
```

---

## Task 5: Statusline — Direct Messages + Handler Layout

**Files:**
- Modify: `cmd/statusline.go`

**Consumes:** `db.DirectCountForSession` from Task 2, `Session.Role` from Task 1

- [ ] **Step 1: Add direct message indicator to standard statusline**

After the existing unread count line, check `d.DirectCountForSession(slSessionID)`. If > 0, append `| ● N direct` to the inbox line in yellow.

- [ ] **Step 2: Add handler-specific statusline**

Check `session.Role == "handler"`. If so, render a different layout:

**Line 1:** `/handler: N active, N blocked | N new events [| ● N direct]`
- Active count: count sessions with status=active excluding self
- Blocked count: same query as triage
- New events: count events since handler's cursor
- Direct: from DirectCountForSession

**Line 2:** Omit inbox-mode line.

**Line 3:** `/watching:` with global resource count and watcher status (reuse existing logic but query all sessions' subscriptions, not just this session's).

- [ ] **Step 3: Build and test**

```bash
go build ./...
handler configure --role handler
handler statusline --session $(handler whoami)
handler configure --role ""
handler statusline --session $(handler whoami)  # should show standard layout
```

- [ ] **Step 4: Commit**

```bash
git add cmd/statusline.go
git commit --signoff -m "feat: direct message indicator + handler session statusline"
```

---

## Task 6: Skills — /handler, /watching update, /using-handler update

**Files:**
- Create: `skills/handler/SKILL.md`
- Modify: `skills/watching/SKILL.md`, `skills/using-handler/SKILL.md`, `cmd/uninstall.go`

- [ ] **Step 1: Create skills/handler/SKILL.md**

```markdown
---
name: handler
description: "Turn this session into the handler — a command center for managing all active sessions. Use when you want a global view of all sessions, events, and resources."
---

# /handler — Handler Session

## On invocation

1. Set this session's role:
```bash
handler configure --role handler
```

2. Load current state:
```bash
handler triage --json
```

3. Present a narrative summary of:
   - Active sessions and any blocked
   - Sessions with unread events
   - Watcher health
   - Events since last check

4. Set up a polling loop (check CronList first — skip if already exists):
```
CronCreate:
  cron: "*/1 * * * *"
  durable: false
  recurring: true
  prompt: "Run handler log --global --since-cursor --json. Also run handler unread --count to check for direct messages. If there are new events or direct messages, summarize them. For direct messages, present them as action items."
```

5. Tell the user what they can ask.

## What the user can ask

- "What's going on?" → `handler triage --json`
- "What changed since last time?" → `handler log --global --since-cursor --json`
- "What should I work on?" → `handler triage --json`, reason about priorities
- "Tell the auth session about X" → `handler emit --type message --title "X" --to <target>`
- "Show me everything about PR #123" → `handler resource history pr:owner/repo#123`
- "Which sessions are related to X?" → `handler resource related --session <id>`

Use `handler <command> --help` for flag details on any command.

## Idempotent

Re-invoking /handler re-runs triage and refreshes. If the cron job already exists, don't create a duplicate.
```

- [ ] **Step 2: Update skills/watching/SKILL.md**

Add a note: "If this session has the handler role, run `handler watching --global --json` instead of `handler watching --json`."

- [ ] **Step 3: Update skills/using-handler/SKILL.md**

Add to the intro: "To send a message to the handler session, use `--to handler`."
Add to the skills section: "- `/handler` — turn this session into a command center for all sessions"

- [ ] **Step 4: Update cmd/uninstall.go skillNames**

Add `"handler"` to the skillNames slice.

- [ ] **Step 5: Build, install, and test**

```bash
make build && make install
```

Verify `/handler` appears in the skills list.

- [ ] **Step 6: Commit**

```bash
git add skills/handler/SKILL.md skills/watching/SKILL.md skills/using-handler/SKILL.md cmd/uninstall.go
git commit --signoff -m "feat: /handler skill, update /watching and /using-handler for handler session"
```

---

## Task 7: Integration Smoke Test

- [ ] **Step 1: Test the full handler session flow**

```bash
# In a session, set up as handler
handler configure --role handler
handler triage
handler triage --json
handler log --global --limit 5
handler log --global --since-cursor
handler statusline --session $(handler whoami)

# From another session, send a message to handler
handler emit --type message --title "Need help with auth types" --to handler

# Back in handler session
handler unread --count  # should show 1 direct
handler statusline --session $(handler whoami)  # should show direct message indicator
```

- [ ] **Step 2: Test role clearing**

```bash
handler configure --role ""
handler statusline --session $(handler whoami)  # should show standard layout
```

- [ ] **Step 3: Commit any fixes**

---

## Task 8: Documentation

- [ ] **Step 1: Update CLAUDE.md**

Add note that `/handler` skill exists and that the role flag affects statusline behavior.

- [ ] **Step 2: Update README.md**

Add a "Handler Session" section explaining how to use `/handler` for cross-session management.

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md README.md
git commit --signoff -m "docs: update CLAUDE.md and README with handler session documentation"
```
