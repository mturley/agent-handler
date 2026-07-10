# agent-handler

Centralized logging, publish/subscribe messaging, event handling and external resource watching for code agents.

## Install

### From source (requires Go 1.22+)

```bash
git clone https://github.com/mturley/agent-handler.git
cd agent-handler
make build
make install # Copies `handler` binary to /usr/local/bin and runs `handler setup`
```

### Using `go install`

```bash
go install github.com/mturley/agent-handler@latest
handler setup
```

Note: `go install` puts the binary in `$(go env GOPATH)/bin`. If you get `command not found: handler`, make sure the go bin directory is on your PATH by adding this to your shell's rc file:
```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

Then run `handler setup` again.

`handler setup` creates a directory at `~/.agent-handler/`, copies skill and hook files there, and configures Claude Code hooks and skills automatically. It will show you what it does and ask for confirmation before proceeding.

## Update

If installed via `go install`:
```bash
handler update
```

If installed from source:
```bash
cd agent-handler
git pull
make build && make install
```

## Uninstall

```bash
handler uninstall
```

The binary and skill/hook configuration will be cleaned up, but your database and session data will remain in `~/.agent-handler`. To fully clean up your installation you can delete that directory.

## Key Commands

```bash
handler status      # Show all sessions with liveness and unread counts
handler emit        # Write an event to the ledger
handler unread      # Check unread events for a session
handler subscribe   # Subscribe to external resource events
handler triage      # What needs attention across all sessions
handler peek        # Capture terminal content of a live session
handler claude      # Start a peekable Claude session
handler tail        # Live event stream
handler query       # Run ad-hoc read-only SQL
handler schema      # Dump table definitions
handler health      # Database health and statistics
```

Run `handler --help` for the full command list, or `handler <command> --help` for details on any command.

## Claude Code Integration

Hooks wire Claude Code session lifecycle events to handler:
- **SessionStart** -- directs the agent to invoke `/using-handler` for ledger awareness
- **UserPromptSubmit** -- registers sessions on first prompt, heartbeat, event injection based on inbox mode
- **Statusline** -- session registration, session name sync, unread notifications
- **PreCompact** -- snapshots context before compaction

Skills teach agents how to interact with handler:
- `/inbox` -- check and act on unread events
- `/inbox-mode` -- configure manual, on-submit, or auto delivery
- `/watch` / `/unwatch` -- subscribe to PRs and Jira issues
- `/watching` -- show watched resources and watcher status
- `/message` -- send messages to other sessions
- `/done` -- log a completion summary before closing a session
- `/catchup` -- summarize events auto-delivered while the user was away
- `/handler` -- turn a session into a command center for all sessions

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

Jira custom fields (epic link, blocked status, story points, etc.) can be configured in `~/.agent-handler/config.yaml` under `services.jira.custom_fields`.

### Management

```bash
handler watcher list         # Show installed watchers and status
handler watcher stop         # Pause all watchers (or: handler watcher stop github)
handler watcher start        # Resume paused watchers (or: handler watcher start github)
handler watcher logs github  # View watcher logs
handler watcher run github   # Run once manually
handler watcher uninstall    # Remove all watchers (or: handler watcher uninstall github)
```

## Handler Session

Use `/handler` in a Claude session to turn it into a command center for managing all active sessions. The handler delivers a prioritized briefing combining triage data, terminal peek results, and a timeline of recent events.

```bash
handler configure --role handler   # Set session as handler
handler triage                     # What needs attention across all sessions
handler peek --session <id>        # Inspect what a session is doing
handler log --global               # Cross-session event timeline
handler log --global --since-cursor  # What changed since last check
handler emit --to handler          # Send a message to the handler session
```

The handler session gets a custom statusline showing active/blocked session counts and global event status.

## Session Inspection (Peek)

Inspect live Claude sessions from other sessions or the handler. Supports cmux (primary) and tmux (fallback) terminal environments.

```bash
handler claude                     # Start a peekable Claude session
handler peek --session <id>        # Capture terminal content
handler status                     # Shows 👁 indicator for peekable sessions
```

Sessions started via `handler claude` or in cmux are automatically peekable. The handler session uses peek via subagents to detect sessions waiting for input.

## .worktree-resources

The `.worktree-resources` file lets any tool declare which external resources a worktree cares about. See [docs/worktree-resources.md](docs/worktree-resources.md) for the format spec and integration guide.

## Design

See [docs/superpowers/specs/2026-06-15-agent-handler-design.md](docs/superpowers/specs/2026-06-15-agent-handler-design.md) for the full design spec.
