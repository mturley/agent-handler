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
    JSONL_PATH=$(echo "$SESSION_DATA" | python3 -c "import sys,json; print(json.load(sys.stdin).get('transcript_path',''))" 2>/dev/null)
    if [ -n "$SESSION_ID" ] && [ -n "$JSONL_PATH" ]; then
        (
            BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
            REPO=$(git remote get-url origin 2>/dev/null | sed 's/.*github.com[:/]//' | sed 's/\.git$//' || echo "unknown")
            SESSION_NAME=$(echo "$SESSION_DATA" | python3 -c "import sys,json; print(json.load(sys.stdin).get('session_name',''))" 2>/dev/null)
            REGISTER_ARGS=(--session-id "$SESSION_ID" --branch "$BRANCH" --repo "$REPO" --pid "$PPID" --jsonl-path "$JSONL_PATH")
            if [ -n "${CMUX_SURFACE_ID:-}" ]; then
                REGISTER_ARGS+=(--terminal-type cmux --terminal-id "$CMUX_SURFACE_ID")
            fi
            if [ -n "$SESSION_NAME" ]; then
                REGISTER_ARGS+=(--session-name "$SESSION_NAME")
            fi
            handler register "${REGISTER_ARGS[@]}" >/dev/null 2>&1
        ) &
    fi
fi

# Sync session name if it changed (statusline gets the current name from Claude)
CURRENT_NAME=$(echo "$SESSION_DATA" | python3 -c "import sys,json; print(json.load(sys.stdin).get('session_name',''))" 2>/dev/null)
if [ -n "$CURRENT_NAME" ] && [ -n "$SESSION_ID" ]; then
    handler heartbeat --session-id "$SESSION_ID" --session-name "$CURRENT_NAME" >/dev/null 2>&1 &
fi

# Extract unread count from output for notification
if [ -n "$SESSION_ID" ]; then
    UNREAD_COUNT=$(echo "$OUTPUT" | grep -o '● [0-9]* unread' 2>/dev/null | grep -o '[0-9]*' || echo "0")
    if [ "$UNREAD_COUNT" -gt 0 ] 2>/dev/null; then
        # Build notification message from the output
        NOTIFY_MSG=$(echo "$OUTPUT" | head -1 | sed 's/.*● //' | sed 's/\x1b\[[0-9;]*m//g')
        handler notify --session "$SESSION_ID" --count "$UNREAD_COUNT" --message "$NOTIFY_MSG" 2>/dev/null &
    else
        handler notify --session "$SESSION_ID" --count 0 2>/dev/null &
    fi
fi

echo "$OUTPUT"
