#!/usr/bin/env bash
# Status line script for agent-handler
# Receives session JSON on stdin from Claude Code

if ! command -v handler &>/dev/null; then
    exit 0
fi

# Read session data from stdin
SESSION_DATA=$(cat)
SESSION_ID=$(echo "$SESSION_DATA" | python3 -c "import sys,json; print(json.load(sys.stdin).get('session_id',''))" 2>/dev/null)

if [ -z "$SESSION_ID" ]; then
    # Fall back to PID cache
    SESSIONS_DIR="${HANDLER_HOME:-$HOME/.agent-handler}/data/sessions"
    CLAUDE_PID="$PPID"
    if [ -f "${SESSIONS_DIR}/${CLAUDE_PID}" ]; then
        SESSION_ID=$(cat "${SESSIONS_DIR}/${CLAUDE_PID}")
    fi
fi

if [ -z "$SESSION_ID" ]; then
    exit 0
fi

OUTPUT=$(handler statusline --session "$SESSION_ID" 2>/dev/null)
if [ -z "$OUTPUT" ]; then
    exit 0
fi

# If session is not registered, try to register in the background
if echo "$OUTPUT" | grep -q "not registered"; then
    SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
    (
        source "${SCRIPT_DIR}/common.sh"
        discover_and_register "$PPID" >/dev/null 2>&1
    ) &
fi

# Extract unread count from output for notification
if [ -n "$SESSION_ID" ]; then
    UNREAD_COUNT=$(echo "$OUTPUT" | grep -oP '● \K\d+(?= unread)' 2>/dev/null || echo "0")
    if [ "$UNREAD_COUNT" -gt 0 ] 2>/dev/null; then
        # Build notification message from the output
        NOTIFY_MSG=$(echo "$OUTPUT" | head -1 | sed 's/.*● //' | sed 's/\x1b\[[0-9;]*m//g')
        handler notify --session "$SESSION_ID" --count "$UNREAD_COUNT" --message "$NOTIFY_MSG" 2>/dev/null &
    else
        handler notify --session "$SESSION_ID" --count 0 2>/dev/null &
    fi
fi

echo "$OUTPUT"
