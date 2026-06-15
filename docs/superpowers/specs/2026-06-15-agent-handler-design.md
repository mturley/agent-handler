# agent-handler: Design Spec

## Project Identity and Scope

**Project name:** agent-handler
**Repo:** `agent-handler` (GitHub: mturley/agent-handler)

agent-handler is a centralized event ledger, pub/sub messaging system, and external resource watcher for managing multiple parallel Claude Code sessions across git worktrees. It supports a human-driven workflow where the developer manually drives each session but wants situational awareness across all of them, with external events (PR reviews, Jira comments, CI failures) surfaced to the relevant sessions automatically.

**CLI tool:** `handler` -- the sole interface for all operations. The database is an implementation detail behind the CLI.

**Components:**
- **Ledger** -- the SQLite event store and pub/sub routing logic. Internal component.
- **Watchers** -- external event polling scripts (GitHub, Jira, etc.), scheduled via launchd (macOS) or cron (Linux).
- **Skills** -- Claude Code skills that teach agents how to interact with handler. Distributed with the project, symlinked into `~/.claude/skills/` on install.
- **Hooks** -- Claude Code hook scripts that wire session lifecycle events to handler operations.
- **Peek** -- a tool for inspecting live Claude sessions via tmux (are they waiting for approval, idle, actively working, etc.).

**What agent-handler is not:**
- Not a task decomposition or autonomous orchestration system
- Not a replacement for the existing `worklog` Obsidian tool (may converge later)
- Not Claude-specific in schema design, but built for Claude Code first with clean boundaries for future harness support

**Implementation language:** Go. Single static binary, ~5ms cold start for hot-path hook operations, pure-Go SQLite bindings (`modernc.org/sqlite`).

**Data location:** `~/.agent-handler/` (DB at `handler.db`, PID cache in `sessions/`, watcher logs in `logs/`).

---

## Session Lifecycle and Identity

**Session ID:** The Claude Code session UUID, discovered at registration time by scanning `~/.claude/projects/-<cwd-path>/` for the most recently modified `.jsonl` file. The filename (minus `.jsonl`) is the UUID. This is the primary key for all session-related data in the DB, and is the same ID used by `claude --resume`.

**Session name:** Read directly from Claude's JSONL transcript -- the last `agent-name` entry (set by `/name`), falling back to the last `ai-title` entry (auto-generated). Handler does not maintain its own naming mechanism. Names are read on demand and cached in session metadata.

**PID cache:** `~/.agent-handler/sessions/<pid>` contains the session UUID. Written on registration, read by hooks on the hot path. Disposable -- if missing or stale, hooks fall back to JSONL discovery (~10ms).

**Registration:** `SessionStart` hook discovers the session UUID and calls `handler register`. Upserts -- if the UUID already exists (resumed session), updates PID, reads current session name from JSONL, bumps `last_active`, and flips status back to `active`. All existing subscriptions, events, and cursor state are preserved across resumes.

**Heartbeat:** `UserPromptSubmit` hook bumps `last_active`. Single indexed UPDATE, sub-millisecond.

**Liveness detection:** No deregistration hook (there is no reliable "session exiting" event). `handler status` checks liveness at query time by scanning for a running Claude process. Three display states:
- **active** -- process found, recent heartbeat
- **idle** -- process found, no recent heartbeat
- **dead** -- no process found

**Cleanup/archival:** `handler cleanup` sets dead sessions to `archived` status. They disappear from default `handler status` output but all data is preserved -- events, subscriptions, cursor position. `handler status --all` shows archived sessions (paginated, most recent first, default 20). Resuming an archived session with `claude --resume <uuid>` re-registers it as active automatically.

**Per-session configuration:**
- Inbox mode: `manual` (default), `on-submit`, or `auto` -- see Inbox Modes section

---

## Event Schema and Storage

**Database:** Single global SQLite database at `~/.agent-handler/handler.db`. WAL mode for concurrent read/write safety.

**Event IDs:** UUIDs, not auto-increment. Avoids collision if the DB is ever split/archived.

### Tables

#### `events`
Append-only event log.

| Column | Type | Notes |
|--------|------|-------|
| `id` | TEXT PK | UUID |
| `ts` | TEXT | ISO 8601 UTC, when the event was recorded |
| `external_ts` | TEXT, nullable | For external events, when the event actually happened |
| `source` | TEXT | `agent`, `github`, `jira`, `jenkins`, `slack`, `handler` |
| `session_id` | TEXT, nullable | Which session emitted this event (null for watcher/system events) |
| `type` | TEXT | Event type (see Event Types below) |
| `title` | TEXT | Short one-liner for list views |
| `body` | TEXT, nullable | Full content, can be markdown |
| `author` | TEXT, nullable | Who triggered the external event (display name or username) |
| `author_type` | TEXT, nullable | `human`, `bot`, `unknown` |
| `broadcast` | INTEGER | 0 or 1, default 0 |
| `tags` | TEXT, nullable | Comma-separated for lightweight filtering |

