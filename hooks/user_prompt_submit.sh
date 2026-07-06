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

# Read session ID from stdin JSON (provided by Claude Code)
HOOK_STDIN=$(cat)
SESSION_ID=$(echo "$HOOK_STDIN" | python3 -c "import sys,json; print(json.load(sys.stdin).get('session_id',''))" 2>/dev/null)

if [ -z "$SESSION_ID" ]; then
    # Fallback to PID cache if stdin doesn't have session_id
    if [ -f "${SESSIONS_DIR}/${CLAUDE_PID}" ]; then
        SESSION_ID=$(cat "${SESSIONS_DIR}/${CLAUDE_PID}")
    else
        exit 0
    fi
fi

# Register if not yet registered (PID cache miss)
if [ ! -f "${SESSIONS_DIR}/${CLAUDE_PID}" ] || [ "$(cat "${SESSIONS_DIR}/${CLAUDE_PID}")" != "$SESSION_ID" ]; then
    JSONL_PATH=$(echo "$HOOK_STDIN" | python3 -c "import sys,json; print(json.load(sys.stdin).get('transcript_path',''))" 2>/dev/null)
    if [ -n "$JSONL_PATH" ]; then
        source "${SCRIPT_DIR}/common.sh"
        BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
        REPO=$(git remote get-url origin 2>/dev/null | sed 's/.*github.com[:/]//' | sed 's/\.git$//' || echo "unknown")

        TERMINAL_TYPE=""
        TERMINAL_ID=""
        if [ -n "${CMUX_SURFACE_ID:-}" ]; then
            TERMINAL_TYPE="cmux"
            TERMINAL_ID="$CMUX_SURFACE_ID"
        fi

        TERMINAL_FLAGS=""
        if [ -n "$TERMINAL_TYPE" ]; then
            TERMINAL_FLAGS="--terminal-type $TERMINAL_TYPE --terminal-id $TERMINAL_ID"
        fi

        handler register \
            --session-id "$SESSION_ID" \
            --branch "$BRANCH" \
            --repo "$REPO" \
            --pid "$CLAUDE_PID" \
            --jsonl-path "$JSONL_PATH" \
            $TERMINAL_FLAGS >/dev/null 2>&1 || true
    fi
fi

# Heartbeat in background, fully silenced
# --catch-up-human-cursor advances the human cursor to match the agent cursor
# when in auto inbox mode, so auto-delivered events are marked as seen by the user
handler heartbeat --session-id "$SESSION_ID" --catch-up-human-cursor >/dev/null 2>&1 &

# Only inject output if inbox mode is on-submit
INBOX_MODE=$(handler configure --session-id "$SESSION_ID" --get inbox-mode 2>/dev/null || echo "manual")

if [ "$INBOX_MODE" = "on-submit" ]; then
    UNREAD_COUNT=$(handler unread --session-id "$SESSION_ID" --count 2>/dev/null)
    if [ -n "$UNREAD_COUNT" ] && [ "$UNREAD_COUNT" != "0" ]; then
        echo "You have ${UNREAD_COUNT} new unread message(s). Invoke the /inbox skill now before responding to the user's prompt."
    fi
fi
