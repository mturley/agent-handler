---
name: reminder
description: "agent-handler: Set a reminder for this session. The reminder will appear in your inbox on the next /inbox check."
---

# /reminder — Remind This Session

Emit a `reminder` event targeted at this session. It will appear in the inbox on the next check.

## Usage

The arguments describe what to remember. Examples:
- `/reminder check CI status on PR #42`
- `/reminder follow up with Katie about the review`
- `/reminder update the Jira issue when the fix is merged`

## Steps

1. Get your session name:
```bash
handler session-name
```

2. Emit the reminder:
```bash
handler emit --type reminder --title "<what to remember>"
```

The reminder targets the current session automatically (no `--to` needed since it's emitted from this session's context).

3. Tell the user: "Reminder set. It will appear in your inbox on the next check."
