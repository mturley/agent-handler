---
name: catchup
description: Summarize events auto-delivered while the user was away, then advance the human cursor
---

# /catchup — Catch Up on Auto-Delivered Events

The user wants a summary of what happened while they were away. Events were auto-delivered to your inbox by the cron job — look back through your conversation history for the `/inbox --auto` results and summarize them.

## Steps

1. Look back through your recent conversation for any `/inbox --auto` results that contain events (not "No new messages"). Summarize what happened, grouped by type.

2. After summarizing, advance the human cursor so the statusline count resets:
```bash
handler heartbeat --catch-up-human-cursor
```

If there are no auto-delivered events to summarize, just say "Nothing to catch up on — you're all caught up."
