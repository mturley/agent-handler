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

## Emitting events

```
handler emit --type <type> --title "..." [--body "..."] [--to <target>] [--broadcast] [--tags "a,b"]
```

Event types: `milestone`, `status`, `decision`, `blocked`, `unblocked`, `handoff`, `followup`, `message`.

| Trigger | Type | Example title |
|---------|------|---------------|
| Finish a commit or meaningful code change | `milestone` | "Implemented retry logic for API client" |
| Push to remote | `milestone` | "Pushed feature branch auth-refactor" |
| Find the root cause of a bug | `milestone` | "Root cause: token validated against wrong key" |
| Choose between approaches | `decision` | "Chose RS256 over HS256 for token signing" |
| Get stuck on something outside this session | `blocked` | "Need review on PR #42 before proceeding" |
| Resume after being blocked | `unblocked` | "PR #42 approved, continuing" |
| Identify work for another session or later | `handoff` / `followup` | "Tech debt: error messages need i18n" |
| Want to tell another session something | `message` | (use `--to <target>`) |
| Starting a new phase, or periodic check-in | `status` | "Still debugging token refresh — narrowed to middleware" |

Use `--body` for details beyond what fits in the title. Use `--tags` for lightweight categorization.

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

The `--to` flag on `handler emit` accepts session names, branch names, or session UUIDs. To send a message to the handler session, use `--to handler`.
