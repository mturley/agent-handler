---
name: inbox
description: Check and act on unread events from the agent-handler ledger
---

# /inbox — Check Unread Events

**MANDATORY: You MUST call the Bash tool to run the command below. NEVER skip the Bash call. NEVER assume there are no messages without running the command first. Even if previous checks returned nothing, you MUST run the command every time.**

## Usage

First, determine the right flags:

- **`--auto`** (e.g. `/inbox --auto`): add `--agent-only` so only the agent cursor advances
- **Handler session** (role is `handler`): add `--global` to see ALL events across all sessions, not just events targeted at this session

Combine flags as needed. Examples:

```bash
# Regular session, manual invocation
handler unread --ack --json 2>/dev/null

# Regular session, auto invocation
handler unread --ack --agent-only --json 2>/dev/null

# Handler session, manual invocation
handler unread --ack --global --json 2>/dev/null

# Handler session, auto invocation
handler unread --ack --agent-only --global --json 2>/dev/null
```

## After reading events

- If the output is `null` or empty: say "No new messages."
- Otherwise: present the events in a clear summary grouped by type, and for each actionable event suggest what to do about it (e.g. "There's a PR review comment — want me to look at it and address the feedback?")

## Formatting references as clickable links

When mentioning Jira issues or GitHub PRs in your summary, always render them as markdown links so they're clickable:

- **GitHub PR**: `[owner/repo#123](https://github.com/owner/repo/pull/123)` — extract owner/repo from event titles
- **Jira**: `[PROJECT-123](https://<jira-host>/browse/PROJECT-123)` — use the Jira host from your conversation context (e.g. CLAUDE.md, MCP config); if unknown, check `resource_url` fields in triage/subscription data

## Available CLI commands for deeper queries

- `handler log` — event timeline for this session
- `handler status --json` — all sessions with liveness and unread counts
- `handler resource history <resource_id>` — all events for a resource
- `handler query "<sql>"` — arbitrary read-only SQL for ad-hoc analysis
- `handler schema` — dump table definitions before writing SQL
