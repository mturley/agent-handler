---
name: handler-debug
description: "Debug session identity and inbox state. Use when the statusline shows unread messages but /inbox returns nothing, or other session identity mismatches."
---

# /handler-debug — Debug Session Identity

Run these commands and report the output exactly. This reveals how the CLI resolves session identity from within a tool call, which may differ from the statusline hook.

```bash
echo "=== Session Resolution ==="
handler whoami 2>&1
echo ""
echo "=== Unread Count ==="
handler unread --count 2>&1
echo ""
echo "=== Cursor ==="
handler query "SELECT session_id, last_seen_ts, human_seen_ts FROM session_cursors WHERE session_id = (SELECT session_id FROM sessions WHERE pid = $PPID AND status = 'active' LIMIT 1)" 2>&1
echo ""
echo "=== PID Info ==="
echo "PPID=$PPID"
echo "CLAUDE_PID=${CLAUDE_PID:-unset}"
ls -la ~/.agent-handler/data/sessions/ 2>&1 | grep "$$\|$PPID" || echo "No PID cache match for $$ or $PPID"
echo ""
echo "=== Active Sessions ==="
handler query "SELECT session_id, session_name, pid, status FROM sessions WHERE status = 'active' ORDER BY last_active DESC LIMIT 10" 2>&1
```

Compare the session ID shown here with the one in the statusline debug output (`[debug] id=...`). If they differ, that's the root cause — the statusline and the CLI are resolving to different sessions.
