---
name: using-handler
description: "Use when starting any session — loads awareness of agent-handler capabilities. agent-handler is installed and active: it tracks your sessions, watches external resources (PRs, Jira issues), and provides an inbox for cross-session messaging. This skill teaches you the available commands and skills."
---

# agent-handler

You are running inside a session managed by agent-handler. It provides:
- **Inbox** — receive messages from other sessions and notifications from watched resources
- **Watchers** — GitHub PRs and Jira issues are polled for changes and delivered to your inbox
- **Cross-session messaging** — send messages to other active sessions by name or branch
- **Subscriptions** — watch external resources and get notified of changes

Your statusline shows your inbox status, inbox mode, and watched resources. Use the skills and commands below to interact with handler.

## Skills available

- `/inbox` — check and respond to unread events
- `/inbox-mode` — switch between manual, on-submit, and auto delivery
- `/watching` — show watched resources, watcher status, and errors

## Key commands

### Events
```
handler unread --ack --json          # read and acknowledge unread events
handler unread --count               # just the count
handler emit --type <type> --title "..." [--body "..."] [--to <target>]
handler emit --type message --title "Hello" --to <session-name-or-branch>
```

### Subscriptions
```
handler subscribe --resource "pr:owner/repo#123" --url "https://github.com/owner/repo/pull/123"
handler subscribe --resource "jira:RHOAIENG-456" --url "https://redhat.atlassian.net/browse/RHOAIENG-456"
handler unsubscribe --resource "pr:owner/repo#123"
```
Resource format is always `--resource "type:id"`. Supported types: `pr`, `jira`.

### Session info
```
handler status                       # all sessions with liveness and unread
handler watching                     # this session's subscriptions + watcher health
handler watching --global            # all subscriptions across all sessions
handler whoami                       # print this session's ID
handler configure --inbox-mode <mode>  # set manual, on-submit, or auto
```

### Messaging other sessions
The `--to` flag accepts session names, branch names, or session UUIDs:
```
handler emit --type message --title "Check the auth types" --to feature-auth
handler emit --type message --title "Done with refactor" --to agent-handler-planning
```

### Querying
```
handler log                          # event timeline for this session
handler tail                         # live event stream
handler query "SELECT ..."           # read-only SQL
handler schema                       # dump table definitions
```
