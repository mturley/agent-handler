---
name: done
description: "agent-handler: Log a completion summary to the ledger before closing the session. Use when the session's work is finished and you're about to close it."
---

# /done — Session Complete

Emit a milestone summarizing what this session accomplished, so the handler knows the session ended intentionally (not mid-task).

## On invocation

1. Look back through the conversation for work completed since the last milestone event. Summarize it in 2-4 sentences covering: what was done, key decisions made, and any follow-up items.

2. Emit the milestone:
```bash
handler emit --type milestone --title "Session complete: <one-line summary>" --body "<detailed summary>" ```

3. Unwatch all watched resources:
```bash
handler watching --json
```
For each active subscription, unsubscribe:
```bash
handler unsubscribe --resource "<type>:<id>"
```

4. Tell the user:
```
Summary logged to the ledger. Unwatched N resource(s). You can close this session now.
```

## If the user continues working

If the user sends another task after `/done` was invoked:

1. Before starting work, list the resources that were unwatched during `/done` (from your conversation history) and ask the user if they want to re-subscribe to any of them.

2. Emit a milestone indicating the session is resuming:
```bash
handler emit --type milestone --title "Session resuming: <brief description of new task>" ```

3. Proceed with the new task normally.
