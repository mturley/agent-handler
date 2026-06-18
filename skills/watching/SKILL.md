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
3. For any watcher in an error state (where `last_error` is more recent than `last_success` in the watcher status), show the error message and help troubleshoot:
   - "Could not resolve to a Repository" → the repo may be private and the GitHub token needs `repo` scope. Suggest re-running `handler watcher auth github` with a new token.
   - "401 Unauthorized" or "403 Forbidden" → the API token is invalid or expired. Suggest `handler watcher auth <service>` to update it.
   - "rate limit" → suggest increasing the polling interval with `handler watcher uninstall <name>` then `handler watcher install <name> --interval 10m`.
   - For other errors, show the error message and suggest `handler watcher logs <name>` for details.
4. If watchers are not installed for subscribed resource types, suggest running `handler watcher install`
5. Tell the user:

You can ask me to watch or unwatch a resource by its ID, number, or URL. For example:
- "watch PR #123" or "watch https://github.com/owner/repo/pull/123"
- "watch RHOAIENG-456" or "watch https://redhat.atlassian.net/browse/RHOAIENG-456"
- "unwatch PR #123"
- "unwatch RHOAIENG-456"
