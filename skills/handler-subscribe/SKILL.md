---
name: handler-subscribe
description: "Watch a PR or Jira issue for changes. Use when the user asks to watch, subscribe to, or track a resource, or when starting work on a PR or Jira issue that should be monitored for reviews, comments, or status changes."
---

# /handler-subscribe — Subscribe to a Resource

Subscribe to receive events about an external resource (PR, Jira issue, etc.).

## Usage

```bash
handler subscribe \
    --resource "pr:owner/repo#123" \
    --url "https://github.com/owner/repo/pull/123"
```

Resource format is `type:id`. Supported types: `pr`, `jira`, `jenkins`, `slack`.

## Persistence

By default, subscriptions are session-scoped — they only apply to the current session. To also write the subscription to `.worktree-resources` so future sessions in this worktree auto-subscribe, add `--persist`:

```bash
handler subscribe \
    --resource "pr:owner/repo#123" \
    --url "https://github.com/owner/repo/pull/123" \
    --persist
```

**After subscribing, ask the user:** "Want me to persist this subscription for future sessions in this worktree?" If yes, re-run with `--persist` or run an additional subscribe with `--persist`.

## When to subscribe

- When you start working on a PR
- When you start working on a Jira issue
- When you want to watch a CI job

## Unsubscribing

```bash
handler unsubscribe --resource "pr:owner/repo#123"
```

To also remove from `.worktree-resources` (stop future sessions from auto-subscribing):

```bash
handler unsubscribe --resource "pr:owner/repo#123" --persist
```

**After unsubscribing, ask the user:** "Want me to also remove this from the worktree resources so future sessions won't auto-subscribe?"
