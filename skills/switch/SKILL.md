---
name: switch
description: "agent-handler: Switch to another session by name (cmux)"
---

# /switch — Switch to Another Session

Switch the cmux workspace and surface to another session by name.

## Usage

If invoked with arguments (e.g. `/switch agent-handler-impl`), run:

```bash
handler switch <session-name>
```

If invoked with no arguments, run `handler status` to show available sessions, then ask the user which session to switch to.

If the command fails with "cmux is not installed", tell the user: "Session switching requires cmux."

If it fails with "session not found", show the error and suggest running `handler status` to see available sessions.
