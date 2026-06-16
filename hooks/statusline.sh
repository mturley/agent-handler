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

# Bell on new unreads: track last count, bell only when it increases
BELL_FILE="${HANDLER_HOME:-$HOME/.agent-handler}/data/.statusline-last-count-${SESSION_ID}"
CURRENT_COUNT=$(echo "$OUTPUT" | head -1 | grep -oE '[0-9]+ unread' | grep -oE '[0-9]+' || echo "0")
LAST_COUNT=$(cat "$BELL_FILE" 2>/dev/null || echo "0")

if [ "$CURRENT_COUNT" -gt "$LAST_COUNT" ] 2>/dev/null; then
    printf '\a'
fi
echo "$CURRENT_COUNT" > "$BELL_FILE" 2>/dev/null

echo "$OUTPUT"
