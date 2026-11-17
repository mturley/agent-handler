#!/bin/bash
# PostToolUse hook (all tools): asks the session to schedule a rate-limit wake
# job if 5h usage crossed the threshold mid-task. Stop only fires once a turn is
# over, so this is what catches a long task that crosses the threshold while
# still running.
#
# It runs after EVERY tool call, so it short-circuits on whether ANY session is
# armed: in the common case (no directory entries) this costs one glob and
# never starts the handler binary, reads stdin, or parses JSON at all. Which
# session is armed, and whether it matches this payload, is decided in Go —
# a bash regex for "session_id" would take the leftmost match, which can be a
# nested key inside tool_input, and a wrong id would silently skip the wake.
DIR="${HANDLER_HOME:-$HOME/.agent-handler}/state/wake-armed"
shopt -s nullglob
armed=("$DIR"/*)
[ ${#armed[@]} -gt 0 ] || exit 0

# Never fail the tool call: a wake hook problem must stay invisible. stdin is
# inherited so Go reads and parses the whole payload itself.
handler wake-check 2>/dev/null || true
exit 0