Indexed on `ts` (cursor queries), `(source, type)` (filtered queries), `session_id` (per-session timeline).

#### `event_recipients`
Direct addressing for inter-session messaging and overseer commands. An event with no recipients and `broadcast = 0` is stored for aggregation only.

| Column | Type | Notes |
|--------|------|-------|
| `event_id` | TEXT FK | |
| `recipient_type` | TEXT | `session`, `branch` |
| `recipient_value` | TEXT | Session UUID or branch name |

Indexed on `(recipient_type, recipient_value)`.

#### `event_resources`
Which external resources an event relates to.

| Column | Type | Notes |
|--------|------|-------|
| `event_id` | TEXT FK | |
| `resource_type` | TEXT | `pr`, `jira`, `jenkins`, `slack`, etc. |
| `resource_id` | TEXT | Canonical identifier for matching (e.g. `owner/repo#123`) |
| `resource_url` | TEXT, nullable | Clickable link |

Indexed on `(resource_type, resource_id)`.

#### `sessions`
Point-in-time session metadata.

| Column | Type | Notes |
|--------|------|-------|
| `session_id` | TEXT PK | Claude session UUID |
| `harness` | TEXT | Default `claude`, future: `cursor`, etc. |
| `repo` | TEXT | |
| `branch` | TEXT | |
| `session_name` | TEXT, nullable | Cached from Claude's JSONL |
| `pid` | INTEGER, nullable | Last known PID |
| `status` | TEXT | `active`, `archived` |
| `inbox_mode` | TEXT | `manual`, `on-submit`, `auto` -- default `manual` |
| `auto_poll_interval` | INTEGER, nullable | Seconds, for `auto` mode |
| `last_active` | TEXT | ISO 8601 UTC |
| `registered_at` | TEXT | |
| `jsonl_path` | TEXT | Path to Claude's transcript JSONL |

#### `session_cursors`
Per-session read position for unread queries.

| Column | Type | Notes |
|--------|------|-------|
| `session_id` | TEXT PK FK | |
| `last_seen_ts` | TEXT | ISO 8601 UTC |

#### `subscriptions`
Session interest in external resources.

| Column | Type | Notes |
|--------|------|-------|
| `id` | TEXT PK | UUID |
| `session_id` | TEXT FK | |
| `resource_type` | TEXT | `pr`, `jira`, `jenkins`, etc. |
| `resource_id` | TEXT | Canonical identifier |
| `resource_url` | TEXT, nullable | Clickable link |
| `created_at` | TEXT | |
| `deleted_at` | TEXT, nullable | Soft delete |

Indexed on `(resource_type, resource_id, deleted_at)`.

#### `resource_relationships`
Structural/hierarchical relationships between external resources. Limited to stable relationships only (epic membership, PR-to-issue links). Transient state like blocking relationships should be queried live, not cached.

| Column | Type | Notes |
|--------|------|-------|
| `id` | TEXT PK | UUID |
| `child_type` | TEXT | e.g. `jira` |
| `child_id` | TEXT | e.g. `RHOAIENG-100` |
| `child_url` | TEXT, nullable | |
| `parent_type` | TEXT | e.g. `jira` |
| `parent_id` | TEXT | e.g. `RHOAIENG-50` (the epic) |
| `parent_url` | TEXT, nullable | |
| `relationship` | TEXT | `epic_child`, `implements`, `subtask_of` |
| `source` | TEXT | `watcher` or `manual` |
| `created_at` | TEXT | |

### Event Types

**Agent-originated:** `milestone`, `status`, `blocked`, `unblocked`, `decision`, `handoff`, `followup`, `pre_compact_snapshot`, `session_start`, `session_end`

**External (from watchers):**
- PR: `pr_comment`, `pr_review_comment`, `pr_review_requested`, `pr_approved`, `pr_merged`, `pr_closed`, `pr_reopened`, `pr_new_commits`
- CI: `ci_pass`, `ci_fail`
- Jira: `jira_comment`, `jira_status_change`, `jira_assigned`

**System:** `message` (explicit inter-session message)

Subscription changes are not events. They are state mutations in the `subscriptions` table with their own audit trail via `created_at`/`deleted_at`.

### Unread Query Logic

