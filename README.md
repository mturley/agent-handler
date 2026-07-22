# agent-handler

Manage parallel Claude Code sessions: SQLite event ledger, pub/sub session inboxes, GitHub and Jira resource watchers, statusline enhancements, terminal peeking, cmux integrations and (WIP) web dashboard.

![Screenshot of Claude Code statusline with agent-handler installed](docs/images/handler-inbox.png)

## Install

Requires Go 1.22+ and Claude Code to already be installed.

```bash
git clone https://github.com/mturley/agent-handler.git
cd agent-handler
make build
make install # Copies `handler` binary to /usr/local/bin and runs `handler setup`
```

`handler setup` creates a directory at `~/.agent-handler/`, copies skill and hook files there, and configures Claude Code hooks and skills automatically. It will show you what it does and ask for confirmation before proceeding. If you skip any of its steps (e.g. there are issues authenticating the watchers), run `handler setup` again to retry.

A more convenient install/update script will come soon.

## Update

```bash
cd agent-handler
git pull
make build && make install
```

## Uninstall

```bash
handler uninstall
```

The binary and skill/hook configuration will be cleaned up, but your database and configuration will remain in `~/.agent-handler`. To fully clean up your installation you can delete that directory.

## Usage

Most features work immediately in new and existing Claude Code sessions — the hooks and rules file are loaded automatically after `handler setup`, so sessions will start registering, emitting events, and showing the statusline right away.

