---
name: inbox
description: Check and act on unread events from the agent-handler ledger
---

# /inbox — Check Unread Events

**MANDATORY: You MUST call the Bash tool to run the command below. NEVER skip the Bash call. NEVER assume there are no messages without running the command first. Even if previous checks returned nothing, you MUST run the command every time.**

## Usage

If invoked with `--auto` (e.g. `/inbox --auto`), use `--agent-only` so only the agent cursor advances:
```bash
handler unread --ack --agent-only --json 2>/dev/null
```

Otherwise (manual invocation), advance both cursors:
```bash
handler unread --ack --json 2>/dev/null
```

## After reading events

- If the output is `null` or empty: say "No new messages."
- Otherwise: present the events in a clear summary grouped by type, and for each actionable event suggest what to do about it (e.g. "There's a PR review comment — want me to look at it and address the feedback?")

## Available CLI commands for deeper queries

- `handler log` — event timeline for this session
- `handler status --json` — all sessions with liveness and unread counts
- `handler resource history <resource_id>` — all events for a resource
- `handler query "<sql>"` — arbitrary read-only SQL for ad-hoc analysis
- `handler schema` — dump table definitions before writing SQL