An event is unread for session S if:
1. `events.ts > session_cursors.last_seen_ts` for session S
2. AND at least one of:
   - An `event_resources` row references a resource that session S subscribes to (active subscription)
   - An `event_recipients` row directly addresses session S (by session ID or branch)
   - `events.broadcast = 1`

Two indexed joins, no JSON parsing.

---

## Subscriptions and Routing

**Creating subscriptions:**
- **Manual:** `handler subscribe --resource "pr:owner/repo#123" --url "https://..."` -- also appends to `.worktree-resources`
- **Auto on session start:** `handler register` reads `.worktree-resources` if present, subscribes to listed resources
- **Via skill:** Agent can `handler subscribe` when it starts working on a PR or issue
- **Auto from watchers:** Watchers discover structural relationships (epic links) and write `resource_relationships` entries, but do not auto-subscribe sessions to parent resources

**Removing subscriptions:**
- Soft delete (`deleted_at` set, record preserved for history)
- **Auto on terminal states:** Watcher writes the close/merge event and soft-deletes the subscription in the same transaction
- **Reopened resources:** Watcher reinstates subscription on reopen event (clears `deleted_at`)
- **Manual:** `handler unsubscribe --resource "..."` -- also removes from `.worktree-resources`

**`.worktree-resources` file:** Lives in the worktree root, gitignored. One resource per line: `<resource_id> <resource_url>`. Bidirectional sync -- `handler subscribe` appends, `handler unsubscribe` removes. Portable across tools; the worktree management script can read/write it without touching the DB.

**Dormancy:** A resource is only skipped by watchers if ALL sessions subscribed to it have `last_active` older than 7 days. If even one active session cares about a resource, the watcher keeps polling it. Sessions archived after 30 days of inactivity.

**Marking as read:** `handler ack` advances the session's cursor. Can ack all events or up to a specific timestamp.

---

## Inbox Modes

Three modes for how a session receives unread events, controlled by `/inbox_mode`:

| Mode | Behavior |
|------|----------|
| `manual` (default) | Status line shows unread count. Agent checks when asked via `/inbox`. |
| `on-submit` | Unread events auto-injected into prompt context on each `UserPromptSubmit`. |
| `auto` | Agent actively polls on a configurable interval, proactively surfaces events and offers to act on them. |

All modes are opt-in and switchable at any time. The agent can change its own mode via the `/inbox_mode` skill.

**Auto mode recovery:** If `inbox_mode` is `auto` but the polling loop is not running (e.g. after a session restart), the `UserPromptSubmit` hook detects this and surfaces a reminder: "Inbox mode is auto but polling is not active. Run `/inbox_mode auto` to restart." The status line also reflects this state.

**Status line display (two lines):**
```
/inbox: 2 unread (1 review, 1 CI fail)
/inbox_mode: manual | on-submit | auto
```
The active mode is visually highlighted. In `auto` mode, the interval is shown: `auto (60s)`. If `auto` is set but polling has stopped: `auto (stopped - /inbox_mode auto to restart)`.

---

## Hook Integration

All hooks shell out to the `handler` CLI.

### `SessionStart`
- Discover session UUID from JSONL
- `handler register --session-id <uuid> --branch <branch> --repo <repo> --pid <pid>`
- Read `.worktree-resources`, auto-subscribe
- Read session name from JSONL, update metadata
- Query for unread events since `last_seen_ts`, return catch-up summary
- If inbox mode is `auto`, remind the agent to restart polling: "Inbox mode is set to auto. Run `/inbox_mode auto` to resume polling."
- Set tmux pane title if in tmux

### `UserPromptSubmit`
Must be fast (<10ms).
- Read session UUID from PID cache
- Bump `last_active`
- If inbox mode is `on-submit`: query unread events, prepend to prompt context, advance cursor

### `PreCompact`
- Write a `pre_compact_snapshot` event with a summary of current session state

### `Stop`
- No action (fires after every turn, not on session exit)

### `WorktreeRemove`
- Soft-delete all subscriptions for that worktree's branch

### Not used in Phase 1
- **Sentinel file + `FileChanged`** -- dropped as fragile for marginal gain
- **`asyncRewake`** -- known bug (renders XML visibly). Revisit if fixed.
- **`Notification`** -- could detect permission-waiting state. Deferred.

---

## External Event Watchers

**Architecture:** Standalone scripts that poll external APIs on a schedule. Stateless -- the ledger DB is their state.

**Polling cycle:**
1. Query `subscriptions` for active, non-dormant resources of this watcher's type
2. For each resource, find the most recent event from this source -- use its `external_ts` as cursor
3. Query external API for activity after that cursor
4. Write new events with `event_resources` entries, `author`, and `author_type`
5. On terminal states (PR merged, issue resolved): write event AND soft-delete subscription in same transaction
6. On reopen: reinstate subscription

