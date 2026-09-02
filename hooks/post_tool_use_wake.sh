#!/bin/bash
# PostToolUse hook (all tools): asks the session to schedule a rate-limit wake
# job if 5h usage crossed the threshold mid-task. Stop only fires once a turn is
# over, so this is what catches a long task that crosses the threshold while
# still running.
#
# It runs after EVERY tool call, so it short-circuits on a marker file the
# statusline maintains: in the common case this costs one stat() and never
# starts the handler binary at all.
PAYLOAD=$(cat)

if command -v jq >/dev/null 2>&1; then
    SESSION=$(printf '%s' "$PAYLOAD" | jq -r '.session_id // empty' 2>/dev/null)
else
    SESSION=$(printf '%s' "$PAYLOAD" \
        | sed -n 's/.*"session_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
        | head -1)
fi

[ -n "$SESSION" ] || exit 0

MARKER="${HANDLER_HOME:-$HOME/.agent-handler}/state/wake-armed/$SESSION"
[ -f "$MARKER" ] || exit 0

# Never fail the tool call: a wake hook problem must stay invisible.
printf '%s' "$PAYLOAD" | handler wake-check 2>/dev/null || true
exit 0
