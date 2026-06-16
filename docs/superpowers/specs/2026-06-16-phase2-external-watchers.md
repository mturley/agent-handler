# Phase 2: External Watchers — Design Spec

## Overview

External event watchers poll GitHub and Jira APIs on a schedule and write events to the handler ledger for sessions subscribed to those resources. Watchers are stateless one-shot commands scheduled via launchd (macOS) or cron (Linux). They only poll resources that have at least one active subscribing session.

---

## Config and Auth

### Config file

`~/.agent-handler/config.yaml` stores API tokens and watcher settings:

```yaml
services:
  github:
    token: ghp_xxxxx
  jira:
    url: https://redhat.atlassian.net
    email: mturley@redhat.com
    token: xxxxx
    bot_usernames:
      - automation-bot
      - jira-bot
```

### `handler watcher auth`

Interactive setup for external services.

- No arguments: walks through all supported services, each optional (skip with Enter)
- `handler watcher auth github` / `handler watcher auth jira`: configure a specific service
- `handler watcher auth status`: show which services are configured (without revealing tokens)
- Re-running validates the existing token (or lets you replace it)
- After validating a token, offers to install the watcher: "Install the GitHub watcher to poll every 3 minutes? [y/N]"

Each service prompt includes a URL for creating the token:

**GitHub:**
```
GitHub Personal Access Token
  Create one at: https://github.com/settings/tokens
  Required scopes: repo (for private repos) or public_repo (for public only)
  Token:
```

**Jira:**
```
Jira API Token
  Create one at: https://id.atlassian.com/manage-profile/security/api-tokens
  Email:
  Token:
  Instance URL (e.g. https://yoursite.atlassian.net):
```

Token validation: makes a lightweight test API call (GitHub: `GET /user` via GraphQL, Jira: `GET /myself`) before saving.

### `handler setup` integration

`handler setup` runs `handler watcher auth` inline after the core setup (DB, hooks, skills, status line). The user can skip any service. For each configured service, it offers to install the watcher.

### Subscribe guard

`handler subscribe --resource "pr:owner/repo#42"` checks if the corresponding service is configured. If not: "GitHub is not configured. Run `handler watcher auth github` to set up API access."

Same guard on `handler watcher install <name>` and `handler watcher run <name>`.

---

## Watcher Framework

### CLI commands

| Command | Description |
|---------|-------------|
| `handler watcher run <name>` | One-shot: poll all active resources for this watcher, write events, exit |
| `handler watcher run <name> --resources "pr:owner/repo#1,pr:owner/repo#2"` | Poll specific resources only (used for catch-up) |
| `handler watcher install <name>` | Create launchd plist or cron entry to schedule polling |
| `handler watcher install <name> --interval 2m` | Custom polling interval |
| `handler watcher uninstall <name>` | Remove the scheduled job |
| `handler watcher list` | Show installed watchers, intervals, last run time, resource count |
| `handler watcher logs <name>` | Tail the watcher's log file |
| `handler watcher auth` | Interactive token setup (see above) |
| `handler watcher auth status` | Show which services are configured |

### Scheduling

- macOS: launchd plist at `~/Library/LaunchAgents/com.agent-handler.watcher-<name>.plist`
- Linux: cron entry via `crontab`
- Scheduled command: `handler watcher run <name>`
- Default intervals: GitHub 3 minutes, Jira 5 minutes (configurable)

### Active resource query

Watchers only poll resources with at least one active subscribing session:

```sql
SELECT DISTINCT sub.resource_type, sub.resource_id, sub.resource_url
FROM subscriptions sub
JOIN sessions s ON s.session_id = sub.session_id
WHERE sub.deleted_at IS NULL
  AND sub.resource_type = ?
  AND s.status = 'active'
```

No active sessions = watcher exits immediately with no API calls.

Resource type to watcher mapping: `pr` → GitHub watcher, `jira` → Jira watcher. The `resource_type` field in subscriptions drives which watcher polls the resource. The `source` field on events (`github`, `jira`) identifies which watcher wrote the event.

### Cursor logic

For each resource, the watcher finds the most recent event it wrote:

```sql
SELECT MAX(external_ts) FROM events
JOIN event_resources ON event_resources.event_id = events.id
WHERE event_resources.resource_type = ? AND event_resources.resource_id = ?
  AND events.source = ?
```

Only fetches activity after that timestamp. For new subscriptions with no prior events, the watcher records the current timestamp as its cursor and only reports future changes.

### First poll: `watch_started` event

On first poll for a new subscription, the watcher emits a single `watch_started` event summarizing the current resource state. This event is unaddressed (no recipients, no broadcast) — it's in the ledger for context but doesn't appear in any inbox.

Example: "Now watching PR owner/repo#123 — Open, 2 approved reviews, CI passing, 12 commits"

### Deduplication

Events are deduplicated by `(source, resource_id, type, external_ts)`. If a watcher re-fetches the same comment on consecutive runs, it won't create a duplicate event.

### Error events

Watcher errors (API failures, auth errors, rate limits) are emitted as `watcher_error` events with `event_resources` entries for the affected resources. This routes them to sessions subscribed to those resources via the existing subscription-based routing — no query changes needed.

