---
name: using-handler
description: "Invoke once per session before starting work — loads agent-handler context for emitting events, messaging, and CLI usage. If you haven't invoked this skill yet in this session, do so now."
---

# agent-handler

You are running inside a session managed by agent-handler. It provides:
- **Event ledger** — record milestones, decisions, blockers, and handoffs visible to other sessions
- **Watchers** — GitHub PRs and Jira issues are polled for changes and delivered to your inbox
- **Cross-session messaging** — send messages to other active sessions by name or branch
- **Subscriptions** — watch external resources and get notified of changes

Your statusline shows inbox status, inbox mode, and watched resources.

To turn this session into a handler (command center for all sessions), use `/handler`.

## Emitting events — MANDATORY

**You MUST call `handler emit` regularly throughout your session. This is not optional.**

The entire purpose of agent-handler is a centralized record of what all sessions are doing. A session that doesn't emit is invisible — the user and other sessions have no idea what you're working on, what progress you've made, or whether you're stuck. **Silent sessions are broken sessions.**

Do not wait until work is "done" to emit. Emit as you go:
- Starting a task? Emit `status`.
- Made progress? Emit `milestone`.
- Hit a wall? Emit `blocked`.
- Made a choice? Emit `decision`.
- Periodic check-in on where you are? Emit `status`.

If you've been working for a while without emitting, you're overdue. **When in doubt, emit.**

### Syntax

```
handler emit --type <type> --title "..." [--body "..."] [--to <target>] [--broadcast] [--tags "a,b"]
```

### Event types and triggers

| Trigger | Type | Example title |
|---------|------|---------------|
| Starting work on a task | `status` | "Beginning auth middleware refactor" |
| Finish a commit or meaningful code change | `milestone` | "Implemented retry logic for API client" |
| Push to remote | `milestone` | "Pushed feature branch auth-refactor" |
| Find the root cause of a bug | `milestone` | "Root cause: token validated against wrong key" |
| Choose between approaches | `decision` | "Chose RS256 over HS256 for token signing" |
| Get stuck on something outside this session | `blocked` | "Need review on PR #42 before proceeding" |
| Resume after being blocked | `unblocked` | "PR #42 approved, continuing" |
| Identify work for another session or later | `handoff` / `followup` | "Tech debt: error messages need i18n" |
| Want to tell another session something | `message` | (use `--to <target>`) |
| Periodic check-in on current work | `status` | "Still debugging token refresh — narrowed to middleware" |

Use `--body` for details beyond what fits in the title. Use `--tags` for lightweight categorization.

## Subscribing to resources — MANDATORY

**When you create a PR or Jira issue, you MUST subscribe to it immediately** so watchers can deliver updates (reviews, comments, status changes) to your inbox.

```
handler subscribe --resource "pr:owner/repo#123" --url "https://github.com/owner/repo/pull/123"
handler subscribe --resource "jira:PROJECT-456" --url "https://your-jira.atlassian.net/browse/PROJECT-456"
```

Also subscribe when you start working on an existing PR or Jira issue. Use `/handler-subscribe` for full syntax and persistence options.

## CLI usage

**Before running any `handler` command, check the CLI help for correct syntax.** Do not guess at subcommand names, argument positions, or flags. The CLI is the source of truth — this skill is just an overview.

```
handler --help                    # list all commands
handler <command> --help          # flags and usage for a specific command
```

### Key commands

These are the commands you'll use most often. Run `--help` on each for exact flag syntax.

- `/handler` — turn this session into a command center for all sessions
- `handler triage` — aggregates what needs attention: sessions, resources, blockers, unread events with resource state
- `handler emit` — record events (milestone, decision, blocked, handoff, message, etc.)
- `handler subscribe` / `handler unsubscribe` — watch or unwatch external resources (add `--persist` to also update `.worktree-resources` for future sessions)
- `handler status` — all sessions with liveness and unread counts
- `handler watching` — this session's subscriptions + watcher health
- `handler log` — event timeline for this session
- `handler tail` — live event stream
- `handler query "SELECT ..."` — read-only SQL against the ledger
- `handler schema` — dump table definitions
- `handler claude` — start Claude in a peekable terminal (use instead of bare `claude`)
- `handler peek --session <id>` — capture terminal content of another session

### Auto inbox mode and the dual cursor

In auto mode, a cron job polls for unread events and delivers them via `/inbox`. To help the user see what happened while they were away, the system tracks two cursors:

- **Agent cursor** — what the agent has processed (advances when events are acked)
- **Human cursor** — what the user has seen (advances when the user sends a prompt)

The gap between them appears in the statusline as "N auto-delivered since last prompt". When the user sends a prompt, the human cursor catches up automatically. Manual `/inbox` invocations advance both cursors.

When `/inbox` is invoked by the cron job, events are acked normally (advancing the agent cursor), and the auto-delivered count grows. When the user returns and sends a prompt, the count resets to zero.

**Session resume cleanup:** If you see a `/inbox --auto` cron running (check with CronList) but inbox mode is not `auto` (check with `handler configure --get inbox-mode`), delete the stale cron with CronDelete. This can happen when a session is resumed after being closed in auto mode.

### Resource format

Resources are identified as `type:id`. Supported types: `pr`, `jira`.
Examples: `pr:owner/repo#123`, `jira:PROJECT-456`

### Messaging other sessions

Use `/message` to send messages to other sessions. It handles `handler emit --type message` with proper sender identification so recipients can reply. The `--to` flag accepts session names, branch names, or session UUIDs. To message the handler session, use `--to handler`.
