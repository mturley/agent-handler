---
name: watching
description: "agent-handler: Show watched resources, watcher status, and recent watcher errors for this session"
disable-model-invocation: true
---

# /watching — Show Watched Resources

## Usage

```bash
handler watching --json
```

If this session has the handler role (`handler configure --get role` returns `handler`), run `handler watching --global --json` instead of `handler watching --json`.

## After reading the data

1. Present the watched resources grouped by type (PRs, Jira issues, Slack threads)
   - **Always render resource IDs as clickable markdown links** using the `resource_url` field from the JSON. Example: instead of `jira:RHOAIENG-69748`, show `[RHOAIENG-69748](https://redhat.atlassian.net/browse/RHOAIENG-69748)`. For PRs: `[owner/repo#123](https://github.com/owner/repo/pull/123)`. For Slack threads, the raw id (`slack:<CHANNEL>:<ts>`) is not human-readable — render the link with the resource's custom name or cached thread title as the link text when the JSON provides one, falling back to the channel id.
2. Show watcher status: configured, installed, running, last run time
3. For any watcher in an error state (where `last_error` is more recent than `last_success` in the watcher status), show the error message and help troubleshoot:
   - "Could not resolve to a Repository" → the repo may be private and the GitHub token needs `repo` scope. Suggest re-running `handler watcher auth github` with a new token.
   - "401 Unauthorized" or "403 Forbidden" → the API token is invalid or expired. Suggest `handler watcher auth <service>` to update it. For the Slack watcher, `invalid_auth`/`not_authed` means the Slack session token/cookie expired (they rotate every 1–2 weeks) → suggest re-running `worktree setup` (or `handler watcher auth slack`).
   - "rate limit" → suggest increasing the polling interval with `handler watcher uninstall <name>` then `handler watcher install <name> --interval 10m`.
   - For other errors, show the error message and suggest `handler watcher logs <name>` for details.
4. If watchers are not installed for subscribed resource types, suggest running `handler watcher install`
5. Tell the user about `/watch` and `/unwatch`:

> Use `/watch` to start watching a PR, Jira issue, or Slack thread, or `/unwatch` to stop. For example:
> - `/watch #123` or `/watch RHOAIENG-456`
> - `/watch https://your-workspace.slack.com/archives/C0123ABCD/p1787257539775119`
> - `/unwatch #123` or `/unwatch RHOAIENG-456`

## CLI reference

To subscribe: `handler subscribe --resource "pr:owner/repo#123" --url "https://github.com/owner/repo/pull/123"`
To unsubscribe: `handler unsubscribe --resource "pr:owner/repo#123"`
Resource format is always `type:id` with the `--resource` flag. Slack threads use `slack:<CHANNEL>:<ts>` (e.g. `slack:C069KSM8T9N:1787257539.775119`).
