# agent-ledger: Inter-Agent Event Ledger — Brainstorming Report

This document summarizes a brainstorming session about designing a system called **agent-ledger** — a lightweight, harness-agnostic inter-agent pub/sub and event coordination layer for managing multiple parallel Claude Code sessions across git worktrees. It is intended as context for a deeper design and implementation session.

---

## Background and Motivation

The author uses Claude Code day-to-day for software development, along with custom scripts for managing git worktrees and logging activity to an Obsidian notes vault (via a separate tool called `worklog`). The goal is not to build a task-decomposition multi-agent system (like Gas Town, Ruflo, or Claude Squad), but rather to support a human-driven workflow where:

- Multiple Claude Code sessions run in parallel, each in its own git worktree, working on different features and bugs of the same product
- The developer manually drives each session but wants situational awareness across all of them
- A high-level "overseer" session or UI can answer questions like "what's going on across all my sessions" and "what should I work on right now"
- External events (GitHub PR reviews, Jira comments, CI failures) are surfaced to the relevant sessions automatically
- Sessions can optionally communicate with each other when explicitly wired to do so

---

## Ecosystem Research

### Claude Code Native Multi-Agent Support
Claude Code has an experimental native Agent Teams feature (requires `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`). One session acts as team lead coordinating via a shared task list, while teammates each run in their own context windows. Known limitations include no session resumption if the terminal closes, task state lag between teammates, and slow teammate shutdown. This is designed for task decomposition, not the parallel independent worktree workflow described here.

### Third-Party Tools Reviewed
- **Gas Town** (Steve Yegge): Go-based orchestration for 20-30 parallel agents via tmux, with git as the persistence layer. Heavy API costs. Uses "Beads" for git-based work tracking. Ephemeral worker agents that die after creating an MR — their work survives in git even if their context doesn't. The "git as memory" pattern is worth borrowing.
- **Ruflo** (formerly Claude Flow): Rewritten in Rust/WASM, mature open-source option with persistent memory and MCP bridging. Complex.
- **Claude Squad**: Zero-setup tmux-based tool with terminal UI dashboard. Closest to a lightweight option.
- **BMAD**: More of a workflow methodology layer than an orchestrator.

### Important Ecosystem Note
Anthropic blocked Claude Pro and Max subscribers from using most third-party agent frameworks. API users are unaffected. This affects Gas Town, Ruflo, etc.

### Prompt Injection into Running Sessions
No native API exists for injecting prompts into a running Claude Code session. There are open GitHub issues requesting a local socket or named pipe approach. The practical workaround is `tmux send-keys -t "$TARGET_PANE" -l "PROMPT"` followed by Enter. For multiline prompts, `tmux load-buffer` with `paste-buffer` is needed since `send-keys` breaks on newlines.

---

## Core Architecture: agent-ledger

### What agent-ledger Is
A global, harness-agnostic inter-agent pub/sub event bus backed by a SQLite database, with a CLI as the sole interface. All agents, watchers, and hooks interact with agent-ledger exclusively through the CLI — the DB is an implementation detail.

### What agent-ledger Is Not
- Not a task decomposition system
- Not an autonomous orchestrator
- Not Claude-specific (designed to support future adapters for Copilot, Codex, etc.)
- Not a replacement for the existing `worklog` Obsidian logging tool

### Naming
**agent-ledger** is the project and repo name. A ledger records transactions (events) from multiple parties (agents), is append-only by convention, is the authoritative source of truth, and implies both writing and reading — making it semantically accurate for what this system does. Individual tools within the project:

- **ledger** — the CLI tool for interacting with the event database. Reads naturally as CLI subcommands: `ledger subscribe`, `ledger status`, `ledger tail`, `ledger health`. The DB lives at `~/.agent-ledger/ledger.db`.
- **watcher** — the external event polling scripts (GitHub watcher, Jira watcher, etc.)
- **conductor** — the overseer/orchestration layer; implies directing without micromanaging, fitting the relationship between the high-level view and individual sessions

"Check the ledger" is the natural instruction in skills and CLAUDE.md. "It's a shared ledger that all your agent sessions write to and read from" explains the system clearly to newcomers.

---

## Event Log Design

