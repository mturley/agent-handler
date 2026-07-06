# Resource State Caching and Handler Briefing — Design Spec

## Overview

This spec adds resource state caching to the watcher system and rewrites the `/handler` skill to deliver a prioritized briefing that combines triage data, resource state, peek results, and a timeline of recent events.

**Two components:**
1. **Resource state caching** — watchers cache the current state of watched resources (PR status, Jira priority, etc.) in a new `resource_state` table. Jira custom fields are configurable per-instance. Triage includes resource state in its output and triggers catch-up polls for stale data.
2. **Handler briefing** — the `/handler` skill is rewritten to peek at all sessions (via Haiku subagents), combine peek results with triage data and resource state, and present a prioritized list of action items plus a timeline.

---

## Resource State Table

New table that caches the current state of watched resources:

```sql
CREATE TABLE IF NOT EXISTS resource_state (
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    state_json TEXT NOT NULL,
    resource_updated_at TEXT NOT NULL,
    watcher_updated_at TEXT NOT NULL,
    PRIMARY KEY (resource_type, resource_id)
);
```

- **One row per resource**, shared across all sessions subscribing to it
- **`state_json`** — type-specific JSON blob (different shape for PRs vs Jira issues)
- **`resource_updated_at`** — when the resource was last modified in Jira/GitHub (from the API response)
- **`watcher_updated_at`** — when our watcher last polled and refreshed this row
- **Cleanup:** when the last subscription for a resource is soft-deleted, delete its `resource_state` row in the same transaction

### PR state_json

```json
{
  "title": "feat: add retry logic",
  "state": "open",
  "author": "mturley",
  "review_decision": "CHANGES_REQUESTED",
  "has_new_commits_since_review": true,
  "ci_status": "SUCCESS",
  "is_draft": false
}
```

- `review_decision` — derived from the latest non-dismissed review per unique reviewer. If any reviewer's latest review is `CHANGES_REQUESTED`, the decision is `"CHANGES_REQUESTED"`. If all are `APPROVED`, the decision is `"APPROVED"`. If reviews exist but none are approvals or changes-requested, the decision is `"REVIEW_REQUIRED"`. If no reviews exist, the decision is `"NONE"`
- `has_new_commits_since_review` — true if the latest commit is newer than the latest review
- `ci_status` — derived from check runs. Values: `"SUCCESS"`, `"FAILURE"`, `"PENDING"`, `"NONE"`
- `resource_updated_at` — from `PRData.UpdatedAt`

### Jira state_json

**Hardcoded base fields** (always included, fetched for every Jira instance):

| Field | API field name | Extraction |
|-------|---------------|------------|
| `summary` | `summary` | direct string |
| `status` | `status` | `.name` |
| `priority` | `priority` | `.name` |
| `assignee` | `assignee` | `.displayName` |
| `issue_type` | `issuetype` | `.name` |
| `labels` | `labels` | direct array |
| `created_at` | `created` | ISO 8601 timestamp |
| `updated_at` | `updated` | ISO 8601 timestamp |

- `resource_updated_at` — from the `updated` field

**Configurable custom fields** (per-instance, see Config section):

Additional fields are configured in `config.yaml` and merged into `state_json` using the configured display name as the key.

Example with custom fields configured:
```json
{
  "summary": "Unsupported resource annotation filtering utility",
  "status": "In Progress",
  "priority": "Major",
  "assignee": "Mike Turley",
  "issue_type": "Story",
  "labels": ["dashboard-area-model-serving"],
  "created_at": "2026-06-17T19:07:01Z",
  "updated_at": "2026-07-02T17:39:25Z",
  "epic_key": "RHOAIENG-69725",
  "blocked": "False",
  "story_points": 1
}
```

### Custom field value extraction

The watcher uses a generic extraction strategy for custom field values from the Jira API response:

- Object with `.value` key → use the `.value` string (e.g. `{"value": "False", "id": "10853"}` → `"False"`)
- Object with `.name` key → use the `.name` string (e.g. `{"name": "Critical", "id": "10001"}` → `"Critical"`)
- String, number, or null → use directly
- Array → store as-is

This covers all common Jira field types without needing type-specific parsing per field.

---

