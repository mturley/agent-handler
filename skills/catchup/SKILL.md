---
name: catchup
description: "agent-handler: Summarize events auto-delivered while the user was away, then advance the human cursor"
---

# /catchup — Catch Up on Auto-Delivered Events

The user wants a summary of what happened while they were away. Events were auto-delivered to your inbox by the cron job.

## Step 1: Summarize

Look back through your conversation history for `/inbox --auto` results and your responses to them since the last `/catchup` you performed (or since auto inbox mode was enabled if there hasn't been a `/catchup`). Summarize the events grouped by type, including what you did in response to each (if anything). For actionable events that weren't addressed, suggest what to do.

If there are no auto-delivered events to summarize, say "Nothing to catch up on — you're all caught up."

## Step 2: Advance the cursor

After summarizing, run:

```bash
handler catch-up-human-cursor
```

This advances the human cursor to match the agent cursor, resetting the auto-delivered count in the statusline.
