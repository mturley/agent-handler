# Phase 5c: Web UI — External Resources Tab

## Overview

The External Resources tab provides a resource-centric view of all watched PRs and Jira issues. Resources are grouped by type (PRs, Jira) with sort controls, showing cached state from watchers, which sessions subscribe to each resource, and watcher health status.

---

## API Endpoint

### `GET /api/resources`

Returns all resources with cached state and subscription information.

**Response:**

```json
{
  "resources": [
    {
      "resource_type": "pr",
      "resource_id": "opendatahub-io/odh-dashboard#8485",
      "resource_url": "https://github.com/opendatahub-io/odh-dashboard/pull/8485",
      "state": {
        "title": "feat: add retry logic",
        "state": "open",
        "author": "mturley",
        "review_decision": "CHANGES_REQUESTED",
        "has_new_commits_since_review": true,
        "ci_status": "SUCCESS",
        "is_draft": false
      },
      "resource_updated_at": "2026-07-20T14:30:00Z",
      "watcher_updated_at": "2026-07-22T10:00:00Z",
      "sessions": [
        {
          "session_id": "abc-123",
          "session_name": "auth-feature",
          "display_state": "active"
        }
      ]
    }
  ],
  "watcher_status": {
    "github": {
      "configured": true,
      "installed": true,
      "last_success": "2026-07-22T10:00:00Z",
      "last_error": null,
      "has_error": false
    },
    "jira": {
      "configured": true,
      "installed": true,
      "last_success": "2026-07-22T09:58:00Z",
      "last_error": null,
      "has_error": false
    }
  }
}
```

---

## Layout

### Watcher Health Bar

At the top of the tab, a small status bar:
- GitHub watcher: green checkmark or red X, with "last run N ago"
- Jira watcher: same
- If a watcher has errors, show the error message in red

### Resource Groups

Two collapsible sections: **Pull Requests** and **Jira Issues**.

**Sort controls** per group:
- Default: urgency (failing CI / changes requested / blocked first)
- Alternative: recent activity (most recently updated resource first)

### Resource Card

**PR card (visible without expanding):**
- Title (bold), linked to GitHub URL (external link icon)
- State badge: open (green), merged (purple), closed (red)
- Review decision badge: approved (green), changes requested (amber), review required (gray)
- CI status badge: success (green), failure (red), pending (yellow)
- Author
- Subscribing sessions: small pills with session names + Switch buttons (cmux only)

**Jira card (visible without expanding):**
- Summary (bold), linked to Jira URL (external link icon)
- Status badge (e.g. "In Progress" in blue, "To Do" in gray)
- Priority indicator (icon + label: Blocker, Critical, Major, Normal, Minor)
- Assignee
- Subscribing sessions: same as PR

**Expandable details (click to show):**
- PR: has_new_commits_since_review, is_draft, watcher freshness
- Jira: blocked status, blocked reason, epic link, story points, labels, watcher freshness

### Cross-tab Navigation

- Each resource card has a "Timeline" button (Clock icon) that switches to the Timeline tab filtered by events related to that resource
- Session pills are clickable — clicking switches to the Sessions tab filtered by that session name
- Switch buttons work the same as on the Sessions tab (POST to /api/actions/switch, toast feedback)

### Responsive

At narrow widths (< 480px):
- Resource cards stack with session pills wrapping
- Sort controls collapse to icon buttons
- Expandable details take full width

---

## Files to Create/Modify

### New (backend)
- `cmd/api/resources.go` — `GET /api/resources` endpoint

### Modified (backend)
- `cmd/api/server.go` — register route

### New (frontend)
- `ui/src/pages/ResourcesPage.tsx` — the Resources tab page
- `ui/src/components/ResourceCard.tsx` — individual resource card
- `ui/src/components/WatcherStatus.tsx` — watcher health bar
- `ui/src/hooks/useResources.ts` — data hook

### Modified (frontend)
- `ui/src/App.tsx` — wire Resources tab (replace placeholder)
- `ui/src/api/client.ts` — add `getResources()` fetch wrapper
- `ui/src/api/types.ts` — add resource types
