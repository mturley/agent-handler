---
name: done
description: "Log a completion summary to the ledger before closing the session. Use when the session's work is finished and you're about to close it."
---

# /done — Session Complete

Emit a milestone summarizing what this session accomplished, so the handler knows the session ended intentionally (not mid-task).

## On invocation

1. Look back through the conversation for work completed since the last milestone event. Summarize it in 2-4 sentences covering: what was done, key decisions made, and any follow-up items.

2. Emit the milestone:
```bash
handler emit --type milestone --title "Session complete: <one-line summary>" --body "<detailed summary>" --broadcast
```

3. Tell the user:
```
Summary logged to the ledger. You can close this session now.
```

## If the user continues working

If the user sends another task after `/done` was invoked, emit a milestone indicating the session is resuming:

```bash
handler emit --type milestone --title "Session resuming: <brief description of new task>" --broadcast
```

Then proceed with the new task normally.
