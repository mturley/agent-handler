---
name: using-handler
description: "CLI reference for agent-handler. Use proactively: invoke this skill when you see handler skills in your statusline (/inbox, /inbox-mode, /watching) or when the user asks about watched resources, inbox, or messaging other sessions."
---

# agent-handler CLI Reference

agent-handler manages sessions, events, subscriptions, and external resource watchers across Claude Code sessions.

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
