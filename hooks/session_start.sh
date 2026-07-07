#!/usr/bin/env bash
# SessionStart hook for agent-handler
# Reads session ID from stdin JSON, registers with handler, returns catch-up summary
set -euo pipefail

if ! command -v handler &>/dev/null; then
    exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

# Read session data from stdin (provided by Claude Code)
HOOK_STDIN=$(cat)
SESSION_ID=$(echo "$HOOK_STDIN" | python3 -c "import sys,json; print(json.load(sys.stdin).get('session_id',''))" 2>/dev/null)
JSONL_PATH=$(echo "$HOOK_STDIN" | python3 -c "import sys,json; print(json.load(sys.stdin).get('transcript_path',''))" 2>/dev/null)

if [ -n "$SESSION_ID" ] && [ -n "$JSONL_PATH" ]; then
    # Use session ID from stdin — accurate even with multiple sessions in same worktree
    BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
    REPO=$(git remote get-url origin 2>/dev/null | sed 's/.*github.com[:/]//' | sed 's/\.git$//' || echo "unknown")

    TERMINAL_TYPE=""
    TERMINAL_ID=""
    if [ -n "${CMUX_SURFACE_ID:-}" ]; then
        TERMINAL_TYPE="cmux"
        TERMINAL_ID="$CMUX_SURFACE_ID"
    elif [ -n "${TMUX:-}" ] && [ "${HANDLER_MANAGED:-}" = "1" ]; then
        TERMINAL_TYPE="tmux"
        TERMINAL_ID=$(tmux display-message -p '#{pane_id}' 2>/dev/null || true)
        tmux select-pane -T "handler:${SESSION_ID}" 2>/dev/null || true
    fi

    TERMINAL_FLAGS=""
    if [ -n "$TERMINAL_TYPE" ]; then
        TERMINAL_FLAGS="--terminal-type $TERMINAL_TYPE --terminal-id $TERMINAL_ID"
    fi

    handler register \
        --session-id "$SESSION_ID" \
        --branch "$BRANCH" \
        --repo "$REPO" \
        --pid "$PPID" \
        --jsonl-path "$JSONL_PATH" \
        $TERMINAL_FLAGS
else
    # Fallback to JSONL discovery (for older Claude Code versions without stdin JSON)
    discover_and_register "$PPID" || exit 0
fi
