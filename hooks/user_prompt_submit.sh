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

# Check PID cache for session ID
if [ -f "${SESSIONS_DIR}/${CLAUDE_PID}" ]; then
    SESSION_ID=$(cat "${SESSIONS_DIR}/${CLAUDE_PID}")
else
    # Not registered yet — try to register now (handles the case where
    # SessionStart fired before Claude created the JSONL file)
    SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
    source "${SCRIPT_DIR}/common.sh"
    if discover_and_register "$CLAUDE_PID" >/dev/null 2>&1; then
        if [ -f "${SESSIONS_DIR}/${CLAUDE_PID}" ]; then
            SESSION_ID=$(cat "${SESSIONS_DIR}/${CLAUDE_PID}")
        else
            exit 0
        fi
    else
        exit 0
    fi
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
