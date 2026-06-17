---
name: inbox-mode
description: Set how this session receives unread events — manual, on-submit, or auto
---

# /inbox-mode — Configure Inbox Mode

Three modes control how you receive unread events:

| Mode | Behavior |
|------|----------|
| `manual` (default) | Status line shows unread count. You check with `/inbox` when ready. |
| `on-submit` | You are notified of new messages on each prompt submit. |
| `auto` | A cron job polls for new events and invokes /inbox when messages arrive. |

## Switching to manual or on-submit

1. If the current mode is auto, use CronList to find the inbox polling cron job and CronDelete to remove it.

2. Set the mode:
```bash
handler configure --inbox-mode <mode>
```

## Switching to auto

1. Set the mode:
```bash
handler configure --inbox-mode auto
```

2. Create a session-scoped cron job (default: every 1 minute):
```
CronCreate with:
  cron: "*/1 * * * *"
  durable: false
  recurring: true
  prompt: "Check handler unread --count. If the count is greater than 0, invoke /inbox."
```

The cron job is session-scoped and will not survive session restarts. Inbox mode resets to manual when the session ends.
