---
name: watching
description: Show watched resources, watcher status, and recent watcher errors for this session
---

# /watching — Show Watched Resources

## Usage

```bash
handler subscriptions --json
handler watcher list --json
handler query "SELECT e.ts, e.title, e.body FROM events e JOIN event_resources er ON er.event_id = e.id JOIN subscriptions sub ON sub.resource_type = er.resource_type AND sub.resource_id = er.resource_id AND sub.deleted_at IS NULL WHERE e.type = 'watcher_error' AND e.ts > datetime('now', '-24 hours') ORDER BY e.ts DESC LIMIT 10"
```

## After reading the data

1. Present the watched resources grouped by type (PRs, Jira issues)
2. For each resource, show: resource ID, URL, and whether it's primary or related
3. Show watcher status: installed, running, last run time
4. If there are recent watcher errors (last 24 hours), summarize them and suggest fixes
5. If watchers are not installed for subscribed resource types, suggest running `handler watcher install`
6. Tell the user:

You can ask me to watch or unwatch a resource by its ID, number, or URL. For example:
- "watch PR #123" or "watch https://github.com/owner/repo/pull/123"
- "watch RHOAIENG-456" or "watch https://redhat.atlassian.net/browse/RHOAIENG-456"
- "unwatch PR #123"
- "unwatch RHOAIENG-456"
