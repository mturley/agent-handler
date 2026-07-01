---
name: handler
description: "Turn this session into the handler — a command center for managing all active sessions. Use when you want a global view of all sessions, events, and resources."
---

# /handler — Handler Session

## On invocation

1. Set this session's role:
```bash
handler configure --role handler
```

2. Load current state:
```bash
handler triage --json
```

3. Present a narrative summary of:
   - Active sessions and any blocked
   - Sessions with unread events
   - Watcher health
   - Events since last check

4. Set up a polling loop (check CronList first — skip if already exists):
```
CronCreate:
  cron: "*/1 * * * *"
  durable: false
  recurring: true
  prompt: "Run handler log --global --since-cursor --json. Also run handler unread --count to check for direct messages. If there are new events or direct messages, summarize them. For direct messages, present them as action items."
```

5. Tell the user what they can ask.

## What the user can ask

- "What's going on?" → `handler triage --json`
- "What changed since last time?" → `handler log --global --since-cursor --json`
- "What should I work on?" → `handler triage --json`, reason about priorities
- "Tell the auth session about X" → `handler emit --type message --title "X" --to <target>`
- "Show me everything about PR #123" → `handler resource history pr:owner/repo#123`
- "Which sessions are related to X?" → `handler resource related --session <id>`

Use `handler <command> --help` for flag details on any command.

## Idempotent

Re-invoking /handler re-runs triage and refreshes. If the cron job already exists, don't create a duplicate.