**Catch-up:** On first run or after downtime, check current resource state, not just deltas.

**Author metadata:** All external events include `author` and `author_type` (`human`/`bot`/`unknown`). Determined from API data (GitHub `type: "Bot"` field) or heuristics.

**Scheduling:**
- macOS: launchd plists in `~/Library/LaunchAgents/`
- Linux: cron entries
- Abstracted behind `handler watcher install/uninstall`
- Recommended intervals: GitHub 2-3 min, Jira 5 min, Jenkins 2-3 min

**Planned watchers:**
- Phase 2: GitHub PR, Jira (with epic link discovery)
- Phase 3+: Jenkins, Slack, git commit hook

---

## CLI Commands

All commands output human-readable text by default, with `--json` for machine-readable output.

### Core

| Command | Description |
|---------|-------------|
| `handler register` | Register/re-register a session |
| `handler unregister` | Archive a session and soft-delete its subscriptions. For explicit session teardown before quitting. |
| `handler heartbeat` | Bump `last_active` |
| `handler status` | All sessions with liveness, unread counts, last activity. `--all` includes archived (paginated, 20 default). |
| `handler cleanup` | Archive dead sessions. `--stale 14d` for idle threshold. |
| `handler health` | DB size, subscription counts, active/dormant/dead breakdown, watcher install status. |
| `handler configure` | Per-session settings (inbox mode, poll interval) |
| `handler statusline --session <id>` | Compact two-line output for status line widget |

### Events

| Command | Description |
|---------|-------------|
| `handler emit` | Write an event to the ledger |
| `handler unread --session <id>` | Show unread events for a session |
| `handler ack --session <id>` | Advance cursor, mark events as read |
| `handler tail` | Live event stream with filters (`--session`, `--type`, `--source`) |
| `handler log --session <id>` | Event timeline for a specific session |

### Subscriptions

| Command | Description |
|---------|-------------|
| `handler subscribe` | Subscribe to a resource (also appends to `.worktree-resources`) |
| `handler unsubscribe` | Soft-delete subscription (also removes from `.worktree-resources`) |
| `handler subscriptions` | List subscriptions for a session or all sessions |

### Resources

| Command | Description |
|---------|-------------|
| `handler resource link` | Create a resource relationship |
| `handler resource related --session <id>` | Find sessions related via shared or linked resources |
| `handler resource history <resource_id>` | All events and sessions related to a resource |

### Watchers

| Command | Description |
|---------|-------------|
| `handler watcher install <name>` | Install watcher (launchd/cron), configure interval |
| `handler watcher uninstall <name>` | Remove watcher |
| `handler watcher list` | Show installed watchers and status |
| `handler watcher logs <name>` | Tail watcher log output |
| `handler watcher run <name>` | Run watcher once manually |

### Session Inspection

| Command | Description |
|---------|-------------|
| `handler peek --session <id>` | Capture tmux pane for a session |

### Advanced

| Command | Description |
|---------|-------------|
| `handler query "<sql>"` | Run arbitrary read-only SQL against the ledger DB. Escape hatch for ad-hoc analysis that the CLI commands don't cover. DB connection opened in read-only mode. |
| `handler schema` | Dump current table definitions so agents can inspect the schema before writing queries. |

---

## Skills

Skills are distributed with agent-handler under a `skills/` directory and symlinked into `~/.claude/skills/` on install.

### Frequently used (short names)

| Skill | Description |
|-------|-------------|
| `/inbox` | Check and act on unread events for the current session |
| `/inbox_mode` | Set inbox mode: `manual`, `on-submit`, `auto` |

### Namespaced skills

| Skill | Description |
|-------|-------------|
| `/handler_register` | Register session, subscribe to `.worktree-resources`, return catch-up summary |
| `/handler_emit` | Write an event to the ledger (guides agent on event type selection) |
| `/handler_subscribe` | Subscribe to a resource |
| `/handler_snapshot` | Write a `pre_compact_snapshot` event (also called by `PreCompact` hook) |
| `/handler_unregister` | Archive this session and soft-delete its subscriptions before quitting |
| `/handler` | Initialize or refresh the handler session. Loads all active sessions, unread events, and presents a narrative summary. Idempotent -- safe to call multiple times. Guides the agent on how to use CLI commands for deeper queries, spawn subagents for detailed worktree summaries, find related sessions, and send inter-session messages. |

---

## Installation and Setup

