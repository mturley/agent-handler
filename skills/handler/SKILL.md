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
  prompt: "MANDATORY: You MUST call the Bash tool to run: handler log --global --since-cursor --agent-only --json 2>/dev/null. NEVER skip this Bash call. Also run handler unread --count to check for direct messages. If there are new events or direct messages, summarize them. For direct messages, present them as action items. If no events, say 'No new events.'"
```

5. Tell the user what they can ask.

## What the user can ask

- "What's going on?" → `handler triage --json`
- "What changed since last time?" → `handler log --global --since-cursor --json`
- "What should I work on?" → `handler triage --json`, reason about priorities
- "Tell the auth session about X" → `handler emit --type message --title "X" --to <target>`
- "Show me everything about PR #123" → `handler resource history pr:owner/repo#123`
- "Which sessions are related to X?" → `handler resource related --session <id>`
- "What is session X doing?" → spawn subagent with `handler peek --session <id> --json`
- "Check on all sessions" → peek at each peekable session via subagents, summarize

Use `handler <command> --help` for flag details on any command.

## Peeking at sessions

When you need to understand what a session is doing (stuck, waiting for approval, idle):

1. Check `handler status --json` — look for peekable sessions (`"peekable": true`)
2. For each session you want to inspect, spawn a subagent
3. The subagent runs `handler peek --session <id> --json` and interprets the raw terminal content
4. The subagent returns a 1-2 sentence summary: what the session appears to be doing

**Important:** Always use subagents for peek — raw captures can be hundreds of lines and will flood your context. Each subagent distills the capture to a short summary.

**When to peek:**
- Sessions that appear stuck (active but no recent heartbeat)
- Sessions that are blocked
- When the user asks "what's session X doing?"
- During triage, for sessions with unread events they haven't processed

## Idempotent

Re-invoking /handler re-runs triage and refreshes. If the cron job already exists, don't create a duplicate.
