---
name: block
description: "agent-handler: Mark this session as blocked. Use when the session is waiting on something external — a PR review, CI results, a Jira blocker, a Slack reply, or a dependency from another session."
---

# /block — Mark Session as Blocked

Emit a `blocked` event to indicate this session is waiting on something external.

## Usage

The arguments describe what you're blocked on. Examples:
- `/block waiting for review on PR #42`
- `/block CI is failing, need to investigate`
- `/block depends on RHOAIENG-12345 being resolved`

## Steps

1. Compose and emit:

```bash
handler emit --type blocked --title "<what you're blocked on>"
```

Use the arguments passed to `/block` as the title. If no arguments were provided, ask the user what they're blocked on.

2. Tell the user: "Marked as blocked. Use `/unblock` when the blocker is resolved."
