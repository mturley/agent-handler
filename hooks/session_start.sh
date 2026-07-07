#!/usr/bin/env bash
# SessionStart hook for agent-handler
# Reads session ID from stdin JSON, registers with handler, returns catch-up summary
set -euo pipefail

if ! command -v handler &>/dev/null; then
    exit 0
fi

# Read session data from stdin (provided by Claude Code)
HOOK_STDIN=$(cat)
SESSION_ID=$(echo "$HOOK_STDIN" | python3 -c "import sys,json; print(json.load(sys.stdin).get('session_id',''))" 2>/dev/null)
JSONL_PATH=$(echo "$HOOK_STDIN" | python3 -c "import sys,json; print(json.load(sys.stdin).get('transcript_path',''))" 2>/dev/null)
SESSION_TITLE=$(echo "$HOOK_STDIN" | python3 -c "import sys,json; print(json.load(sys.stdin).get('session_title',''))" 2>/dev/null)

if [ -z "$SESSION_ID" ] || [ -z "$JSONL_PATH" ]; then
    exit 0
fi

BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
REPO=$(git remote get-url origin 2>/dev/null | sed 's/.*github.com[:/]//' | sed 's/\.git$//' || echo "unknown")

REGISTER_ARGS=(
    --session-id "$SESSION_ID"
    --branch "$BRANCH"
    --repo "$REPO"
    --pid "$PPID"
    --jsonl-path "$JSONL_PATH"
)

if [ -n "${CMUX_SURFACE_ID:-}" ]; then
    REGISTER_ARGS+=(--terminal-type cmux --terminal-id "$CMUX_SURFACE_ID")
elif [ -n "${TMUX:-}" ] && [ "${HANDLER_MANAGED:-}" = "1" ]; then
    REGISTER_ARGS+=(--terminal-type tmux --terminal-id "$(tmux display-message -p '#{pane_id}' 2>/dev/null || true)")
    tmux select-pane -T "handler:${SESSION_ID}" 2>/dev/null || true
fi

if [ -n "$SESSION_TITLE" ]; then
    REGISTER_ARGS+=(--session-name "$SESSION_TITLE")
fi

handler register "${REGISTER_ARGS[@]}"
