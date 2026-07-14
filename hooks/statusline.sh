#!/usr/bin/env bash
# Status line script for agent-handler
# Thin wrapper: pipes Claude Code's stdin JSON to the Go binary for rendering.
# Background side-effects (registration, heartbeat, notifications) run after.

if ! command -v handler &>/dev/null; then
    exit 0
fi

# Capture stdin for both the Go binary and background tasks
HOOK_STDIN=$(cat)

# Render statusline (all logic is in Go)
OUTPUT=$(echo "$HOOK_STDIN" | handler statusline --from-hook 2>/dev/null)
if [ -z "$OUTPUT" ]; then
    exit 0
fi
printf "%s\n" "$OUTPUT"

# --- Background side-effects (don't block rendering) ---

# Extract session info for background tasks
EXTRA=$(echo "$HOOK_STDIN" | python3 -c "
import json, sys
d = json.load(sys.stdin)
def esc(s): return \"'\" + s.replace(\"'\", \"'\\\\''\") + \"'\"
print(f'SESSION_ID={esc(d.get(\"session_id\",\"\"))}')
print(f'SESSION_NAME={esc(d.get(\"session_name\",\"\"))}')
print(f'JSONL_PATH={esc(d.get(\"transcript_path\",\"\"))}')
" 2>/dev/null)

if [ -z "$EXTRA" ]; then
    exit 0
fi
eval "$EXTRA"

if [ -z "$SESSION_ID" ]; then
    exit 0
fi

CWD=$(echo "$HOOK_STDIN" | python3 -c "import sys,json; print(json.load(sys.stdin).get('cwd','.'))" 2>/dev/null || echo ".")

# Register if not yet registered
if echo "$OUTPUT" | grep -q "not registered"; then
    if [ -n "$JSONL_PATH" ]; then
        (
            BRANCH=$(git -C "$CWD" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
            REPO=$(git -C "$CWD" remote get-url origin 2>/dev/null | sed 's/.*github.com[:/]//' | sed 's/\.git$//' || echo "unknown")

            REGISTER_ARGS=(--session-id "$SESSION_ID" --branch "$BRANCH" --repo "$REPO" --pid "$PPID" --jsonl-path "$JSONL_PATH")
            if [ -n "${CMUX_SURFACE_ID:-}" ]; then
                REGISTER_ARGS+=(--terminal-type cmux --terminal-id "$CMUX_SURFACE_ID")
            fi
            if [ -n "$SESSION_NAME" ]; then
                REGISTER_ARGS+=(--session-name "$SESSION_NAME")
            fi
            handler register "${REGISTER_ARGS[@]}" >/dev/null 2>&1
        ) &
    fi
fi

# Sync session name
if [ -n "$SESSION_NAME" ]; then
    handler heartbeat --session-id "$SESSION_ID" --session-name "$SESSION_NAME" >/dev/null 2>&1 &
fi

# Dispatch notification
UNREAD_COUNT=$(echo "$OUTPUT" | grep -o '● [0-9]* unread' 2>/dev/null | grep -o '[0-9]*' || echo "0")
if [ "$UNREAD_COUNT" -gt 0 ] 2>/dev/null; then
    NOTIFY_MSG=$(echo "$OUTPUT" | head -1 | sed 's/.*● //' | sed 's/\x1b\[[0-9;]*m//g')
    handler notify --session "$SESSION_ID" --count "$UNREAD_COUNT" --message "$NOTIFY_MSG" 2>/dev/null &
else
    handler notify --session "$SESSION_ID" --count 0 2>/dev/null &
fi
