---
name: inbox
description: Check and act on unread events from the agent-handler ledger
---

# /inbox — Check Unread Events

Run `handler unread --session-id <your-session-id> --json` to check for unread events addressed to this session.

## Finding your session ID

Your session ID is stored in the PID cache. Run:
```bash
cat ~/.agent-handler/sessions/$PPID
```
If that file doesn't exist, discover it from the JSONL:
```bash
ls -t ~/.claude/projects/-$(pwd | sed 's/\//-/g' | sed 's/^-//')/*.jsonl | head -1 | xargs basename | sed 's/.jsonl//'
```

## After reading events

1. Present the events to the user in a clear summary, grouped by type
2. Offer to act on actionable events (e.g. "Want me to look at that PR review comment?")
3. After the user has seen the events, acknowledge them: `handler ack --session-id <id>`

## Available CLI commands for deeper queries

- `handler log --session-id <id>` — event timeline for this session
- `handler status --json` — all sessions with liveness and unread counts
- `handler resource history <resource_id>` — all events for a resource
- `handler query "<sql>"` — arbitrary read-only SQL for ad-hoc analysis
- `handler schema` — dump table definitions before writing SQL
