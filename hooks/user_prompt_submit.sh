#!/usr/bin/env bash
# UserPromptSubmit hook for agent-handler
# Registers if needed, bumps heartbeat, optionally injects unread events.
# IMPORTANT: No output unless inbox mode is on-submit. All other operations are silent.
set -euo pipefail

if ! command -v handler &>/dev/null; then
    exit 0
fi

CLAUDE_PID="$PPID"
SESSIONS_DIR="${HANDLER_HOME:-$HOME/.agent-handler}/data/sessions"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Discover the actual current session ID from the JSONL
source "${SCRIPT_DIR}/common.sh"
ACTUAL_SESSION_ID=$(discover_session_id 2>/dev/null || echo "")

# Check PID cache
CACHED_SESSION_ID=""
if [ -f "${SESSIONS_DIR}/${CLAUDE_PID}" ]; then
    CACHED_SESSION_ID=$(cat "${SESSIONS_DIR}/${CLAUDE_PID}")
fi

# Register if: no cache, or cache points to wrong session
if [ -z "$CACHED_SESSION_ID" ] || [ "$CACHED_SESSION_ID" != "$ACTUAL_SESSION_ID" ]; then
    if [ -n "$ACTUAL_SESSION_ID" ]; then
        # Don't resurrect archived sessions — they were intentionally unregistered
        STATUS=$(handler query "SELECT status FROM sessions WHERE session_id='${ACTUAL_SESSION_ID}'" 2>/dev/null | tail -1)
        if [ "$STATUS" = "archived" ]; then
            exit 0
        fi
        discover_and_register "$CLAUDE_PID" >/dev/null 2>&1 || true
        if [ -f "${SESSIONS_DIR}/${CLAUDE_PID}" ]; then
            SESSION_ID=$(cat "${SESSIONS_DIR}/${CLAUDE_PID}")
        else
            exit 0
        fi
    else
        exit 0
    fi
else
    SESSION_ID="$CACHED_SESSION_ID"
fi

# Heartbeat in background, fully silenced
handler heartbeat --session-id "$SESSION_ID" >/dev/null 2>&1 &

# Only inject output if inbox mode is on-submit
INBOX_MODE=$(handler configure --session-id "$SESSION_ID" --get inbox-mode 2>/dev/null || echo "manual")

if [ "$INBOX_MODE" = "on-submit" ]; then
    UNREAD=$(handler unread --session-id "$SESSION_ID" --ack --json 2>/dev/null)
    if [ -n "$UNREAD" ] && [ "$UNREAD" != "[]" ] && [ "$UNREAD" != "null" ]; then
        echo "--- agent-handler: injected unread events (inbox mode: on-submit) ---"
        echo "$UNREAD"
        echo "--- end injected events ---"
    fi
fi
