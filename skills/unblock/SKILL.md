---
name: unblock
description: "agent-handler: Mark this session as unblocked. Use when a blocker has been resolved and work can resume."
---

# /unblock — Mark Session as Unblocked

Emit an `unblocked` event to indicate the blocker has been resolved.

## Usage

The arguments describe what was resolved. Examples:
- `/unblock PR #42 approved`
- `/unblock CI is green now`
- `/unblock RHOAIENG-12345 was resolved`

## Steps

1. Compose and emit:

```bash
handler emit --type unblocked --title "<what was resolved>"
```

Use the arguments passed to `/unblock` as the title. If no arguments were provided, ask the user what was resolved.

2. Tell the user: "Blocker cleared — session is no longer marked as blocked."
