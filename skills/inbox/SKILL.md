---
name: inbox
description: Check and act on unread events from the agent-handler ledger
---

# /inbox — Check Unread Events

## Usage

```bash
handler unread --session-id $(handler whoami) --json
```

## After reading events

1. Present the events to the user in a clear summary, grouped by type
2. Offer to act on actionable events (e.g. "Want me to look at that PR review comment?")
3. After the user has seen the events, mark them as read: `handler ack --session-id $(handler whoami)`

## Available CLI commands for deeper queries

- `handler log --session-id $(handler whoami)` — event timeline for this session
- `handler status --json` — all sessions with liveness and unread counts
- `handler resource history <resource_id>` — all events for a resource
- `handler query "<sql>"` — arbitrary read-only SQL for ad-hoc analysis
- `handler schema` — dump table definitions before writing SQL
