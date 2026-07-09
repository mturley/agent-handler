---
name: unwatch
description: "Stop watching a PR or Jira issue. Use when the user says 'unwatch PR #123', 'unwatch RHOAIENG-456', or wants to stop monitoring a resource."
---

# /unwatch — Stop Watching a Resource

Unsubscribe from a PR or Jira issue so you stop receiving updates about it.

## If invoked with no arguments

Show what's currently being watched and let the user pick what to unwatch.

Run `handler watching --json` and list the watched resources. Then say:

> Use `/unwatch <resource>` to stop watching. For example:
> - `/unwatch #123` or `/unwatch RHOAIENG-456`

After printing the list, stop.

## With arguments: unsubscribe from a resource

The user's args contain a resource identifier. Parse it the same way as `/watch`.

### Step 1: Parse the resource

| User input | Resource format |
|-----------|----------------|
| `#123` or `PR #123` | Get `owner/repo` from `git remote get-url origin`, then `pr:owner/repo#123` |
| `owner/repo#123` | `pr:owner/repo#123` |
| `RHOAIENG-456` | `jira:RHOAIENG-456` |
| Full GitHub or Jira URL | Extract the resource type and ID |

### Step 2: Unsubscribe

```bash
handler unsubscribe --resource "<type:id>"
```

### Step 3: Confirm

Tell the user the resource is no longer being watched. Mention they can use `/watch` to start watching something else.
