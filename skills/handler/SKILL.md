---
name: handler
description: "Turn this session into the handler — a command center for managing all active sessions. Use when you want a global view of all sessions, events, and resources."
---

# /handler — Handler Session

## CLI discovery — never guess

Before running any `handler` command for the first time in a session, run `handler --help` to learn the available commands. Before using a command's flags for the first time, run `handler <command> --help` to learn its flags. **Never invent commands or flags** — if you're unsure whether a command or flag exists, check `--help` first. The CLI is the source of truth; this skill intentionally does not duplicate the command reference.

Key commands you'll use (verify with `--help`):
- `handler status` — list all sessions (not `handler sessions` or `handler list`)
- `handler log --global --since-cursor` — show new events AND advance the cursor (there is no separate advance command)

## On invocation

1. Run `handler --help` to learn available commands, then set this session's role (if not already set):
```bash
handler --help
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

**All** sessions from `handler status --json` — not just sessions with subscriptions. Group sessions by repo. Within each repo, show: name, branch, display state, last active (relative, e.g. "5m ago"), peek summary. Where a session has subscribed resources (from triage `session_resources`), include their current state (priority, status, review decision, CI status). Sessions without subscriptions still appear.

### Formatting references as clickable links

When mentioning Jira issues or GitHub PRs anywhere in the briefing, always render them as markdown links:

- **GitHub PR**: `[owner/repo#123](https://github.com/owner/repo/pull/123)` — extract owner/repo from event data
- **Jira**: `[PROJECT-123](https://<jira-host>/browse/PROJECT-123)` — use the Jira host from your conversation context (e.g. CLAUDE.md, MCP config); if unknown, check `resource_url` fields in triage data

5. The cursor is already advanced — `handler log --since-cursor` advances it as a side effect when it runs in step 2.

6. Tell the user what they can ask.

## What the user can ask

- "What's going on?" → re-run the full briefing (steps 2-4 above)
- "What changed since last time?" → `handler log --global --since-cursor --json`
- "What should I work on?" → re-run triage, reason about priorities using resource state
- "Tell session X about Y" → `handler emit --type message --title "Y" --to <target>`
- "Show me everything about PR #123" → `handler resource history pr:owner/repo#123`
- "Which sessions are related to X?" → `handler resource related --session <id>`
- "What is session X doing?" → spawn a subagent with `handler peek --session <id> --json`
- "Check on all sessions" → peek at each peekable session via subagents, summarize

When you need a command or flag not listed above, run `handler <command> --help` — don't guess.

## Peeking at sessions

Always use subagents for peek — raw captures can be hundreds of lines and will flood your context. Each subagent distills the capture to a short summary.

Use Haiku for peek subagents — the task is focused (detect permission prompts/questions) and fast.

**When to peek:**
- During every briefing (step 3 above)
- When the user asks about a specific session
- Sessions that appear stuck, blocked, or idle

## Idempotent

Re-invoking /handler re-runs the full briefing.
