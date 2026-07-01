---
name: using-handler
description: "Use when starting any session — loads awareness of agent-handler capabilities. agent-handler is installed and active: it tracks your sessions, watches external resources (PRs, Jira issues), and provides an inbox for cross-session messaging. This skill teaches you the available commands and skills."
---

# agent-handler

You are running inside a session managed by agent-handler. It provides:
- **Event ledger** — record milestones, decisions, blockers, and handoffs visible to other sessions
- **Watchers** — GitHub PRs and Jira issues are polled for changes and delivered to your inbox
- **Cross-session messaging** — send messages to other active sessions by name or branch
- **Subscriptions** — watch external resources and get notified of changes

Your statusline shows inbox status, inbox mode, and watched resources.

To turn this session into a handler (command center for all sessions), use `/handler`.

## Emitting events

**You MUST use `handler emit` proactively throughout your session.** Don't wait to be asked — emit events whenever something significant happens. Other sessions and the handler rely on these events to understand what's going on across the system.

Emit when you:
- Complete a meaningful unit of work → `milestone`
- Make an architectural or design choice → `decision`
- Get stuck or need something from outside this session → `blocked` / `unblocked`
- Identify work that should happen in another session or later → `handoff` / `followup`
- Want to communicate with another session → `message` (use `--to`)
- Want to record current progress → `status`

```
handler emit --type <type> --title "..." [--body "..."] [--to <target>] [--broadcast] [--tags "a,b"]
```

## CLI usage

**Before running any `handler` command, check the CLI help for correct syntax.** Do not guess at subcommand names, argument positions, or flags. The CLI is the source of truth — this skill is just an overview.

```
handler --help                    # list all commands
handler <command> --help          # flags and usage for a specific command
```

### Key commands

These are the commands you'll use most often. Run `--help` on each for exact flag syntax.

- `/handler` — turn this session into a command center for all sessions
- `handler emit` — record events (milestone, decision, blocked, handoff, message, etc.)
- `handler subscribe` / `handler unsubscribe` — watch or unwatch external resources (add `--persist` to also update `.worktree-resources` for future sessions)
- `handler status` — all sessions with liveness and unread counts
- `handler watching` — this session's subscriptions + watcher health
- `handler log` — event timeline for this session
- `handler tail` — live event stream
- `handler query "SELECT ..."` — read-only SQL against the ledger
- `handler schema` — dump table definitions

### Auto inbox mode and the dual cursor

In auto mode, a cron job polls for unread events and delivers them via `/inbox`. To help the user see what happened while they were away, the system tracks two cursors:

- **Agent cursor** — what the agent has processed (advances when events are acked)
- **Human cursor** — what the user has seen (advances when the user sends a prompt)

The gap between them appears in the statusline as "N auto-delivered since last prompt". When the user sends a prompt, the human cursor catches up automatically. Manual `/inbox` invocations advance both cursors.

When `/inbox` is invoked by the cron job, events are acked normally (advancing the agent cursor), and the auto-delivered count grows. When the user returns and sends a prompt, the count resets to zero.

### Resource format

Resources are identified as `type:id`. Supported types: `pr`, `jira`.
Examples: `pr:owner/repo#123`, `jira:PROJECT-456`

### Messaging other sessions

The `--to` flag on `handler emit` accepts session names, branch names, or session UUIDs. To send a message to the handler session, use `--to handler`.