**`handler install`** -- sets up agent-handler for a user:
- Builds and installs the `handler` binary to a location on PATH
- Creates `~/.agent-handler/` directory and initializes `handler.db`
- Symlinks skills from the agent-handler installation into `~/.claude/skills/`
- Registers Claude Code hooks in the appropriate settings file
- Optionally installs watchers (with `--watchers github,jira`)

**`handler uninstall`** -- reverses the installation:
- Removes skill symlinks from `~/.claude/skills/`
- Removes Claude Code hook registrations
- Uninstalls any active watchers (launchd plists / cron entries)
- Optionally removes `~/.agent-handler/` (with `--purge` flag, prompts for confirmation)
- Does not remove the `handler` binary itself (user manages that)

The exact installation mechanism will be determined during implementation. During development, symlinks and hook registration are done manually.

---

## Implementation Phasing

### Phase 1: Core Ledger and Observability
- SQLite schema (all tables)
- `handler` CLI: `register`, `heartbeat`, `emit`, `unread`, `ack`, `status`, `cleanup`, `health`, `configure`, `statusline`, `subscribe`, `unsubscribe`, `subscriptions`, `log`, `tail`
- `handler resource`: `link`, `related`, `history`
- Claude Code hooks: `SessionStart`, `UserPromptSubmit`, `PreCompact`
- Status line widget
- Skills: `/inbox`, `/inbox_mode` (all three modes: manual, on-submit, auto), `/handler_register`, `/handler_emit`, `/handler_subscribe`, `/handler_snapshot`
- `.worktree-resources` file support
- PID cache
- `handler install` / `handler uninstall`

### Phase 2: External Watchers
- GitHub PR watcher
- Jira watcher (with epic link discovery)
- `handler watcher`: `install`, `uninstall`, `list`, `logs`, `run`
- Launchd/cron scheduling abstraction
- Auto-subscribe/unsubscribe on terminal states
- Dormancy logic

### Phase 3: Handler Session and Cross-Session Awareness
- `/handler` skill: loads all active session state and unread events, presents narrative summary. Idempotent -- call again to refresh. Guides the agent on deeper queries (subagents per worktree, related session discovery, inter-session messaging).
- Inter-session messaging via `event_recipients`

### Phase 4: Session Inspection (Peek)
- `handler peek --session <id>` -- capture and interpret a session's tmux pane
- `handler claude` -- a wrapper that starts a Claude session inside a tmux pane with a placeholder title (`handler:pending`), then passes through all arguments to `claude`. When `SessionStart` fires and `handler register` discovers the session UUID, it detects the wrapped tmux pane (by the placeholder title or an env var set by the wrapper) and updates the title to `handler:<session_id>`. Using this wrapper is optional; all other handler functionality works without it. Future harness wrappers (e.g. `handler cursor`) would follow the same pattern.
- Pane discovery: `handler peek` finds the tmux pane by matching the `handler:<session_id>` title convention. If the session was not started via the wrapper (no matching pane title), `handler peek` reports that the session exists but is not inspectable.
- Pane capture output can be piped through an LLM call to interpret state ("waiting for tool approval", "actively generating", "idle at prompt")
- `handler status` shows a `peekable` indicator for sessions started via the wrapper

### Phase 5: Web UI
- Event timeline with filters
- Session status cards
- Subscription graph visualization

### Phase 6: Additional Watchers and Integrations
- DB archival (`handler archive --before <date>`)
- Jenkins watcher
- Git commit hook (post-commit events)
- Slack watcher
- Worklog (Obsidian) integration or replacement

### Phase 7: Non-Claude Harness Support
- Adapter layer for alternative agent harnesses (Cursor, etc.)
- `handler cursor` wrapper (same pattern as `handler claude`)
- Harness-specific session discovery and hook integration

---

## Design Principles

- **The DB is the memory, not any session** -- sessions are ephemeral, the ledger is durable
- **CLI as the sole interface** -- storage is an implementation detail
- **Stateless watchers** -- the ledger DB is the state; watchers carry none
- **Polling over persistent watching** -- simpler, self-healing
- **Opt-in communication** -- sessions don't talk to each other by default
- **Events not broadcast by default** -- the ledger is passive unless explicitly addressed or subscribed
- **Graceful degradation** -- tmux optional, handler optional in worktree script
- **Observability before communication** -- build passive logging first, active messaging later
- **No urgency field** -- event type and author_type provide structural signal; content-based urgency is the consuming agent's judgment
- **Soft deletes everywhere** -- subscriptions and sessions are archived, never destroyed. Full history always queryable.
- **`.worktree-resources` as the portable contract** -- bidirectional sync with DB subscriptions, readable by other tools without touching the DB
