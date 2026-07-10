#!/usr/bin/env bash
# UserPromptSubmit hook for agent-handler
# Registers if needed, bumps heartbeat, optionally injects unread events.
# IMPORTANT: No output unless inbox mode is on-submit. All other operations are silent.
set -euo pipefail

if ! command -v handler &>/dev/null; then
    exit 0
fi

CLAUDE_PID="$PPID"
SESSIONS_DIR="${HANDLER_HOME:-$HOME/.agent-handler}/data/sessions"

# Read session ID from stdin JSON (provided by Claude Code)
HOOK_STDIN=$(cat)
SESSION_ID=$(echo "$HOOK_STDIN" | python3 -c "import sys,json; print(json.load(sys.stdin).get('session_id',''))" 2>/dev/null)
PROMPT_TEXT=$(echo "$HOOK_STDIN" | python3 -c "import sys,json; print(json.load(sys.stdin).get('prompt',''))" 2>/dev/null)

if [ -z "$SESSION_ID" ]; then
    # Fallback to PID cache if stdin doesn't have session_id
    if [ -f "${SESSIONS_DIR}/${CLAUDE_PID}" ]; then
        SESSION_ID=$(cat "${SESSIONS_DIR}/${CLAUDE_PID}")
    else
        exit 0
    fi
fi

# Register if not yet registered (PID cache miss) — runs in background
# to avoid blocking the hook timeout on slow registration
if [ ! -f "${SESSIONS_DIR}/${CLAUDE_PID}" ] || [ "$(cat "${SESSIONS_DIR}/${CLAUDE_PID}")" != "$SESSION_ID" ]; then
    JSONL_PATH=$(echo "$HOOK_STDIN" | python3 -c "import sys,json; print(json.load(sys.stdin).get('transcript_path',''))" 2>/dev/null)
    if [ -n "$JSONL_PATH" ]; then
        (
            BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
            REPO=$(git remote get-url origin 2>/dev/null | sed 's/.*github.com[:/]//' | sed 's/\.git$//' || echo "unknown")

            TERMINAL_TYPE=""
            TERMINAL_ID=""
            if [ -n "${CMUX_SURFACE_ID:-}" ]; then
                TERMINAL_TYPE="cmux"
                TERMINAL_ID="$CMUX_SURFACE_ID"
            fi

            TERMINAL_FLAGS=""
            if [ -n "$TERMINAL_TYPE" ]; then
                TERMINAL_FLAGS="--terminal-type $TERMINAL_TYPE --terminal-id $TERMINAL_ID"
            fi

            handler register \
                --session-id "$SESSION_ID" \
                --branch "$BRANCH" \
                --repo "$REPO" \
                --pid "$CLAUDE_PID" \
                --jsonl-path "$JSONL_PATH" \
                $TERMINAL_FLAGS >/dev/null 2>&1 || true
        ) &
    fi
fi

# Check inbox mode before heartbeat (heartbeat may advance cursors)
INBOX_MODE=$(handler configure --session-id "$SESSION_ID" --get inbox-mode 2>/dev/null || echo "manual")

# Skip catch-up logic for cron-fired /inbox --auto prompts
IS_AUTO_INBOX=false
if [ "$PROMPT_TEXT" = "/inbox --auto" ]; then
    IS_AUTO_INBOX=true
fi

# In auto mode, check for auto-delivered events before catching up the human cursor
# Only do this for real user prompts, not cron-fired /inbox --auto
if [ "$INBOX_MODE" = "auto" ] && [ "$IS_AUTO_INBOX" = false ]; then
    AUTO_COUNT=$(handler auto-delivered --session-id "$SESSION_ID" 2>/dev/null || echo "0")
    if [ -n "$AUTO_COUNT" ] && [ "$AUTO_COUNT" != "0" ]; then
        echo "The user is back. While they were away, ${AUTO_COUNT} event(s) were auto-delivered to your inbox. Before responding to their prompt, briefly summarize what happened since their last prompt (look back through your conversation for the auto-delivered inbox results)."
    fi
fi

# Heartbeat in background
# Only catch up human cursor for real user prompts, not cron-fired ones
if [ "$IS_AUTO_INBOX" = true ]; then
    handler heartbeat --session-id "$SESSION_ID" >/dev/null 2>&1 &
else
    handler heartbeat --session-id "$SESSION_ID" --catch-up-human-cursor >/dev/null 2>&1 &
fi

# In on-submit mode, notify about unread events
if [ "$INBOX_MODE" = "on-submit" ]; then
    UNREAD_COUNT=$(handler unread --session-id "$SESSION_ID" --count 2>/dev/null)
    if [ -n "$UNREAD_COUNT" ] && [ "$UNREAD_COUNT" != "0" ]; then
        echo "You have ${UNREAD_COUNT} new unread message(s). Invoke the /inbox skill now before responding to the user's prompt."
    fi
fi
