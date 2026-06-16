#!/usr/bin/env bash
# SessionStart hook for agent-handler
# Discovers session ID, registers with handler, returns catch-up summary
set -euo pipefail

if ! command -v handler &>/dev/null; then
    exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

discover_and_register "$PPID" || exit 0
