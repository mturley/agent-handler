# Phase 3: Handler Session — Design Spec

## Overview

The handler session is a Claude Code session that acts as a command center for all active sessions. The user invokes `/handler` to load overseer capabilities: a global view of all sessions, events, and resources, with a polling loop that proactively surfaces new activity.

---

## Session Role

A `role` column (TEXT, nullable) is added to the `sessions` table. When `/handler` is invoked, it sets `role = 'handler'` via `handler configure --role handler`. This flag drives statusline behavior and skill defaults.

`handler configure --role <role>` is a new flag on the existing configure command. Supported roles: `handler` (or empty to clear).

---

## handler log --global

Extends the existing `handler log` command to show events across all sessions.

**New flags:**
- `--global` — show all events from all sessions and watchers
- `--since-cursor` — only events after this session's cursor, then advance the cursor
- `--since <timestamp>` — only events after the given timestamp

Each event in the output includes the session name/branch or watcher source for attribution.

**Behavior:**
- Without `--global`: existing per-session behavior (unchanged)
- With `--global`: queries all events regardless of session, ordered chronologically
- `--since-cursor` uses the handler session's own cursor from `session_cursors`, advances it after output
- `--global` and `--since-cursor` can be combined: "show me everything I haven't seen"

---

## handler triage

A new command that aggregates what needs attention across all sessions.

**Output (JSON):**
```json
{
  "sessions_active": 5,
  "sessions_blocked": 1,
  "sessions_dead": 2,
  "blocked_sessions": [
    {"session_id": "...", "session_name": "...", "branch": "...", "blocked_since": "..."}
  ],
  "sessions_with_unread": [
    {"session_id": "...", "session_name": "...", "unread_count": 3, "unread_types": {"pr_comment": 2, "ci_check_failed": 1}}
  ],
  "watcher_errors": [
    {"name": "github", "last_error_message": "..."}
  ],
  "events_since_last_check": 12,
  "dead_sessions": [
    {"session_id": "...", "session_name": "...", "last_active": "..."}
  ]
}
```

**How blocked sessions are detected:**
Query for sessions that have a `blocked` event with no subsequent `unblocked` event:
```sql
SELECT s.session_id, s.session_name, s.branch, e.ts as blocked_since
FROM sessions s
JOIN events e ON e.session_id = s.session_id AND e.type = 'blocked'
WHERE s.status = 'active'
  AND NOT EXISTS (
    SELECT 1 FROM events e2
    WHERE e2.session_id = s.session_id
      AND e2.type = 'unblocked'
      AND e2.ts > e.ts
  )
```

**Text output** provides a human-readable summary. `--json` provides the structured data for the agent.

Command group: `human`.

---

## Direct Message Indicator (All Sessions)

The statusline for all sessions (not just handler) gains a direct message indicator. Unread events are split into "watched" (routed via resource subscriptions) and "direct" (routed via `event_recipients` matching session ID, branch, or role).

**Standard session statusline:**
```
/inbox: ● 3 unread (2 pr_comment, 1 ci_fail) | ● 1 direct
```

The `| ● N direct` suffix appears only when there are direct messages. The yellow `●` signals "someone is talking to you" — distinct from passive watcher events.

**Implementation:** The unread query already returns all matching events. To split them, the statusline queries:
- Total unread count (existing)
- Direct count: events where an `event_recipients` row matches this session's ID, branch, or role

---

## Handler Session Statusline

When `role = handler`, the statusline shows a different layout:

**Line 1** (replaces `/inbox`):
```
/handler: 3 active, 1 blocked | 5 new events | ● 1 direct message
```
- Count of active sessions (excluding self)
- Count of blocked sessions
- Count of events since the handler's cursor
- Count of unread messages directed to this session (via `--to handler`), shown with yellow `●` if > 0

**Line 2** (replaces `/inbox-mode`): omitted — handler sessions don't use inbox modes.

**Line 3** (`/watching`): shows global resource count + watcher status (same as `handler watching --global` summary).

When `role` is empty/null, the standard statusline is shown (unchanged).

---

## Role-based Message Routing

`--to handler` in `handler emit` resolves to sessions with `role = 'handler'`. This allows any session to direct a message specifically to the handler session — not just for awareness (the handler sees everything), but as a request for the handler to take action.

**resolveRecipient update:** In `cmd/emit.go`, `resolveRecipient` checks if the target matches a known role before checking session names and branches. If it matches, it creates a `role` recipient type.

**event_recipients:** A new recipient type `role` is supported: `{recipient_type: "role", recipient_value: "handler"}`.

**Unread query update:** The unread query in `db/events.go` gets an additional clause matching `recipient_type = 'role'` against the session's role.

**using-handler skill update:** "To send a message to the handler session, use `--to handler`."

---

## /handler Skill

### On invocation:
1. Set session role: `handler configure --role handler`
2. Run `handler triage --json` to load current state
3. Present a narrative summary to the user:
   - How many sessions are active, any blocked
   - Unread events across sessions
   - Watcher health
   - What's changed since last check (if handler session has been used before)
4. Set up a session-scoped cron job (every minute):
   ```
   CronCreate:
     cron: "*/1 * * * *"
     durable: false
     recurring: true
     prompt: "Run handler log --global --since-cursor --json. Also run handler unread --count to check for messages directed to the handler. If there are new events or unread messages, summarize them. For direct messages (--to handler), present them as action items."
   ```
5. Tell the user what they can ask

### Idempotent:
Invoking `/handler` again re-runs triage and refreshes. If the cron job exists (check via CronList), skip creating a new one.

### What the user can ask:
- "What's going on?" → `handler triage --json`
- "What changed since last time?" → `handler log --global --since-cursor --json`
- "What should I work on?" → `handler triage --json`, reason about priorities
- "Tell the auth session about X" → `handler emit --type message --title "X" --to <target>`
- "Show me everything about PR #123" → `handler resource history pr:owner/repo#123`
- "Which sessions are related to X?" → `handler resource related --session <id>`

The skill teaches the agent to use `handler <command> --help` for flag details on any command it needs.

---

## /watching in Handler Sessions

When the session has `role = handler`, the `/watching` skill runs `handler watching --global --json` instead of `handler watching --json`.

---

## Modified Skills

### using-handler
Add mention of `/handler` skill: "If you're the handler session (managing all sessions), use the `/handler` skill."

---

## Schema Change

Add `role TEXT` column to `sessions` table. Nullable, default empty.

---

## Files to Create/Modify

**New:**
- `cmd/triage.go` — handler triage command
- `skills/handler/SKILL.md` — /handler skill

**Modified:**
- `cmd/log_cmd.go` — add `--global`, `--since-cursor`, `--since` flags
- `cmd/configure.go` — add `--role` flag
- `cmd/emit.go` — resolveRecipient handles role-based targeting
- `cmd/statusline.go` — handler session layout
- `cmd/watching.go` — auto-global for handler sessions (optional, or just in skill)
- `db/events.go` — unread query matches role-based recipients
- `db/schema.sql` — add role column
- `db/sessions.go` — include role in Session struct and queries
- `skills/watching/SKILL.md` — use global when handler session
- `skills/using-handler/SKILL.md` — mention /handler and --to handler
