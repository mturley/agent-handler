---
name: watching
description: Show watched resources, watcher status, and recent watcher errors for this session
---

# /watching — Show Watched Resources

## Usage

```bash
handler watching --json
```

## After reading the data

1. Present the watched resources grouped by type (PRs, Jira issues)
2. Show watcher status: configured, installed, running, last run time
3. If there are recent watcher errors (last 24 hours), summarize them and suggest fixes
4. If watchers are not installed for subscribed resource types, suggest running `handler watcher install`
5. Tell the user:

You can ask me to watch or unwatch a resource by its ID, number, or URL. For example:
- "watch PR #123" or "watch https://github.com/owner/repo/pull/123"
- "watch RHOAIENG-456" or "watch https://redhat.atlassian.net/browse/RHOAIENG-456"
- "unwatch PR #123"
- "unwatch RHOAIENG-456"
