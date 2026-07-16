---
name: awaiting
description: "agent-handler: Switch to the first session awaiting approval (cmux)"
---

# /awaiting — Switch to a Session Awaiting Approval

Switch to the first session that is waiting for user approval. Requires cmux.

## Usage

Run the following command:

```bash
handler switch --first-awaiting
```

If the command fails with "cmux is not installed", tell the user: "Session switching requires cmux."

If no sessions are awaiting approval, tell the user: "No sessions are currently awaiting approval."

If the command succeeds, tell the user which session was switched to. They can use `/awaiting` again to cycle through additional sessions that need attention, or `/switch <name>` to switch to a specific session.