### Logging

Each watcher run appends to `~/.agent-handler/data/logs/watcher-<name>.log`. Logs include: resources polled, events written, errors, API rate limit remaining.

### Catch-up on re-registration

When `handler register` detects active subscriptions for a session, it spawns `handler watcher run <name> --resources <list>` in the background for each watcher type that has subscribed resources. The watcher's cursor logic handles deduplication — it only fetches activity newer than the most recent event for each resource.

---

## GitHub PR Watcher

### API

GitHub GraphQL API via HTTPS. One batched query per watcher run covering all active PR subscriptions.

The query fetches per PR:
- PR state (open, merged, closed)
- Reviews (state, author, submittedAt)
- Comments on the conversation thread (author, createdAt, body preview)
- Review comments / inline comments (author, createdAt, path, body preview)
- Commit count and latest commit SHA
- Check runs with individual status per check (name, conclusion, completedAt)
- Last updated timestamp

### Event types

| Event Type | Trigger |
|------------|---------|
| `watch_started` | First poll for a new subscription. Summary of current PR state. Unaddressed. |
| `pr_comment` | New comment on the PR conversation thread |
| `pr_review_comment` | New inline review comment |
| `pr_review_requested` | Review requested from someone |
| `pr_approved` | PR approved |
| `pr_closed` | PR closed without merge |
| `pr_merged` | PR merged |
| `pr_reopened` | PR reopened |
| `pr_new_commits` | New commits pushed since last poll |
| `ci_check_passed` | A specific check run completed successfully. Title includes check name. |
| `ci_check_failed` | A specific check run failed. Title includes check name. |

Checks that remain `in_progress` across polls generate no events — only state transitions to `success`, `failure`, or `error` are reported.

### Author metadata

- `author`: GitHub username
- `author_type`: `human` or `bot` (from GitHub's user type field)

### Terminal state handling

When a PR is merged or closed:
1. Write the `pr_merged` or `pr_closed` event
2. Soft-delete all subscriptions for that resource in the same transaction
3. Future watcher runs won't poll it

### Reopen handling

If a watcher run discovers a PR was reopened (close event exists but PR state is now `open`), it writes a `pr_reopened` event and reinstates soft-deleted subscriptions.

### Rate limit awareness

After each API call, log the remaining rate limit from response headers. If remaining < 100, log a warning. The watcher does not retry on rate limit errors — it skips that run and tries again on the next schedule. Rate limit errors are emitted as `watcher_error` events attached to the affected resources.

---

## Jira Watcher

### API

Jira REST API v3. Each subscribed issue is polled individually.

### What it checks per issue

- Comments (via issue comments endpoint, filtered by date)
- Changelog entries for: status, assignee, description, labels
- Epic link field (for `resource_relationships` discovery)

### Event types

| Event Type | Trigger |
|------------|---------|
| `watch_started` | First poll. Summary of issue state. Unaddressed. |
| `jira_comment` | New comment on the issue |
| `jira_status_change` | Issue status transition |
| `jira_assigned` | Issue assigned or reassigned |
| `jira_description_changed` | Issue description edited |
| `jira_labels_changed` | Labels added or removed. Title shows changes: "+approved, -needs-review" |

### Author metadata

- `author`: Jira display name
- `author_type`: `human` by default. Usernames matching `bot_usernames` in config are marked `bot`.

### Epic link discovery

On each poll, the watcher reads the epic link field. If the issue belongs to an epic:
- Write a `resource_relationships` entry: `child: jira:RHOAIENG-100, parent: jira:RHOAIENG-50, relationship: epic_child`
- Include URLs for both child and parent
- Only write if the relationship doesn't already exist

### Terminal state handling

When an issue reaches a terminal status (Done, Resolved, Won't Fix, Closed):
1. Write the `jira_status_change` event
2. Soft-delete all subscriptions for that resource

---

## Changes to Existing Commands

### `handler setup`

After core setup, runs `handler watcher auth` inline. The user can skip any service. For each configured service, offers to install the watcher.

### `handler subscribe`

Checks if the corresponding service is configured before allowing subscription.

### `handler register`

After registration, if the session has active subscriptions, spawns background watcher runs for catch-up.

### `handler uninstall`

Additionally:
- Uninstalls any active watcher schedules (launchd/cron)
- Does not remove `~/.agent-handler/config.yaml` (preserved with other data)

---

## New File Structure

```
config/
├── config.go          # Read/write config.yaml, service auth validation
└── config_test.go
watcher/
├── watcher.go         # Shared framework: active resource query, cursor, dedup, event writing
├── watcher_test.go
├── github/
│   ├── github.go      # GraphQL client, PR polling logic
│   └── github_test.go
└── jira/
    ├── jira.go        # REST client, issue polling logic
    └── jira_test.go
cmd/watcher/
├── watcher.go         # Parent command for handler watcher subcommands
├── run.go             # handler watcher run
├── install.go         # handler watcher install
├── uninstall.go       # handler watcher uninstall
├── list.go            # handler watcher list
├── logs.go            # handler watcher logs
├── auth.go            # handler watcher auth
└── auth_status.go     # handler watcher auth status
```
