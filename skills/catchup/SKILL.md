---
name: catchup
description: Summarize events auto-delivered while the user was away, then advance the human cursor
---

# /catchup — Catch Up on Auto-Delivered Events

The user wants a summary of what happened while they were away. Events were auto-delivered to your inbox by the cron job.

Look back through your conversation history for `/inbox --auto` results since the last real user prompt (not counting `/inbox --auto` cron prompts). Summarize the events grouped by type. For actionable events, suggest what to do.

If there are no auto-delivered events to summarize, say "Nothing to catch up on — you're all caught up."
