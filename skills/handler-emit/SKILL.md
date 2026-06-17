---
name: handler-emit
description: Write an event to the agent-handler ledger
---

# /handler-emit — Emit an Event

Record significant events to the ledger so other sessions and the handler can see what happened.

## Event Types

| Type | When to use |
|------|-------------|
| `milestone` | Significant progress (root cause found, plan finalized, approach decided) |
| `status` | Periodic status update |
| `blocked` | Waiting for input, review, or external dependency |
| `unblocked` | Blocker resolved |
| `decision` | Rationale record ("chose approach A over B because X") |
| `handoff` | Continuation note for the next session on this worktree |
| `followup` | Identified follow-up work needed |

## Usage

```bash
handler emit \
    --type milestone \
    --title "Found root cause of auth bug" \
    --body "The issue is in the session middleware — tokens are being validated against the wrong key."
```

Use `--tags` for lightweight categorization: `--tags "auth,security"`.
