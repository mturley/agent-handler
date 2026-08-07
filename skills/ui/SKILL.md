---
name: ui
description: "agent-handler: Open the handler web dashboard focused on this session."
disable-model-invocation: true
---

# /ui — Open This Session in the Web Dashboard

Open a browser to the handler web UI's single-session page for the current
session, showing its inbox, timeline, and resources. Useful in cmux to keep a
dedicated browser pane pointed at this session.

## Steps

Run:

```bash
handler ui-open
```

With no argument, `ui-open` targets the current session automatically. It
detects whether the dev server (port 5173) or prod server (port 8420) is
running and opens the right URL.

If it reports that no UI server is running, tell the user to start one with
`handler ui` in another terminal (or `make dev` for development), then run
`/ui` again.

## Notes

- The page opens on the Inbox tab by default.
- To open a *different* session's page, pass its name or ID:
  `handler ui-open <session-name>`.
