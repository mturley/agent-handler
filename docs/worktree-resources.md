# .worktree-resources File Format

The `.worktree-resources` file lives in the root of a git worktree (gitignored). It declares which external resources this worktree cares about. Any tool can read or write this file — it's the portable contract between agent-handler and other worktree-aware tools.

## Format

One resource per line: `<type>:<id> <url>`

```
pr:owner/repo#123 https://github.com/owner/repo/pull/123
jira:RHOAIENG-12345 https://redhat.atlassian.net/browse/RHOAIENG-12345
```

## Supported resource types

| Type | ID Format | Example |
|------|-----------|---------|
| `pr` | `owner/repo#number` | `pr:mturley/odh-dashboard#7705` |
| `jira` | Issue key | `jira:RHOAIENG-12345` |

## Behavior

- `handler register` reads this file on session start and auto-subscribes to listed resources
- `handler subscribe` appends new entries
- `handler unsubscribe` removes entries
- Malformed lines (missing URL, empty lines) are silently skipped
- Duplicate entries are deduplicated on append

## Integration with other tools

Any tool that sets up a worktree can seed this file. For example, a worktree creation script that associates a worktree with a PR:

```bash
echo "pr:owner/repo#123 https://github.com/owner/repo/pull/123" >> .worktree-resources
```

agent-handler will pick up these subscriptions on the next session start in that worktree. If watchers are configured, they'll start polling the subscribed resources for changes.

The file is designed to be human-readable, easy to write from shell scripts, and tool-agnostic — no dependency on agent-handler to read or write it.