The optional [web UI](#web-ui) provides a visual dashboard and needs to be started separately:

```bash
handler ui
```

Run this from within [cmux](https://cmux.dev) if you want cmux-specific features like session switching. The UI is available at http://localhost:8420.

For development with hot reload:

```bash
make dev    # requires mprocs; uses air for Go auto-reload if installed
```

This starts both the Go API server and the Vite dev server. Use the UI at http://localhost:5173.

## Key Commands

These are the commands you can use directly from your terminal:

```bash
handler status          # Show all sessions with liveness and unread counts
handler log --global    # Cross-session event timeline
handler triage          # What needs attention across all sessions
handler tail            # Live event stream
handler cost            # API cost breakdown (today/month/per-session)
handler switch          # Interactive session switcher (cmux)
handler switch -a       # Jump to first session awaiting approval (cmux)
handler claude          # Start a peekable Claude session
handler watching        # Show watched resources and watcher status
handler health          # Database health and statistics
handler cleanup         # Archive dead sessions
handler query "SQL"     # Run ad-hoc read-only SQL
```

There are also commands used by hooks and skills (`emit`, `peek`, `register`, `unread`, `statusline`, etc.) that you won't need to run directly. Run `handler --help` for the full list.

## How It Works

Sessions auto-register on their first prompt — you don't need to do anything. The UserPromptSubmit hook detects new sessions and registers them with the current git repo, branch, and terminal environment.

Once registered, sessions emit events to a central SQLite ledger as they work. The global rules file (`~/.claude/rules/agent-handler.md`) teaches each session what events to emit and when — milestones, decisions, blockers, status check-ins. Other sessions and the handler can see these events, enabling cross-session awareness.

### Hooks

Hooks wire Claude Code session lifecycle events to handler:
- **UserPromptSubmit** — registers sessions on first prompt, heartbeat, event injection based on inbox mode, auto-catchup summary on user return
- **SessionEnd** — archives the session and soft-deletes subscriptions
- **Statusline** — heartbeat, session metadata sync, unread notifications, awaiting-approval scan
- **PreCompact** — snapshots context before compaction

### Slash commands

These are available as `/slash-commands` in any Claude session:
- `/inbox` — check and act on unread events
- `/inbox-clear` — dismiss unread events without reading them
- `/inbox-mode` — configure manual, on-submit, or auto delivery
- `/catchup` — in auto-inbox mode, summarize auto-delivered events since the last `/catchup`
- `/watch` / `/unwatch` — subscribe to PRs and Jira issues
- `/watching` — show watched resources and watcher status
- `/message` — send messages to other sessions
- `/done` — log a completion summary before closing a session
- `/handler` — turn a session into a command center for all sessions
- `/handler-debug` — debug session identity and inbox state

## Inbox Modes

Each session has an inbox mode that controls how it receives events from other sessions and watchers:

| Mode | Behavior |
|------|----------|
| **manual** (default) | The statusline shows an unread count. The agent checks with `/inbox` when you ask. |
| **on-submit** | The UserPromptSubmit hook notifies the agent of unread messages on every prompt, so it checks `/inbox` automatically before responding. |
| **auto** | A cron job polls for new events every minute and invokes `/inbox --auto` in the background. When you return, use `/catchup` for a summary of what happened. |

Use `/inbox-mode manual`, `/inbox-mode on-submit`, or `/inbox-mode auto` to switch. Auto mode sets up a session-scoped cron job that does not survive session restarts — inbox mode resets to manual when the session ends.

In auto mode, the agent processes events in the background but tracks what you've seen separately (dual cursor). When you send a prompt after being away, the hook detects undelivered events and prompts the agent to invoke `/catchup`, which summarizes everything from the conversation history — including what the agent did in response — and then advances your cursor.

## Handler Session

Use `/handler` in a Claude session to turn it into a command center for managing all active sessions. The handler session delivers a prioritized briefing combining triage data, terminal peek results, and a timeline of recent events. It gets a custom statusline showing active/blocked session counts, global event status, and aggregate API cost.

## External Watchers

Watch for external events (PR reviews, Jira comments, CI status) and deliver them to your sessions. Watchers cache current resource state (PR review status, Jira priority, blocked status) for use in triage.

### Setup

```bash
handler watcher install      # Configure tokens + install all authenticated watchers
```

Or step by step:
```bash
handler watcher auth         # Configure API tokens (GitHub, Jira)
handler watcher install github
handler watcher install jira
```

`handler watcher install` creates a scheduled job that runs `handler watcher run <service>` periodically. On macOS this creates a launchd plist; on Linux it adds a cron entry. Both poll at a configurable interval (default: every 2 minutes).

Alternatively, you can skip `handler watcher install` and schedule the watcher runs yourself with cron or any other scheduler:
```bash
# Example crontab entries (every 2 minutes)
*/2 * * * * /usr/local/bin/handler watcher run github
*/2 * * * * /usr/local/bin/handler watcher run jira
```

### Jira custom fields

Jira custom fields let the watcher fetch additional data (epic links, blocked status, story points, etc.) when polling issues. This data is cached in the resource state and available to `handler triage` for richer context. Configure them in `~/.agent-handler/config.yaml`:

```yaml
services:
  jira:
    custom_fields:
      blocked: customfield_10517        # Blocked flag
      blocked_reason: customfield_10483 # Blocked reason (rich text)
      epic_key: customfield_10014       # Epic link
      flagged: customfield_10021        # Impediment flag
      story_points: customfield_10028   # Story points estimate
      git_pull_request: customfield_10875 # Linked PR
```

Default custom fields are added automatically during `handler watcher auth`. The field IDs above are common for Jira Cloud but may differ for your instance — check your Jira admin or use the Jira REST API to find the right IDs.

### Management

```bash
handler watcher list         # Show installed watchers and status
handler watcher stop         # Pause all watchers (or: handler watcher stop github)
handler watcher start        # Resume paused watchers (or: handler watcher start github)
handler watcher logs github  # View watcher logs
handler watcher run github   # Run once manually
handler watcher uninstall    # Remove all watchers (or: handler watcher uninstall github)
```

## cmux Integration

When running inside [cmux](https://cmux.dev), agent-handler integrates deeply with the terminal environment:

- **Session switching** — `handler switch` navigates to any session's cmux workspace and surface tab, with an interactive mode featuring readline tab completion
- **Keyboard shortcuts** — `handler setup` configures cmux actions for quick session switching:
  - `cmd+shift+a` — jump to the first session awaiting approval
  - `cmd+shift+s` — interactive session switcher
- **Workspace tracking** — sessions store their cmux workspace ID, name, and color; `handler status` groups sessions by repo and workspace with colored indicators
- **Awaiting approval detection** — the statusline scans other sessions for approval prompts and shows the keyboard shortcut to jump to them
- **Terminal notifications** — flash and notify via cmux's native notification system when new events arrive

All cmux features degrade gracefully outside cmux — the statusline adapts, keyboard shortcuts don't render, and `handler switch` exits with a clear error.

## Session Inspection (Peek)

Inspect live Claude sessions from other sessions or the handler. Supports cmux (primary) and tmux (fallback) terminal environments.

```bash
handler claude                     # Start a peekable Claude session
handler peek --session <id>        # Capture terminal content
handler status                     # Shows 👁 indicator for peekable sessions
```

Sessions started via `handler claude` or in cmux are automatically peekable. The handler session uses peek via subagents to detect sessions waiting for input.

## Cost Tracking

Track Claude API spend across all sessions with daily rollups and reset detection. Cost data is captured automatically from the statusline hook (every ~10s). This is especially useful in environments like Vertex AI where Anthropic's billing dashboard and Admin API are unavailable.

```bash
handler cost                    # Summary header + current month breakdown
handler cost --today            # Today's spend by session
handler cost --month 2026-06    # Specific month breakdown
handler cost --session <id>     # Single session detail (true cost, adjustments, model)
handler cost --json             # Machine-readable output
```

**Reset detection:** Claude Code's in-memory cost counter resets when a laptop restarts and a session resumes. handler detects this (new value lower than last snapshot) and records a cost adjustment, preserving the true lifetime total for each session.

### Experimental: enhanced cost display

> **Note:** Enhanced cost display is experimental. The accuracy of cost figures has not been fully validated — they are derived from Claude Code's own `total_cost_usd` field, which may not match your actual billing. Use `handler cost` output as a rough guide, not as a billing source of truth.

To enable enhanced cost display in the statusline, add this to `~/.agent-handler/config.yaml`:

```yaml
experimental:
  cost_display: true
```

When enabled:
- Every session's model line shows true session cost (with reset adjustments) plus today's spend: `$39.07 ($18.42 today)`
- The handler session shows aggregate cost across all sessions: `Cost (all sessions): $48.23 today · $342.17 this month · $280.44 Jun`

When disabled (the default), the statusline shows the raw cost value from Claude Code without adjustments or daily breakdowns. The `handler cost` CLI command works regardless of this setting.

## Web UI

A dark-mode web dashboard served by `handler ui`. Built with React, TypeScript, Tailwind CSS, and shadcn/ui. Responsive down to 400px for use in narrow cmux browser panes.

### Sessions Tab

View all active and idle sessions grouped by repo and cmux workspace. Features:
- **Fuzzy search** across session names and branches
- **Filter chips** — Active, Idle, Awaiting approval, Has unread — each showing a count
- **Sort options** — Match cmux tab order (default), Last prompt, Unread count, Name
- **Collapsible grouping** by repo and workspace, with colored workspace bars matching cmux
- **Session cards** with state badges, unread event counts with type breakdowns, resource subscription counts, and cmux Switch buttons
- **Inbox dialog** — view unread events for a session, dismiss them, or switch to the session
- **Attention summary** — highlights sessions awaiting approval and sessions with unread messages

### Timeline Tab

A chronological event feed in a chat-style layout with a vertical timeline, colored type dots, and expandable event bubbles. Features:
- **Infinite scroll** — loads older events as you scroll down
- **Live updates** — new events appear at the top via SSE
- **Full filtering** — by session, event type, source, and free-text search
- **Expandable details** — click events to see full body content
- **External resource links** — PR and Jira events link directly to their URLs
- **Cross-tab navigation** — click a session card's Timeline button to jump to its events, or click a session name on an event to jump back to the Sessions tab

### cmux Integration

When `handler ui` is started from within cmux, session Switch buttons are enabled — clicking one navigates cmux to that session's workspace and surface tab. Outside cmux, Switch buttons are hidden and a warning is shown at startup.

## Design

See [docs/superpowers/specs/2026-06-15-agent-handler-design.md](docs/superpowers/specs/2026-06-15-agent-handler-design.md) for the original design spec. A lot has changed since that design, and I've preserved [superpowers](https://claude.com/plugins/superpowers) specs from features I've implemented if you want to explore the evolution of this tool.
