#!/usr/bin/env bash
# Shared functions for agent-handler hooks

discover_and_register() {
    local CLAUDE_PID="$1"
    local CLAUDE_HOME="${HOME}/.claude"
    local CWD="$(pwd)"

    PROJECT_DIR_NAME="-$(echo "$CWD" | sed 's/\//-/g' | sed 's/^-//')"
    PROJECT_DIR="${CLAUDE_HOME}/projects/${PROJECT_DIR_NAME}"

    if [ ! -d "$PROJECT_DIR" ]; then
        return 1
    fi

    JSONL_PATH=$(ls -t "$PROJECT_DIR"/*.jsonl 2>/dev/null | head -1)
    if [ -z "$JSONL_PATH" ]; then
        return 1
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
}
