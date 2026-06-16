---
name: handler_unregister
description: Archive this session and soft-delete its subscriptions before quitting
---

# /handler_unregister — Unregister Session

Use this when you're done with a session and want to cleanly archive it. This:
- Archives the session (removes from default `handler status` output)
- Soft-deletes all active subscriptions
- Emits a `session_end` event
- Cleans up the PID cache

## Usage

```bash
handler unregister --session-id $(handler whoami)
```

## When to use

When the user says they're done with this session, wrapping up, or explicitly wants to close out the session's handler tracking. The session can always be resumed later with `claude --resume` — re-registering will restore it to active status.
