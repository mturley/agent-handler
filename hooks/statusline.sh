#!/usr/bin/env bash
# Status line script for agent-handler
# Receives session JSON on stdin from Claude Code

if ! command -v handler &>/dev/null; then
    exit 0
fi

# Read session data from stdin
SESSION_DATA=$(cat)

# --- Single python3 call: extract all session info + git status ---
# Outputs shell variable assignments to be eval'd
EXTRA_VARS=$(echo "$SESSION_DATA" | python3 -c "
import subprocess, json, re, sys

stdin_data = json.load(sys.stdin)
session_id = stdin_data.get('session_id', '')
model = stdin_data.get('model', {}).get('display_name', '')
ctx_pct = int(stdin_data.get('context_window', {}).get('used_percentage', 0))
cost = stdin_data.get('cost', {}).get('total_cost_usd', 0.0)
cwd = stdin_data.get('cwd', '.')
session_name = stdin_data.get('session_name', '')
jsonl_path = stdin_data.get('transcript_path', '')

def shell_escape(s):
    return \"'\" + s.replace(\"'\", \"'\\\\''\") + \"'\"

# Always output session fields
lines = [
    f'SESSION_ID={shell_escape(session_id)}',
    f'MODEL_NAME={shell_escape(model)}',
    f'CTX_PCT={ctx_pct}',
    f'COST_USD={cost:.2f}',
    f'SESSION_NAME={shell_escape(session_name)}',
    f'JSONL_PATH={shell_escape(jsonl_path)}',
]

# Git fields
git_vars = {
    'IN_GIT': 0, 'GIT_BRANCH': '', 'GIT_DEFAULT': 'main', 'GIT_AHEAD': 0, 'GIT_BEHIND': 0,
    'GIT_CADDS': 0, 'GIT_CDELS': 0, 'GIT_MODIFIED': 0, 'GIT_UNTRACKED': 0,
    'GIT_UADDS': 0, 'GIT_UDELS': 0, 'GIT_REBASING': 0, 'GIT_REBASE_BRANCH': '',
}

def git(*args):
    r = subprocess.run(['git', '-C', cwd] + list(args), capture_output=True, text=True)
    return r.stdout.strip() if r.returncode == 0 else ''

# Check if we're in a git repo
check = subprocess.run(['git', '-C', cwd, 'rev-parse', '--git-dir'], capture_output=True)
if check.returncode == 0:
    git_vars['IN_GIT'] = 1

    try:
        from concurrent.futures import ThreadPoolExecutor

        # Phase 1: independent lookups (parallel)
        with ThreadPoolExecutor(max_workers=4) as ex:
            f_branch = ex.submit(git, 'rev-parse', '--abbrev-ref', 'HEAD')
            f_default = ex.submit(git, 'symbolic-ref', 'refs/remotes/origin/HEAD')
            f_porcelain = ex.submit(git, 'status', '--porcelain')
            f_uncommitted = ex.submit(git, 'diff', 'HEAD', '--shortstat')

        branch = f_branch.result()
        default_raw = f_default.result()
        default_branch = default_raw.replace('refs/remotes/origin/', '') if default_raw else 'main'
        porcelain = f_porcelain.result()
        uncommitted_stat = f_uncommitted.result()

        git_vars['GIT_BRANCH'] = branch
        git_vars['GIT_DEFAULT'] = default_branch

        # Detect interactive rebase
        import os
        git_dir = subprocess.run(['git', '-C', cwd, 'rev-parse', '--git-dir'],
                                 capture_output=True, text=True).stdout.strip()
        if git_dir:
            for rebase_dir in ['rebase-merge', 'rebase-apply']:
                head_name_file = os.path.join(git_dir, rebase_dir, 'head-name')
                if os.path.isfile(head_name_file):
                    git_vars['GIT_REBASING'] = 1
                    with open(head_name_file) as f:
                        ref = f.read().strip()
                    git_vars['GIT_REBASE_BRANCH'] = ref.replace('refs/heads/', '')
                    if branch == 'HEAD':
                        branch = git_vars['GIT_REBASE_BRANCH']
                        git_vars['GIT_BRANCH'] = branch
                    break

        # Phase 2: merge-base dependent (parallel)
        # Pick the best ref for merge-base: upstream > origin > local
        base_ref = default_branch
        for candidate in [f'upstream/{default_branch}', f'origin/{default_branch}']:
            if git('rev-parse', '--verify', candidate):
                base_ref = candidate
                break

        if branch and branch != default_branch:
            merge_base = git('merge-base', base_ref, 'HEAD')
            if merge_base:
                with ThreadPoolExecutor(max_workers=3) as ex:
                    f_ahead = ex.submit(git, 'rev-list', '--count', f'{merge_base}..HEAD')
                    f_behind = ex.submit(git, 'rev-list', '--count', f'HEAD..{base_ref}')
                    f_diff = ex.submit(git, 'diff', '--shortstat', f'{merge_base}..HEAD')
                git_vars['GIT_AHEAD'] = int(f_ahead.result() or 0)
                git_vars['GIT_BEHIND'] = int(f_behind.result() or 0)
                diff_stat = f_diff.result()
                m = re.search(r'(\d+) insertion', diff_stat)
                if m: git_vars['GIT_CADDS'] = int(m.group(1))
                m = re.search(r'(\d+) deletion', diff_stat)
                if m: git_vars['GIT_CDELS'] = int(m.group(1))

        # Parse porcelain
        for l in porcelain.split('\n'):
            if not l: continue
            if l.startswith('??'):
                git_vars['GIT_UNTRACKED'] += 1
            else:
                git_vars['GIT_MODIFIED'] += 1

        # Parse uncommitted stats
        m = re.search(r'(\d+) insertion', uncommitted_stat)
        if m: git_vars['GIT_UADDS'] = int(m.group(1))
        m = re.search(r'(\d+) deletion', uncommitted_stat)
        if m: git_vars['GIT_UDELS'] = int(m.group(1))
    except Exception:
        pass

for k, v in git_vars.items():
    if isinstance(v, str):
        lines.append(f'{k}={shell_escape(v)}')
    else:
        lines.append(f'{k}={v}')

print('\n'.join(lines))
" 2>/dev/null)

if [ -z "$EXTRA_VARS" ]; then
    # Fallback: extract just session_id for handler statusline
    SESSION_ID=$(echo "$SESSION_DATA" | python3 -c "import sys,json; print(json.load(sys.stdin).get('session_id',''))" 2>/dev/null)
else
    eval "$EXTRA_VARS"
fi

if [ -z "$SESSION_ID" ]; then
    # Fall back to PID cache
    SESSIONS_DIR="${HANDLER_HOME:-$HOME/.agent-handler}/data/sessions"
    CLAUDE_PID="$PPID"
    if [ -f "${SESSIONS_DIR}/${CLAUDE_PID}" ]; then
        SESSION_ID=$(cat "${SESSIONS_DIR}/${CLAUDE_PID}")
    fi
fi

if [ -z "$SESSION_ID" ]; then
    exit 0
fi

# --- Get handler statusline output ---
OUTPUT=$(handler statusline --session "$SESSION_ID" 2>/dev/null)
if [ -z "$OUTPUT" ]; then
    exit 0
fi

# If session is not registered, try to register in the background
if echo "$OUTPUT" | grep -q "not registered"; then
    if [ -n "$SESSION_ID" ] && [ -n "$JSONL_PATH" ]; then
        (
            REPO=$(git remote get-url origin 2>/dev/null | sed 's/.*github.com[:/]//' | sed 's/\.git$//' || echo "unknown")
            REGISTER_ARGS=(--session-id "$SESSION_ID" --branch "${GIT_BRANCH:-unknown}" --repo "$REPO" --pid "$PPID" --jsonl-path "$JSONL_PATH")
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

# Sync session name if it changed
if [ -n "$SESSION_NAME" ] && [ -n "$SESSION_ID" ]; then
    handler heartbeat --session-id "$SESSION_ID" --session-name "$SESSION_NAME" >/dev/null 2>&1 &
fi

# Extract unread count from output for notification
if [ -n "$SESSION_ID" ]; then
    UNREAD_COUNT=$(echo "$OUTPUT" | grep -o '● [0-9]* unread' 2>/dev/null | grep -o '[0-9]*' || echo "0")
    if [ "$UNREAD_COUNT" -gt 0 ] 2>/dev/null; then
        NOTIFY_MSG=$(echo "$OUTPUT" | head -1 | sed 's/.*● //' | sed 's/\x1b\[[0-9;]*m//g')
        handler notify --session "$SESSION_ID" --count "$UNREAD_COUNT" --message "$NOTIFY_MSG" 2>/dev/null &
    else
        handler notify --session "$SESSION_ID" --count 0 2>/dev/null &
    fi
fi

# --- Parse config prefix from handler output ---
SHOW_CONTEXT=1
SHOW_GIT=1
CFG_LINE=$(echo "$OUTPUT" | head -1)
if [[ "$CFG_LINE" == __cfg:* ]]; then
    OUTPUT=$(echo "$OUTPUT" | tail -n +2)
    [[ "$CFG_LINE" =~ context=([01]) ]] && SHOW_CONTEXT="${BASH_REMATCH[1]}"
    [[ "$CFG_LINE" =~ git=([01]) ]] && SHOW_GIT="${BASH_REMATCH[1]}"
fi

# Detect handler session
IS_HANDLER=false
if echo "$OUTPUT" | head -1 | grep -q '/handler'; then
    IS_HANDLER=true
fi

# --- ANSI colors ---
CLAUDE_ORANGE="\033[38;2;218;119;86m"
CYAN="\033[36m"
GREEN="\033[32m"
RED="\033[31m"
YELLOW="\033[33m"
BOLD_WHITE="\033[1;37m"
DIM="\033[2m"
DIM_GREEN="\033[2;32m"
RESET="\033[0m"

# --- Build model/context/cost line ---
MODEL_LINE=""
if [ "$SHOW_CONTEXT" = "1" ] && [ -n "$MODEL_NAME" ] && [ -n "$CTX_PCT" ]; then
    # Progress bar: 20 chars wide
    FILLED=$((CTX_PCT * 20 / 100))
    EMPTY=$((20 - FILLED))

    BAR=""
    for ((i=0; i<FILLED; i++)); do BAR+="▓"; done
    for ((i=0; i<EMPTY; i++)); do BAR+="░"; done

    # Bar color based on percentage
    if [ "$CTX_PCT" -ge 80 ]; then
        BAR_COLOR="${RED}"
    elif [ "$CTX_PCT" -ge 50 ]; then
        BAR_COLOR="${YELLOW}"
    else
        BAR_COLOR="${GREEN}"
    fi

    MODEL_LINE="${CLAUDE_ORANGE}${MODEL_NAME}${RESET} ${BAR_COLOR}${BAR}${RESET} ${CTX_PCT}% ctx ${DIM}| \$${COST_USD}${RESET}"
fi

# --- Build git status line ---
GIT_LINE=""
if [ "$SHOW_GIT" = "1" ] && [ "$IS_HANDLER" = "false" ] && [ "$IN_GIT" = "1" ]; then
    # Rebase indicator + branch name
    if [ "$GIT_REBASING" = "1" ] 2>/dev/null; then
        GIT_LINE="${YELLOW}rebasing${RESET} ${BOLD_WHITE}${GIT_BRANCH}${RESET}"
    elif [ "$GIT_BRANCH" = "$GIT_DEFAULT" ]; then
        GIT_LINE="on ${BOLD_WHITE}${GIT_BRANCH}${RESET}"
    else
        GIT_LINE="${BOLD_WHITE}${GIT_BRANCH}${RESET}"
    fi

    # Commits ahead section
    if [ "$GIT_AHEAD" -gt 0 ] 2>/dev/null; then
        GIT_LINE+=" ${GREEN}↑${GIT_AHEAD}${RESET}"
        if [ "$GIT_CADDS" -gt 0 ] || [ "$GIT_CDELS" -gt 0 ]; then
            GIT_LINE+=" (${GREEN}+${GIT_CADDS}${RESET} ${RED}−${GIT_CDELS}${RESET})"
        fi
    fi

    # Dirty/clean section
    DIRTY_COUNT=$((GIT_MODIFIED + GIT_UNTRACKED))
    if [ "$DIRTY_COUNT" -gt 0 ] 2>/dev/null; then
        GIT_LINE+=" | "
        PARTS=""
        if [ "$GIT_MODIFIED" -gt 0 ]; then
            PARTS+="${YELLOW}${GIT_MODIFIED} modified${RESET}"
        fi
        if [ "$GIT_UNTRACKED" -gt 0 ]; then
            [ -n "$PARTS" ] && PARTS+=", "
            PARTS+="${YELLOW}${GIT_UNTRACKED} untracked${RESET}"
        fi
        GIT_LINE+="$PARTS"
        if [ "$GIT_UADDS" -gt 0 ] || [ "$GIT_UDELS" -gt 0 ]; then
            GIT_LINE+=" (${GREEN}+${GIT_UADDS}${RESET} ${RED}−${GIT_UDELS}${RESET})"
        fi
    else
        GIT_LINE+=" | ${DIM_GREEN}clean${RESET}"
    fi

    # Behind default branch
    if [ "$GIT_BEHIND" -gt 0 ] 2>/dev/null; then
        GIT_LINE+=" ${DIM}↓${GIT_BEHIND} behind ${GIT_DEFAULT}${RESET}"
    fi
fi

# --- Assemble final output ---
FINAL=""
if [ "$IS_HANDLER" = "true" ]; then
    # Handler: first line, then model line, then rest
    FIRST_LINE=$(echo "$OUTPUT" | head -1)
    REST=$(echo "$OUTPUT" | tail -n +2)
    FINAL+="$FIRST_LINE"
    if [ -n "$MODEL_LINE" ]; then
        FINAL+="\n$MODEL_LINE"
    fi
    if [ -n "$REST" ]; then
        FINAL+="\n$REST"
    fi
else
    # Regular: git, model, handler output
    if [ -n "$GIT_LINE" ]; then
        FINAL+="$GIT_LINE"
    fi
    if [ -n "$MODEL_LINE" ]; then
        [ -n "$FINAL" ] && FINAL+="\n"
        FINAL+="$MODEL_LINE"
    fi
    if [ -n "$OUTPUT" ]; then
        [ -n "$FINAL" ] && FINAL+="\n"
        FINAL+="$OUTPUT"
    fi

    # Check for sessions awaiting approval (non-handler only)
    AWAITING_COUNT=$(handler peek --list-need-input --json 2>/dev/null | python3 -c "import sys,json; print(len(json.load(sys.stdin) or []))" 2>/dev/null || echo "0")
    if [ "$AWAITING_COUNT" -gt 0 ] 2>/dev/null; then
        [ -n "$FINAL" ] && FINAL+="\n"
        FINAL+="${YELLOW}${AWAITING_COUNT} session(s) awaiting approval${RESET}"
    fi
fi

printf "%b\n" "$FINAL"