### Single Global JSONL → SQLite
After considering per-worktree JSONL files and a single combined JSONL, the decision converged on a **single global SQLite database**. Rationale:
- Multiple concurrent writers with safe concurrent write handling
- Real SQL filtering, indexing, and joins
- No flock hacks needed
- Supports pagination, archiving, and complex subscription queries
- Single file, easy to back up and inspect via CLI

### Key Schema Fields (to be finalized with Opus)
Every event should have at minimum:
- `id` — unique event ID
- `ts` — ISO 8601 UTC timestamp of when the event occurred
- `external_ts` — for external events, when the external event actually happened (vs. when the watcher logged it)
- `from` — origin of the event: `{session_id, branch, repo, session_name?}` for agent events; `{source: "github"|"jira"|"ledger-watcher"}` for external events
- `to` — array of typed recipients (see Routing section below); absent means not routed to any session by default (stored for aggregation only)
- `type` — event type (see Event Types section)
- `title` — short human-readable one-liner for UI list views
- `body` — full event content; can be prose, markdown, or structured text
- `resources` — array of external resource references this event relates to: `{type: "pr"|"jira"|"slack"|..., id: <URL or identifier>}`
- `severity` / `urgency` — for UI highlighting and notification priority
- `tags` — for flexible filtering
- `source` — top-level: `"agent"`, `"git"`, `"github"`, `"jira"`, `"slack"`, `"ledger"`, etc.

### Two Companion Files Per Session (Not Per Worktree)
In addition to the ledger DB, each Claude Code session has:
- `summary.md` — Claude Code's internal session memory file, updated periodically. Path: `~/.claude/projects/<project-hash>/<session-id>/session-memory/summary.md`. Overwritten on update, not appended. Useful for current state, not history. Should be accessed via `ledger session-summary --worktree <branch>` CLI wrapper in case the path changes.
- ledger DB — the authoritative historical record and event bus

### What Goes in the Ledger vs. Obsidian Worklog
The existing `worklog` tool logs to Obsidian on specific events (PR creation, etc.) as defined in CLAUDE.md. This continues unchanged for now. agent-ledger is the machine-readable event layer; Obsidian worklog is the human-readable narrative layer. They serve different audiences. A later phase may connect significant ledger events to the Obsidian worklog, or phase it out, but this is not a current concern.

---

## Event Types

To be fully designed with Opus, but known types include:

**Agent-originated:**
- `milestone` — significant progress event (root cause found, plan finalized, approach decided)
- `status` — periodic status report with freeform body
- `blocked` / `unblocked` — session waiting for input, review, or external dependency
- `decision` — rationale record ("chose approach A over B because X")
- `handoff` — explicit continuation note for the next session on this worktree
- `followup` — identified necessary follow-up work
- `pre_compact_snapshot` — summary written before context compaction
- `session_start` / `session_end` — lifecycle events
- `active` / `idle` — heartbeat state machine events

**External (from watchers):**
- `pr_review_comment` — someone left a review comment
- `pr_review_requested` / `pr_approved` / `pr_merged` / `pr_closed` / `pr_reopened`
- `pr_new_commits` — new commits pushed to a PR being reviewed
- `ci_pass` / `ci_fail`
- `jira_comment` / `jira_status_change` / `jira_assigned`
- `slack_mention` (future)
- `email_thread` (future)

**System:**
- `subscribe` / `unsubscribe` — subscription changes
- `worktree_created` / `worktree_removed`
- `message` — explicit inter-session message

---

## Routing and Pub/Sub Model

### Default Behavior
Status updates and milestone logs are **not broadcast by default**. They are stored in the ledger for aggregation by the overseer or UI, but do not route to any other session unless explicitly addressed. This keeps the system quiet and prevents accidental feedback loops.

### Typed Recipients in the `to` Field
```json
"to": [
  {"type": "session", "id": "abc123"},
  {"type": "worktree", "branch": "feature-auth", "repo": "myrepo"},
  {"type": "session_name", "name": "auth-refactor"},
  {"type": "broadcast"}
]
```
Sessions are identified by a coordinate tuple: `(repo, branch, session_name?, session_id?)`. `session_id` is an opaque string (not assumed to be a Claude UUID) for harness-agnosticism. Session name is preferred when present as it is human-meaningful and survives session restarts.

