---
name: watch
description: "Watch a PR or Jira issue for changes. Use when the user says 'watch PR #123', 'watch RHOAIENG-456', or provides a GitHub/Jira URL to monitor."
---

# /watch — Watch a Resource for Changes

Subscribe to a PR or Jira issue so you get notified in `/inbox` when it changes (new comments, reviews, status changes, CI results, etc.).

## If invoked with no arguments

Explain how watching works and show current watch status. Do NOT prompt for input — just explain and let the user invoke `/watch <resource>` when ready.

Print something like:

> **Resource watching** monitors PRs and Jira issues for changes and delivers updates to your `/inbox`.
>
> **Usage:**
> - `/watch PR #123` or `/watch https://github.com/owner/repo/pull/123`
> - `/watch RHOAIENG-456` or `/watch https://redhat.atlassian.net/browse/RHOAIENG-456`
>
> Watchers poll GitHub and Jira APIs periodically. When something changes (new comment, review, status change, CI result), it appears in your inbox.
>
> To stop watching a resource, use `/unwatch`.

Then run `handler watching --json` and show the current watch status (what's being watched, watcher health). If nothing is being watched, say so.

After printing the explanation and status, stop.

## With arguments: subscribe to a resource

The user's args contain a resource identifier — a PR number, Jira key, or URL. Parse it and subscribe.

### Step 1: Parse the resource

| User input | Resource format | URL |
|-----------|----------------|-----|
| `#123` or `PR #123` | Needs repo context — check `git remote get-url origin` to get `owner/repo`, then use `pr:owner/repo#123` | `https://github.com/owner/repo/pull/123` |
| `owner/repo#123` | `pr:owner/repo#123` | `https://github.com/owner/repo/pull/123` |
| `https://github.com/owner/repo/pull/123` | `pr:owner/repo#123` | (use the URL as-is) |
| `RHOAIENG-456` | `jira:RHOAIENG-456` | Look up from handler config |
| `https://...atlassian.net/browse/RHOAIENG-456` | `jira:RHOAIENG-456` | (use the URL as-is) |

### Step 2: Subscribe

```bash
handler subscribe --resource "<type:id>" --url "<url>"
```

### Step 3: Confirm

Tell the user what you subscribed to and that updates will appear in `/inbox`. Mention they can use `/unwatch` to stop watching.
