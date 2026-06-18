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

## CLI commands you should know

Use `handler <command> --help` for full flag details on any command.

### Emitting events

Record significant events so other sessions and the handler can see what happened:
```
handler emit --type <type> --title "..." [--body "..."] [--to <target>]
```
Types: `milestone`, `decision`, `blocked`, `unblocked`, `handoff`, `followup`, `status`

### Messaging other sessions

The `--to` flag accepts session names, branch names, or session UUIDs:
```
handler emit --type message --title "Check the auth types" --to feature-auth
```

### Subscribing to resources

```
handler subscribe --resource "pr:owner/repo#123" --url "https://github.com/owner/repo/pull/123"
handler unsubscribe --resource "pr:owner/repo#123"
```
Resource format is always `--resource "type:id"`. Supported types: `pr`, `jira`.

### Querying

```
handler status                       # all sessions with liveness and unread
handler watching                     # this session's subscriptions + watcher health
handler watching --global            # all subscriptions across all sessions
handler log                          # event timeline for this session
handler tail                         # live event stream
handler query "SELECT ..."           # read-only SQL against the ledger
handler schema                       # dump table definitions
```