### Resource-Based Subscriptions
Sessions declare interest in external resources via a `subscriptions` table:
```
subscriptions(worktree_branch, repo, resource_type, resource_id, created_at, deleted_at)
```
When an external watcher writes an event referencing a resource, agent-ledger routes it to all sessions subscribed to that resource. More than one session can subscribe to the same resource.

### Unread Query Logic
An event is unread for a session if:
- `ts > session_cursor.last_seen_ts` AND
- (`to` explicitly addresses this session OR `to` is absent and the event is a broadcast OR the event references a resource this session subscribes to)

### Session Opt-Out
A session can opt out of agent-ledger entirely (no sending or receiving) via a flag in `session_metadata` or a skill call. Useful for sessions where the overhead is unwanted.

### Explicit Inter-Session Messaging
Sessions do not talk to each other by default. A skill can explicitly wire two sessions to communicate by writing events with appropriate `to` fields. The overseer session can also send messages to specific sessions by name.

---

## Subscription Management

### Subscriptions Table
```
subscriptions(id, worktree_branch, repo, resource_type, resource_id, created_at, deleted_at)
```
Soft deletes (`deleted_at`) preserve history — unsubscription stops new event routing but retains the record.

### Auto-Subscribe Signals
- Branch-to-PR mapping (strong signal, auto-subscribe)
- Existing `.worktree-env` file contents on worktree init (bootstrap seed)
- Session mentions a Jira ticket ID in a milestone (weaker signal, possibly auto-subscribe)

