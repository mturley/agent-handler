---
name: inbox_mode
description: Set how this session receives unread events — manual, on-submit, or auto
---

# /inbox_mode — Configure Inbox Mode

Three modes control how you receive unread events:

| Mode | Behavior |
|------|----------|
| `manual` (default) | Status line shows unread count. You check with `/inbox` when ready. |
| `on-submit` | Unread events auto-injected on each prompt submit. |
| `auto` | Actively poll for new events on an interval and proactively surface them. |

## Usage

To set the mode, run:
```bash
handler configure --session-id <your-session-id> --inbox-mode <mode>
```

For auto mode with a custom interval:
```bash
handler configure --session-id <your-session-id> --inbox-mode auto --auto-poll-interval 60
```

## Auto mode

When setting auto mode, start a polling loop that periodically runs `/inbox`:
- Use ScheduleWakeup or /loop to check every N seconds (default 60)
- On each poll, run `handler unread --session-id <id> --json`
- If there are unread events, present them and offer to act
- Acknowledge after presenting: `handler ack --session-id <id>`
