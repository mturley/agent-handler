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

This also updates the `.worktree-resources` file so other tools can see what this worktree cares about.

## When to subscribe

- When you start working on a PR
- When you start working on a Jira issue
- When you want to watch a CI job

## Unsubscribing

```bash
handler unsubscribe --resource "pr:owner/repo#123"
```