### Auto-Unsubscribe Triggers
- PR merged, closed (watcher writes close event AND soft-deletes subscription in same transaction)
- Jira ticket reaches terminal status (resolved, done, won't fix, etc.)
- Worktree removed (WorktreeRemove hook cleans up all subscriptions for that worktree)
- Reopened PRs: reinstate subscription on reopen event

### Dormancy and Cleanup
- Subscriptions whose worktree has no session activity in 7 days go **dormant** — watchers stop polling their resources
- Dormant subscriptions reactivate instantly on `SessionStart` heartbeat
- Hard cleanup at 30 days of inactivity
- Watcher query for active resources:
```sql
SELECT DISTINCT resource_id FROM subscriptions s
WHERE deleted_at IS NULL
AND EXISTS (
  SELECT 1 FROM session_cursors c
  WHERE c.worktree = s.worktree AND c.repo = s.repo
  AND c.last_active > datetime('now', '-7 days')
)
```

### Integration with Existing Worktree Script
The existing worktree management script checks for PR association and stores it in `.worktree-env`. The integration pattern:
```bash
if command -v ledger &>/dev/null; then
  ledger subscribe --worktree "$BRANCH" --repo "$REPO" --resource "pr:$PR_URL"
fi
```
Zero impact for users without agent-ledger. The worktree script remains portable and usable by others. A `ledger init-worktree` command seeds subscriptions from `.worktree-env` for migration.

---

## Per-Session Cursors

Each session tracks its own last-seen timestamp independently. This enables sessions that were offline to catch up on missed events on resume.

```
session_cursors(session_id, worktree_branch, repo, session_name, last_seen_ts, last_active)
```

The `SessionStart` hook (including the compact matcher for resume) reads this cursor and queries for all events since `last_seen_ts` to generate a catch-up summary.

---

## Hook Integration

### Relevant Claude Code Hooks

**`SessionStart`** (including compact/resume matcher)
- Load session identity into context
- Update `last_active` in `session_cursors`
- Query ledger for events missed since `last_seen_ts`
- Surface catch-up summary: "While you were away: 2 PR review comments, CI failed, 1 message from overseer"

**`UserPromptSubmit`**
- Check ledger for unread events addressed to this session
- Prepend them to the prompt context if present
- Update cursor after delivery

**`FileChanged`** (on sentinel file — see below)
- Triggered when new events are written to the ledger
- Hook script queries ledger for events relevant to this session since its cursor
- Surfaces notification if relevant unread events found

**`PreCompact`**
- Write a `pre_compact_snapshot` event to ledger with current session summary
- Preserves what the session was doing even after compaction erases context

**`PostCompact`**
- Reload essential context from ledger and `summary.md`

**`Stop`**
- Optionally write `idle` state event to ledger

**`WorktreeCreate`** / **`WorktreeRemove`**
- Bootstrap / teardown ledger subscriptions for the worktree
- Note: author uses a custom worktree script rather than Claude's built-in worktree management; both should be supported

**`Notification`**
- Detect permission-waiting state and write `blocked` event to ledger so overseer can see it

### The Sentinel File Pattern
SQLite in WAL mode does not reliably trigger filesystem change events on macOS (FSEvents does not fire on `-wal` file writes). Solution: the ledger CLI `write` path always does `touch ~/.agent-ledger/ledger.trigger` after every DB insert. Claude Code `FileChanged` hooks watch the sentinel file, not the DB directly. The sentinel file contains the timestamp or row ID of the last insert, so the hook script can use it as a cursor without maintaining separate state.

Per-session cursors still exist for catch-up on resume — the sentinel is only for live notification of currently-running sessions.

### `asyncRewake`
An `asyncRewake: true` option on async hooks is designed to allow background hooks to interrupt an idle session with new context. As of the brainstorming session this feature has a known bug (output renders as visible `<system-reminder>` XML in the terminal instead of being silently injected). **Check whether this is fixed before building around it.** If fixed, it could replace some of the sentinel + `UserPromptSubmit` pattern for more immediate interruption.

### Hook Exit Codes
Exit code 2 blocks an action. Exit code 1 (or any other non-zero) is non-blocking. Developers commonly reach for exit 1 by Unix convention — ledger hook scripts must use exit 2 for policy enforcement.

---

## Status Line Integration

Claude Code supports a `statusLine` setting that runs a shell script on a configurable refresh interval and displays output at the bottom of the terminal. The agent-ledger status line widget:
- Queries ledger for unread events addressed to this session (fast SQLite query)
- Displays a compact summary: `📬 2 unread (1 review, 1 CI fail) | feature-auth | sonnet | 34%`
- Updates in near-realtime as watchers write new events
- Two-stage UX: status line shows count passively while typing → `UserPromptSubmit` hook prepends full event details on submit

The count in the status line and the events in `UserPromptSubmit` should use the same ledger CLI command with different output format flags (`--format status|full`) to stay in sync.

---

## External Event Watchers

### Design Principles
- **Polling, not persistent watching** — cron jobs or launchd plists running periodically. Simpler, self-healing, no process to babysit.
- **Stateless** — the ledger DB is the state. On each run, find the most recent event from this source for each resource, use its timestamp as the cursor, query the external API for everything after that.
- **Catch-up on start** — check current resource state on startup, not just deltas. Handles the case where the watcher was offline when a PR was merged.
- **Watcher reads subscriptions** — the subscriptions table is the contract between the worktree world and the watcher world. The GitHub watcher queries active subscriptions to know which PRs to poll.
- **Auto-unsubscribe on terminal states** — watcher writes close event AND soft-deletes subscription in same transaction when PR merges, Jira ticket resolves, etc.
- **`external_ts` field** — the time the external event actually happened, separate from `fetched_at` (when the watcher logged it). Timeline reconstruction should sort by `external_ts`.

### Planned Watchers
- GitHub PR watcher (reviews, comments, CI status, new commits, merge/close)
- Jira watcher (comments, status changes, assignments)
- Slack watcher (future)
- Email watcher (future)
- Git commit hook (appends commit events to the ledger for timeline anchoring)

---

## Overseer Session

A Claude Code session (not a persistent process) that can be spun up on demand to answer high-level questions. It is ephemeral — spin up, ask a question, get an answer, let it die. **The ledger DB and `summary.md` files are the memory, not the session.**

### "What's going on in my sessions?"
A skill that spawns a subagent per active worktree. Each subagent:
1. Reads that worktree's `summary.md` for current state
2. Queries ledger for events from the last N hours
3. Returns a synthesized summary to the overseer
4. Overseer assembles the cross-session narrative

### "What should I work on right now?"
Runs a set of targeted reads in parallel:
- Unaddressed external events (ledger query)
- Sessions in `blocked` / waiting state (ledger query)
- Sessions with unread high-urgency events (ledger query)
- Upcoming Jira backlog (live Jira query)
- Slack/email follow-ups (future, live queries)

Two modes: "go respond to something" (driven by unread ledger events — most common) and "what to tackle next" (driven by overseer reasoning — for when nothing is urgently waiting).

### "What changed since last time I checked?"
The overseer writes its own cursor to ledger after each check. Next invocation queries only events after that cursor. One line of state, no database beyond ledger.

---

## Session Status Dashboard (CLI and UI)

### `ledger status` Command
Shows all active worktrees with unread event counts:
```
feature-auth      3 unread  (pr_review, ci_fail)     2 min ago
bugfix-payments   1 unread  (jira_comment)            1 hr ago
feature-search    —
```
Primary "where should I be right now" view. Routing logic must account for both `to` field matching AND resource subscription matching.

### `ledger health`
Shows all subscriptions with active/dormant/expired status, last activity, and whether the underlying resource is still open. Hygiene view.

### `ledger tail`
Live view of the event stream.

### `ledger screen --worktree <branch>`
Captures the tmux pane for the given worktree (see tmux section).

---

## tmux Integration (Optional)

tmux is optional but adds useful capabilities if present. agent-ledger degrades gracefully when tmux is absent.

### Pane Discovery (Not Storage)
tmux pane IDs are ephemeral — they change when sessions are killed and resumed in new panes. agent-ledger does not store pane IDs. Instead, `ledger screen` does dynamic discovery at query time:
```bash
tmux list-panes -a -F "#{pane_id} #{pane_current_path} #{pane_current_command}" \
  | grep claude \
  | grep "/path/to/worktree"
```

### Pane Title Convention
A `SessionStart` hook sets the tmux pane title if tmux is active:
```bash
if command -v tmux &>/dev/null && [ -n "$TMUX" ]; then
  printf '\033]2;ledger:%s:%s\033\\' "$BRANCH" "$SESSION_NAME"
fi
```
This makes discovery a clean title match rather than path heuristics, and survives moving panes around.

### Session State Detection
Hooks cover known state transitions (active, idle, blocked). Tmux pane capture provides ground truth for ambiguous states — `ledger screen` can pipe the captured pane through a quick API call to get a one-line interpretation ("waiting for Bash tool approval" vs "idle at prompt").

---

## Session Metadata Table

A `session_metadata` table tracks point-in-time state rather than historical events:
```
session_metadata(session_id, worktree_branch, repo, session_name, display_name, 
                 last_active, current_status, ledger_opt_out, summary_path,
                 tmux_pane_title, created_at)
```
`summary_path` points to the Claude Code `summary.md` file for this session. Accessed via `ledger session-summary` wrapper. `ledger_opt_out` flag for sessions that don't want ledger participation.

---

## Harness Agnosticism

agent-ledger is designed to eventually support non-Claude agents (Copilot, Codex, etc.).

### Where Claude-Specifics Currently Exist
- Session ID format (Claude UUIDs are opaque strings in agent-ledger's model)
- `summary.md` path (wrapped behind CLI)
- Hook system (Claude Code specific; other harnesses need adapters)
- `${CLAUDE_SESSION_ID}` substitution in skills (Claude Code specific)

### The Abstraction Boundary
The ledger CLI is the sole interface. Agent-specific details are isolated to:
- An adapter that maps the harness's native session identity to agent-ledger's coordinate system
- An adapter that translates the harness's hook/extension system into ledger CLI calls
- A harness-specific "skill" equivalent

The DB schema, watcher scripts, UI, and routing logic are fully harness-agnostic.

### Session Identity
`session_id` is treated as an opaque string — never assumed to be a Claude UUID. Full worktree coordinate: `(repo, branch, session_name?, session_id?)`.

---

## Web UI (Later Phase)

Design the schema with the UI as a first-class reader.

### Phase 1: Raw Event Log
- Timeline view of all events with filtering by worktree, source, type, date range
- Unread inbox per session
- Each session card showing `summary.md` content + ledger metadata (unread count, last active, subscribed resources)

### Phase 2: Session Overview
- High-level status per session
- Current state (active/idle/blocked)
- Associated resources

### Phase 3: Subscription Graph Visualization
- Visual graph of sessions, resources, and subscription relationships
- Event flows between nodes
- Requires enough metadata on each node for rendering without N+1 external API calls

### Schema Implications for UI
- `title` field (one-liner for list views)
- `severity`/`urgency` (for highlighting)
- `session_metadata` table (fast session card rendering)
- Materialized or well-indexed latest-status-per-session query
- `subscriptions` table with resource metadata for graph rendering

---

## Implementation Phasing

### Phase 1: Observability
- Design and lock the Ensemble DB schema
- Build the ledger CLI (Python, wrapping SQLite; collection of scripts sharing helpers)
- Implement `ledger status`, `ledger tail`, `ledger health`, `ledger init-worktree`
- Implement per-session cursors and `SessionStart` catch-up hook
- Implement `PreCompact` snapshot hook
- Implement `Stop` idle state hook
- Implement sentinel file + `FileChanged` hook for live notification
- Implement status line widget showing unread count
- Seed subscriptions from `.worktree-env` via `ledger init-worktree`
- Add optional agent-ledger integration to existing worktree script

### Phase 2: External Event Watchers
- GitHub PR watcher (polling via cron/launchd)
- Jira watcher
- Git commit hook
- Auto-subscribe/unsubscribe on PR and ticket state changes
- Dormancy logic for inactive worktrees

### Phase 3: Overseer Skills
- "What's going on" skill (subagent per worktree, reads summary.md + ledger)
- "What should I work on" skill (multi-source parallel query)
- "What changed since last check" skill (cursor-based)
- Inter-session messaging via skills

### Phase 4: Web UI
- Raw event log with filters
- Unread inbox per session
- Session status cards

### Phase 5: Advanced
- Subscription graph visualization
- asyncRewake-based interruption (if bug is fixed)
- Slack/email watcher adapters
- Non-Claude harness adapters
- Obsidian worklog integration or replacement

---

## Open Questions for Opus Session

1. **Exact DB schema** — full table definitions, indexes, how `to`/`resources`/`subscriptions` relate. This is the highest-leverage design decision.
2. **ledger CLI structure** — single Python script vs. collection of scripts with shared helpers. Python is preferred over shell for SQLite library access.
3. **Global vs. per-repo DB** — decided global for now, but revisit if cross-repo event bleed becomes a problem.
4. **`asyncRewake` bug status** — check whether fixed before designing notification flow around it.
5. **Claude built-in worktree management compatibility** — author uses custom script; agent-ledger should support both.
6. **Schema for `to` field** — JSON column in SQLite vs. separate `recipients` junction table.
7. **Schema for `resources` field** — JSON column vs. separate `event_resources` junction table.
8. **Dormancy threshold** — 7 days chosen; validate this feels right.
9. **`ledger screen` tmux pane capture** — exact disambiguation strategy when multiple Claude sessions are in the same worktree directory.
10. **Skill distribution** — shared `~/.claude/skills/agent-ledger/` directory for skills used across all worktrees.

---

## Key Design Principles (Summary)

- **agent-ledger DB is the memory, not any session** — sessions are ephemeral; Ensemble is durable
- **CLI as the sole interface** — storage is an implementation detail, swappable behind the ledger CLI
- **Stateless watchers** — agent-ledger DB is the state; watchers carry none of their own
- **Polling over persistent watching** — simpler, self-healing, tolerant of stops and starts
- **Opt-in inter-session communication** — sessions don't talk to each other by default
- **Status updates not broadcast by default** — agent-ledger is a passive ledger unless explicitly addressed
- **Graceful degradation** — tmux optional, agent-ledger optional in worktree script, everything has a fallback
- **Harness agnostic** — session IDs are opaque strings; Claude-specifics are isolated to adapters
- **Primary key is (repo, branch), not session ID** — session ID is a queryable field, not the organizing concept; session name preferred as human identifier when present
- **Two files per session** — agent-ledger DB for events/routing, `summary.md` for current state snapshot
- **Observability before communication** — build the passive logging and aggregation layer first; active inter-session messaging is a later phase
