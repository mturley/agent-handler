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

## Usage

Set the mode with:
```bash
handler configure --inbox-mode <mode>
```

## Switching to auto mode

After running the configure command, set up a durable cron job to poll for messages:

```
CronCreate with:
  cron: "*/<interval-in-minutes> * * * *"  (e.g. "*/1 * * * *" for every minute)
  durable: true
  recurring: true
  prompt: "Check handler unread --count. If the count is greater than 0, invoke /inbox."
```

Save the returned job ID by storing it with:
```bash
handler emit --type handler_cron_id --title "<job-id>" --tags "inbox_auto_cron"
```

This stores the cron job ID in the ledger so it can be found later for cleanup.

## Switching away from auto mode

When switching from auto to manual or on-submit:

1. Find the cron job ID:
```bash
handler query "SELECT title FROM events WHERE type='handler_cron_id' AND tags LIKE '%inbox_auto_cron%' ORDER BY ts DESC LIMIT 1"
```

2. Delete the cron job using CronDelete with that ID.

3. Set the new mode:
```bash
handler configure --inbox-mode <mode>
```

## Default interval

If the user doesn't specify an interval, default to every 1 minute.
