---
name: inbox_mode
description: Set how this session receives unread events — manual, on-submit, or auto
---

# /inbox_mode — Configure Inbox Mode

Three modes control how you receive unread events:

| Mode | Behavior |
|------|----------|
| `manual` (default) | Status line shows unread count. You check with `/inbox` when ready. |
| `on-submit` | You are notified of new messages on each prompt submit. |
| `auto` | A cron job polls for new events and invokes /inbox when messages arrive. |

## Switching to manual or on-submit

1. If the current mode is auto, clean up the cron job first:
```bash
CRON_ID=$(handler configure --get cron-job-id)
```
If CRON_ID is not empty, use CronDelete to delete that job.

2. Set the mode:
```bash
handler configure --inbox-mode <mode>
```

## Switching to auto

1. Set the mode:
```bash
handler configure --inbox-mode auto
```

2. Create a durable cron job (default: every 1 minute):
```
CronCreate with:
  cron: "*/1 * * * *"
  durable: true
  recurring: true
  prompt: "Check handler unread --count. If the count is greater than 0, invoke /inbox."
```

3. Store the returned job ID:
```bash
handler configure --cron-job-id <job-id>
```
