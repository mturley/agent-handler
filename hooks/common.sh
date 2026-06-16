#!/usr/bin/env bash
# Shared functions for agent-handler hooks

find_project_dir() {
    local CWD="$1"
    local CLAUDE_HOME="${HOME}/.claude"
    local PROJECTS_DIR="${CLAUDE_HOME}/projects"

    # Try Claude's encoding: replace both / and . with -
    local ENCODED="-$(echo "$CWD" | sed 's/[\/\.]/-/g' | sed 's/^-//')"
    if [ -d "${PROJECTS_DIR}/${ENCODED}" ]; then
        echo "${PROJECTS_DIR}/${ENCODED}"
        return 0
    fi

    # Fallback: replace only / with - (in case encoding rules change)
    ENCODED="-$(echo "$CWD" | sed 's/\//-/g' | sed 's/^-//')"
    if [ -d "${PROJECTS_DIR}/${ENCODED}" ]; then
        echo "${PROJECTS_DIR}/${ENCODED}"
        return 0
    fi

    return 1
}

discover_and_register() {
    local CLAUDE_PID="$1"
    local CWD="$(pwd)"

    PROJECT_DIR=$(find_project_dir "$CWD") || return 1

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
