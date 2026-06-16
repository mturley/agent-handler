#!/usr/bin/env bash
# SessionEnd hook for agent-handler
# Archives the session and soft-deletes subscriptions when Claude exits.
set -euo pipefail

if ! command -v handler &>/dev/null; then
    exit 0
fi

CLAUDE_PID="$PPID"
SESSIONS_DIR="${HANDLER_HOME:-$HOME/.agent-handler}/data/sessions"

if [ -f "${SESSIONS_DIR}/${CLAUDE_PID}" ]; then
    SESSION_ID=$(cat "${SESSIONS_DIR}/${CLAUDE_PID}")
else
    exit 0
fi

handler unregister --session-id "$SESSION_ID" >/dev/null 2>&1 || true
