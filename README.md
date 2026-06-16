# agent-handler

Centralized logging, publish/subscribe messaging, event handling and external resource watching for code agents.

## Install

### From source (requires Go 1.22+)

```bash
git clone https://github.com/mturley/agent-handler.git
cd agent-handler
make build
make install
```

### Using `go install`

```bash
go install github.com/mturley/agent-handler@latest
handler install
```

Both methods install to `~/.agent-handler/` and configure Claude Code hooks and skills automatically. The `handler install` command shows what it will do and asks for confirmation before proceeding.

To update, pull the latest changes (or re-run `go install`) and run `handler install` again.

### Uninstall

```bash
handler uninstall
```

## Key Commands

```bash
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

## Claude Code Integration

Hooks wire Claude Code session lifecycle events to handler:
- **SessionStart** -- auto-registers sessions, shows catch-up summary of missed events
- **UserPromptSubmit** -- heartbeat, optional event injection based on inbox mode
- **PreCompact** -- snapshots context before compaction

Skills teach agents how to interact with handler:
- `/inbox` -- check and act on unread events
- `/inbox_mode` -- configure manual, on-submit, or auto delivery

## Design

See [docs/superpowers/specs/2026-06-15-agent-handler-design.md](docs/superpowers/specs/2026-06-15-agent-handler-design.md) for the full design spec.
