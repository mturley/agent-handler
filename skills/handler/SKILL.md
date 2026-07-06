---
name: handler
description: "Turn this session into the handler — a command center for managing all active sessions. Use when you want a global view of all sessions, events, and resources."
---

# /handler — Handler Session

## On invocation

1. Set this session's role (if not already set):
```bash
handler configure --role handler
```

2. Gather data (run both in parallel):
```bash
handler triage --json
handler log --global --since-cursor --json
```

3. Peek at all peekable sessions — for each session in the triage output with `"peekable": true` and `display_state` of `"active"` or `"idle"`, spawn a Haiku subagent that:
   - Runs `handler peek --session <session_id> --json`
   - Answers: "Is this session waiting for user input (permission prompt, question, or approval)? If yes, what exactly is it asking? If no, say 'working' or 'idle at prompt'."
   - Returns a 1-2 sentence summary

4. Present a prioritized briefing with three sections:

### Action Items

Ordered by priority. Use your judgment to rank items, but default to this order:

1. **Sessions waiting for input** — permission prompts, questions, approval requests (from peek results). Sessions working on higher-priority resources (Blocker/Critical Jira issues, PRs with failing CI) rank higher.
2. **Blocked sessions** — from triage `blocked_sessions`
3. **Unread external events** — PR reviews with changes requested, new comments on your PRs, Jira status changes. Derive what needs attention from triage `sessions_with_unread` combined with `session_resources` state.
4. **Stale resources** — from triage `stale_resources`, where watcher data couldn't be refreshed

Weight priority by resource importance: a session working on a Blocker/Critical Jira issue ranks higher than one on a Normal issue. A PR with `ci_status: "FAILURE"` or `review_decision: "CHANGES_REQUESTED"` ranks higher than one with passing CI.

### Timeline

Chronological list of events since last report (from `handler log --global --since-cursor`). Group by session, showing milestones, decisions, status updates, and external events.

### Session Overview

Table of all sessions with: name, branch, display state, peek summary, subscribed resources with their current state (priority, status, review decision, CI status).

5. Advance the cursor after presenting.

6. Set up a polling loop (check CronList first — skip if already exists):
```
CronCreate:
  cron: "*/1 * * * *"
  durable: false
  recurring: true
  prompt: "MANDATORY: You MUST call the Bash tool to run: handler log --global --since-cursor --agent-only --json 2>/dev/null. NEVER skip this Bash call. Also run handler unread --count to check for direct messages. If there are new events or direct messages, summarize them. For direct messages, present them as action items. If no events, say 'No new events.'"
```

7. Tell the user what they can ask.

## What the user can ask

- "What's going on?" → re-run the full briefing (steps 2-4 above)
- "What changed since last time?" → `handler log --global --since-cursor --json`
- "What should I work on?" → re-run triage, reason about priorities using resource state
- "Tell session X about Y" → `handler emit --type message --title "Y" --to <target>`
- "Show me everything about PR #123" → `handler resource history pr:owner/repo#123`
- "Which sessions are related to X?" → `handler resource related --session <id>`
- "What is session X doing?" → spawn a subagent with `handler peek --session <id> --json`
- "Check on all sessions" → peek at each peekable session via subagents, summarize

Use `handler <command> --help` for flag details on any command.

## Peeking at sessions

Always use subagents for peek — raw captures can be hundreds of lines and will flood your context. Each subagent distills the capture to a short summary.

Use Haiku for peek subagents — the task is focused (detect permission prompts/questions) and fast.

**When to peek:**
- During every briefing (step 3 above)
- When the user asks about a specific session
- Sessions that appear stuck, blocked, or idle

## Idempotent

Re-invoking /handler re-runs the full briefing. If the cron job already exists, don't create a duplicate.
