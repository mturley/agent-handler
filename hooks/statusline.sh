#!/usr/bin/env bash
# Status line script for agent-handler
# Thin wrapper: all logic (rendering, heartbeat, notifications) is in Go.

if ! command -v handler &>/dev/null; then
    exit 0
fi

cat | handler statusline --from-hook 2>/dev/null
