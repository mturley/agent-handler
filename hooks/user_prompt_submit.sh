#!/usr/bin/env bash
# UserPromptSubmit hook for agent-handler
# Thin wrapper: all logic (registration, heartbeat, inbox) is in Go.
# IMPORTANT: No output unless inbox mode requires it (on-submit or auto catchup).

if ! command -v handler &>/dev/null; then
    exit 0
fi

export CLAUDE_PID=$PPID
cat | handler user-prompt-submit --from-hook
