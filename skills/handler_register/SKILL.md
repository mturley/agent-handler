---
name: handler_register
description: Manually register this session with agent-handler
---

# /handler_register — Register Session

This is usually done automatically by the SessionStart hook. Use this skill when you need to manually register or re-register.

## Usage

Discover your session ID and register:
```bash
SESSION_ID=$(ls -t ~/.claude/projects/-$(pwd | sed 's/\//-/g' | sed 's/^-//')/*.jsonl | head -1 | xargs basename | sed 's/.jsonl//')
BRANCH=$(git rev-parse --abbrev-ref HEAD)
REPO=$(git remote get-url origin | sed 's/.*github.com[:/]//' | sed 's/\.git$//')

handler register \
    --session-id "$SESSION_ID" \
    --branch "$BRANCH" \
    --repo "$REPO" \
    --pid $PPID \
    --jsonl-path "$(ls -t ~/.claude/projects/-$(pwd | sed 's/\//-/g' | sed 's/^-//')/*.jsonl | head -1)"
```

This will:
- Register or re-register the session
- Auto-subscribe to resources listed in `.worktree-resources`
- Show a catch-up summary of unread events
