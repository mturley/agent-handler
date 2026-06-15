#!/usr/bin/env bash
# PreCompact hook for agent-handler
# Writes a pre_compact_snapshot event to preserve context before compaction.
set -euo pipefail

if ! command -v handler &>/dev/null; then
    exit 0
fi

CLAUDE_PID="$PPID"
SESSIONS_DIR="${HOME}/.agent-handler/sessions"

if [ -f "${SESSIONS_DIR}/${CLAUDE_PID}" ]; then
    SESSION_ID=$(cat "${SESSIONS_DIR}/${CLAUDE_PID}")
else
    exit 0
fi

handler emit \
    --session-id "$SESSION_ID" \
    --type pre_compact_snapshot \
    --title "Pre-compaction snapshot" \
    --body "Context is about to be compacted. Check handler log for this session's recent activity."