## Configurable Jira Custom Fields

Custom fields are configured in `config.yaml` under `services.jira.custom_fields`. Each entry maps a display name (used as the key in `state_json`) to a Jira field ID.

### Config format

```yaml
services:
  jira:
    url: https://redhat.atlassian.net
    email: mturley@redhat.com
    token: "..."
    bot_usernames: [...]
    # Custom Jira fields to include in resource state.
    # Map a display name to the Jira field ID.
    # These are fetched on each watcher poll and cached for triage reports.
    # Adding custom fields (e.g. blocked status, epic links) provides
    # additional context when the handler session triages work across sessions.
    # Field values are auto-extracted: objects use .value or .name,
    # strings/numbers/nulls are stored directly, arrays are preserved.
    # Uncomment and adjust field IDs for your Jira instance:
    # custom_fields:
    #   epic_key: "customfield_10014"        # Epic Link
    #   blocked: "customfield_10517"         # Blocked (True/False)
    #   blocked_reason: "customfield_10483"  # Blocked Reason
    #   flagged: "customfield_10021"         # Flagged/Impediment
    #   story_points: "customfield_10028"    # Story Points
```

### Config struct change

```go
type JiraConfig struct {
    URL          string            `yaml:"url"`
    Email        string            `yaml:"email"`
    Token        string            `yaml:"token"`
    BotUsernames []string          `yaml:"bot_usernames,omitempty"`
    CustomFields map[string]string `yaml:"custom_fields,omitempty"`
}
```

### `handler setup` update

After Jira configuration, print:
```
Custom Jira fields can be configured in config.yaml under services.jira.custom_fields.
Adding custom fields (e.g. priority, blocked status, epic links) provides additional
context when the handler session triages work across sessions.
See the commented examples in config.yaml for common fields.
```

---

## Watcher State Updates

Both watchers gain a "write resource state" step after processing each resource.

### Jira watcher changes

1. Add `priority`, `issuetype`, `created`, `updated` to the hardcoded `fields` parameter in `FetchIssue`
2. Read configured `custom_fields` from config, append those field IDs to the API request
3. Parse base fields into `IssueData` struct (gains `Priority`, `IssueType`, `Created`, `Updated` fields)
4. Parse custom fields using the generic extraction strategy into `IssueData.CustomFields map[string]interface{}`
5. After processing events, build `state_json` from base fields + custom fields
6. Upsert `resource_state` with the JSON, `resource_updated_at` from the issue's `updated` field, and `watcher_updated_at` as now
7. Remove the hardcoded `customfield_12311140` (Red Hat DC-era epic link) — epic link is now configurable via `custom_fields`

### GitHub watcher changes

1. The GraphQL query already fetches everything needed (state, reviews, check runs, commits)
2. After processing events, derive state summary from `PRData`:
   - `review_decision`: scan reviews — latest non-dismissed review per reviewer, count approvals vs changes-requested
   - `has_new_commits_since_review`: compare latest commit timestamp vs latest review timestamp
   - `ci_status`: scan check runs — all success → `"SUCCESS"`, any failure → `"FAILURE"`, any pending → `"PENDING"`, none → `"NONE"`
3. Upsert `resource_state` with the JSON, `resource_updated_at` from `PRData.UpdatedAt`, and `watcher_updated_at` as now

### DB functions

- `db.UpsertResourceState(resourceType, resourceID, stateJSON, resourceUpdatedAt, watcherUpdatedAt string) error`
- `db.GetResourceState(resourceType, resourceID string) (*ResourceState, error)`
- `db.ListResourceStatesForSession(sessionID string) ([]ResourceState, error)` — joins with subscriptions
- `db.DeleteResourceState(resourceType, resourceID string) error`

### Cleanup on unsubscribe

When soft-deleting a subscription, check if it was the last active subscription for that resource. If so, delete the `resource_state` row in the same transaction.

---

## Enhanced `handler triage`

Triage output gains `session_resources` and `stale_resources` sections.

### New JSON output fields

