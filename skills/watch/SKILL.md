---
name: watch
description: "agent-handler: Watch a PR, Jira issue, or Slack thread for changes. Use when the user says 'watch PR #123', 'watch RHOAIENG-456', or provides a GitHub/Jira/Slack URL to monitor."
---

# /watch — Watch a Resource for Changes

Subscribe to a PR, Jira issue, or Slack thread so you get notified in `/inbox` when it changes (new comments, reviews, status changes, CI results, thread replies, etc.).

## If invoked with no arguments

Explain how watching works and show current watch status. Do NOT prompt for input — just explain and let the user invoke `/watch <resource>` when ready.

Print something like:

> **Resource watching** monitors PRs, Jira issues, and Slack threads for changes and delivers updates to your `/inbox`.
>
> **Usage:**
> - `/watch PR #123` or `/watch https://github.com/owner/repo/pull/123`
> - `/watch RHOAIENG-456` or `/watch https://redhat.atlassian.net/browse/RHOAIENG-456`
> - `/watch https://your-workspace.slack.com/archives/C0123ABCD/p1787257539775119`
>
> Watchers poll GitHub, Jira, and Slack APIs periodically. When something changes (new comment, review, status change, CI result, thread reply), it appears in your inbox.
>
> To stop watching a resource, use `/unwatch`.

Then run `handler watching --json` and show the current watch status (what's being watched, watcher health). If nothing is being watched, say so.

After printing the explanation and status, stop.

## With arguments: subscribe to a resource

The user's args contain a resource identifier — a PR number, Jira key, or a GitHub/Jira/Slack URL. Parse it and subscribe.

### Step 1: Parse the resource

| User input | Resource format | URL |
|-----------|----------------|-----|
| `#123` or `PR #123` | Needs repo context — check `git remote get-url origin` to get `owner/repo`, then use `pr:owner/repo#123` | `https://github.com/owner/repo/pull/123` |
| `owner/repo#123` | `pr:owner/repo#123` | `https://github.com/owner/repo/pull/123` |
| `https://github.com/owner/repo/pull/123` | `pr:owner/repo#123` | (use the URL as-is) |
| `RHOAIENG-456` | `jira:RHOAIENG-456` | Look up from handler config |
| `https://...atlassian.net/browse/RHOAIENG-456` | `jira:RHOAIENG-456` | (use the URL as-is) |
| `https://<workspace>.slack.com/archives/<CHANNEL>/p<TS>` | `slack:<CHANNEL>:<ts>` (see below) | (use the URL as-is) |

**Slack thread URLs:** the last path segment is `p` followed by the message
timestamp with the dot removed — e.g. `p1787257539775119`. Restore the dot 6
digits from the right to get the `ts`: `1787257539775119` → `1787257539.775119`.
Combine with the channel id to form `slack:<CHANNEL>:<ts>`, e.g.
`slack:C069KSM8T9N:1787257539.775119`. A `?thread_ts=…` query param, when
present, is the thread's root `ts` — prefer it over the path `p…` if they
differ (the path may point at a reply). Slack watching requires Slack creds in
the shared watcher config (`~/.config/watcher/auth.yaml`, via `worktree setup`);
if `handler watching` shows the Slack watcher unconfigured, tell the user to run
`worktree setup` (or `handler watcher auth slack`) first.

### Step 2: Subscribe

```bash
handler subscribe --resource "<type:id>" --url "<url>"
```

### Step 3: Confirm

Tell the user what you subscribed to and that updates will appear in `/inbox`. Mention they can use `/unwatch` to stop watching.
