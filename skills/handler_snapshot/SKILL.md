---
name: handler_snapshot
description: Write a pre-compaction snapshot to preserve session context
---

# /handler_snapshot — Snapshot Current State

Write a `pre_compact_snapshot` event to the ledger capturing what this session is currently doing. This preserves context that would otherwise be lost when the conversation is compacted.

## When to use

- Before context compaction (called automatically by the PreCompact hook)
- Manually, when you want to checkpoint progress
- Before a long pause in work

## Usage

```bash
handler emit \
    --session-id $(handler whoami) \
    --type pre_compact_snapshot \
    --title "Snapshot: implementing auth middleware refactor" \
    --body "Currently on step 3 of 5. Completed: session validation, token refresh. Remaining: error handling, tests. Blocked on: nothing. Key decision: using RS256 instead of HS256."
```

## What to include in the body

- Current task and progress
- What's been completed
- What remains
- Any blockers
- Key decisions made and their rationale
