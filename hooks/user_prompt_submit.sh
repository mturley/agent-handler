#!/usr/bin/env bash
# UserPromptSubmit hook for agent-handler
# Bumps heartbeat. If inbox mode is on-submit, injects unread events.
set -euo pipefail

if ! command -v handler &>/dev/null; then
    exit 0
fi

CLAUDE_PID="$PPID"
SESSIONS_DIR="${HOME}/.agent-handler/data/sessions"

if [ -f "${SESSIONS_DIR}/${CLAUDE_PID}" ]; then
    SESSION_ID=$(cat "${SESSIONS_DIR}/${CLAUDE_PID}")
else
    exit 0
fi

handler heartbeat --session-id "$SESSION_ID"

INBOX_MODE=$(handler configure --session-id "$SESSION_ID" --get inbox-mode 2>/dev/null || echo "manual")

if [ "$INBOX_MODE" = "on-submit" ]; then
    UNREAD=$(handler unread --session-id "$SESSION_ID" --json 2>/dev/null)
    if [ -n "$UNREAD" ] && [ "$UNREAD" != "[]" ] && [ "$UNREAD" != "null" ]; then
        echo "$UNREAD"
        handler ack --session-id "$SESSION_ID"
    fi
fi

if [ "$INBOX_MODE" = "auto" ]; then
    echo "Inbox mode is auto but polling may not be active. Run /inbox_mode auto to restart."
fi
