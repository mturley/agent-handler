# agent-handler

Centralized logging, publish/subscribe messaging, event handling and external resource watching for code agents.

## Getting Started

### Build

```bash
go build -o handler .
```

### Install

```bash
./handler install
```

This creates `~/.agent-handler/`, initializes the SQLite database, and symlinks skills into `~/.claude/skills/`.

### Key Commands

```bash
handler register    # Register a Claude Code session
handler status      # Show all sessions with liveness and unread counts
handler emit        # Write an event to the ledger
handler unread      # Check unread events for a session
handler subscribe   # Subscribe to external resource events
handler tail        # Live event stream
handler query       # Run ad-hoc read-only SQL
handler schema      # Dump table definitions
handler health      # Database health and statistics
```

Run `handler --help` for the full command list, or `handler <command> --help` for details on any command.

### Claude Code Integration

Hook scripts in `hooks/` wire Claude Code session lifecycle events to handler:
- `session_start.sh` -- auto-registers sessions, shows catch-up summary
- `user_prompt_submit.sh` -- heartbeat, optional event injection
- `pre_compact.sh` -- snapshots context before compaction

Skills in `skills/` teach agents how to interact with handler:
- `/inbox` -- check and act on unread events
- `/inbox_mode` -- configure manual, on-submit, or auto delivery

## Design

See [docs/superpowers/specs/2026-06-15-agent-handler-design.md](docs/superpowers/specs/2026-06-15-agent-handler-design.md) for the full design spec.
