#!/usr/bin/env bash
# SessionStart hook for agent-handler
# Discovers session ID, registers with handler, returns catch-up summary
set -euo pipefail

if ! command -v handler &>/dev/null; then
    exit 0
fi

CLAUDE_PID="$PPID"
CLAUDE_HOME="${HOME}/.claude"
CWD="$(pwd)"

PROJECT_DIR_NAME="-$(echo "$CWD" | sed 's/\//-/g' | sed 's/^-//')"
PROJECT_DIR="${CLAUDE_HOME}/projects/${PROJECT_DIR_NAME}"

if [ ! -d "$PROJECT_DIR" ]; then
    exit 0
fi

JSONL_PATH=$(ls -t "$PROJECT_DIR"/*.jsonl 2>/dev/null | head -1)
if [ -z "$JSONL_PATH" ]; then
    exit 0
fi

SESSION_ID=$(basename "$JSONL_PATH" .jsonl)
BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
REPO=$(git remote get-url origin 2>/dev/null | sed 's/.*github.com[:/]//' | sed 's/\.git$//' || echo "unknown")

handler register \
    --session-id "$SESSION_ID" \
    --branch "$BRANCH" \
    --repo "$REPO" \
    --pid "$CLAUDE_PID" \
    --jsonl-path "$JSONL_PATH"