```json
{
  "session_resources": [
    {
      "session_id": "abc-123",
      "session_name": "auth-feature",
      "resources": [
        {
          "resource_type": "jira",
          "resource_id": "RHOAIENG-69748",
          "resource_url": "https://redhat.atlassian.net/browse/RHOAIENG-69748",
          "state": { ... },
          "watcher_updated_at": "2026-07-02T19:45:00Z"
        }
      ]
    }
  ],
  "stale_resources": [
    {
      "resource_type": "pr",
      "resource_id": "opendatahub-io/odh-dashboard#8312",
      "watcher_updated_at": "2026-07-02T18:30:00Z",
      "stale_minutes": 15
    }
  ]
}
```

### Freshness catch-up flow

1. Query all subscribed resources and their `watcher_updated_at`
2. For any resource older than 5 minutes where the watcher service is configured: run `handler watcher run <service> --resources <list>` in the background
3. Read state from DB regardless (may be stale, but still useful)
4. Report stale resources in the output so the consumer knows what might be outdated

Catch-up is best-effort. If the watcher run errors, triage still completes with whatever state is cached.

---

## `/handler` Skill Rewrite

The `/handler` skill becomes a proactive briefing combining triage, peek, and timeline.

### On invocation (or re-invocation)

1. **Set role** (if not already): `handler configure --role handler`

2. **Gather data** (parallel where possible):
   - `handler triage --json` — sessions, resources, blockers, unread counts, resource state
   - `handler log --global --since-cursor --json` — timeline since last report

3. **Peek at all peekable sessions** — spawn a Haiku subagent per peekable active/idle session. Each subagent:
   - Runs `handler peek --session <id> --json`
   - Answers one focused question: "Is this session waiting for user input (permission prompt, question, or approval)? If yes, what exactly is it asking? If no, say 'working' or 'idle at prompt'."
   - Returns a 1-2 sentence summary

4. **Present a prioritized briefing** with three sections:

   **Action Items** — ordered by priority, handler agent judges based on context but defaults to:
   1. Sessions waiting for permission prompts or asking questions (from peek)
   2. Blocked sessions (from triage `blocked_sessions`)
   3. Unread external events needing response — PR reviews with changes requested, new comments on your PRs, Jira status changes (from triage `sessions_with_unread` + resource state)
   4. Stale resources that couldn't be refreshed (from triage `stale_resources`)
   
   Priority is weighted by resource importance: a session working on a Blocker/Critical Jira issue ranks higher than one on a Normal issue. A PR with failing CI ranks higher than one with passing CI.

   **Timeline** — chronological list of events since last report (from `handler log --global --since-cursor`). Grouped by session, showing milestones, decisions, status updates, and external events.

   **Session Overview** — table of all sessions: name, branch, display state, peek summary, subscribed resources with their current state.

5. **Advance cursor** after presenting.

6. **Set up polling loop** (if not already running — check CronList first):
   ```
   CronCreate: every 1 minute, session-scoped, non-durable
   Prompt: check handler log --global --since-cursor and handler unread --count.
   If new events or direct messages, summarize them.
   ```

### Skill principles

- Always use subagents for peek — never read raw captures in handler context
- Use Haiku for peek subagents (focused question, fast, cheap)
- Combine resource state with peek results for priority judgment
- Present action items first, then context (timeline, overview)

---

## Files to Create/Modify

**New:**
- `db/resource_state.go` — ResourceState struct, CRUD functions
- `db/resource_state_test.go` — tests

**Modified:**
- `db/schema.sql` — add `resource_state` table
- `db/db.go` — migration for new table (existing databases)
- `db/subscriptions.go` — cleanup resource_state on last unsubscribe
- `config/config.go` — add `CustomFields` to `JiraConfig`
- `watcher/jira/client.go` — add priority/issuetype/created/updated to fetch, add CustomFields to IssueData, accept config for custom fields
- `watcher/jira/poller.go` — upsert resource_state after processing each issue
- `watcher/github/poller.go` — upsert resource_state after processing each PR
- `watcher/github/graphql.go` — (if needed) derive review_decision, ci_status, has_new_commits
- `cmd/triage.go` — add session_resources and stale_resources to output, freshness catch-up
- `cmd/setup.go` — mention custom fields after Jira configuration
- `skills/handler/SKILL.md` — full rewrite for prioritized briefing with peek
- `skills/using-handler/SKILL.md` — mention resource state in triage description
