# .worktree-resources File Format

The `.worktree-resources` file lives in the root of a git worktree (gitignored). It declares which external resources this worktree cares about. Any tool can read or write this file — it's the portable contract between agent-handler and other worktree-aware tools.

## Format

One resource per line: `<type>:<id> <url>`

Lines prefixed with `~ ` are **related** (watching for context). Unmarked lines are **primary** (the reason this worktree exists).

```
pr:owner/repo#123 https://github.com/owner/repo/pull/123
jira:RHOAIENG-456 https://redhat.atlassian.net/browse/RHOAIENG-456
~ pr:owner/repo#120 https://github.com/owner/repo/pull/120
~ jira:RHOAIENG-400 https://redhat.atlassian.net/browse/RHOAIENG-400
```

In this example, the worktree was created to work on PR #123 and issue RHOAIENG-456 (primary). PR #120 and RHOAIENG-400 are being watched for context (related).

## Primary vs Related

| | Primary | Related |
|---|---------|---------|
| **Prefix** | (none) | `~ ` |
| **Meaning** | The worktree exists for this resource | Watching for context |
| **Example** | The PR for this branch | A PR that touches the same files |
| **Terminal state** | Signals the worktree may be done | Informational only |

Both primary and related resources are subscribed to equally — watchers poll them the same way and events route the same way. The distinction is metadata for tools that want to understand why the worktree exists.

## Supported resource types

| Type | ID Format | Example |
|------|-----------|---------|
| `pr` | `owner/repo#number` | `pr:mturley/odh-dashboard#7705` |
| `jira` | Issue key | `jira:RHOAIENG-12345` |

## Behavior

- `handler register` reads this file on session start and auto-subscribes to all listed resources
- `handler subscribe` appends new entries (use `--primary` to mark as primary, default is related)
- `handler unsubscribe` removes entries
- Malformed lines (missing URL, empty lines) are silently skipped
- Duplicate entries are deduplicated on append

## Integration with other tools

Any tool that sets up a worktree can seed this file. Primary resources go on unmarked lines, related resources get the `~ ` prefix:

```bash
# Primary: the PR this worktree was created for
echo "pr:owner/repo#123 https://github.com/owner/repo/pull/123" >> .worktree-resources

# Related: a PR we want to watch for context
echo "~ pr:owner/repo#120 https://github.com/owner/repo/pull/120" >> .worktree-resources
```

agent-handler will pick up these subscriptions on the next session start in that worktree. If watchers are configured, they'll start polling all subscribed resources for changes.

The file is designed to be human-readable, easy to write from shell scripts, and tool-agnostic — no dependency on agent-handler to read or write it.
