# agent-handler

## Build and Install

```bash
make build      # builds to bin/handler
make install    # atomically installs binary, runs non-interactive setup
make clean      # removes bin/
```

`make install` uses atomic rename so it's safe to run while handler is actively running. Use `NONINTERACTIVE=1 make install` to skip confirmation prompts and watcher setup. Always use `NONINTERACTIVE=1` when installing from a Claude session.

**NEVER copy the binary directly with `cp`.** Always use `make install` — it handles atomic rename and setup. Direct `cp` corrupts the binary if it's replaced while running (handler is invoked by hooks every few seconds).

Or without the repo:
```bash
go install github.com/mturley/agent-handler@latest
handler setup
```

Skills and hooks are embedded in the binary via `//go:embed`. `handler setup` extracts them to `~/.agent-handler/` and configures Claude Code. `handler uninstall` reverses everything including removing the binary.

## Test

```bash
go test ./...
```

## Project Structure

- `cmd/` — CLI commands (cobra). Each file is one subcommand.
- `db/` — SQLite data layer. All DB access goes through typed Go functions here.
- `discover/` — Claude session ID/name discovery from JSONL, PID cache, process liveness.
- `worktree/` — `.worktree-resources` file read/write.
- `hooks/` — Shell scripts for Claude Code hooks (SessionStart, UserPromptSubmit, PreCompact). See `docs/claude-hook-stdin.md` for the JSON fields available on stdin for each hook type.
- `skills/` — Claude Code skill markdown files. Each skill is a directory with a `SKILL.md`.

## Installation Model

`handler setup` extracts embedded skills and hooks to `~/.agent-handler/`, symlinks skills into `~/.claude/skills/`, and configures Claude Code hooks in settings.json. The binary goes to `$GOBIN` (via `go install` or `make install`). Data lives at `~/.agent-handler/`.

Skills and hooks are embedded into the binary at build time via `//go:embed` in `embedded.go`. The embed directives use glob patterns (`skills/*/SKILL.md`, `hooks/*.sh`), so new skills/hooks are picked up automatically as long as they follow the directory convention.

When adding or removing skills, update the `skillNames` slice in `cmd/uninstall.go` (install discovers skills from the embedded FS, but uninstall needs the list to know what to clean up).

When adding or removing hooks, update `configureHooks()` in `cmd/setup.go` and `removeHooks()` in `cmd/uninstall.go`.

**IMPORTANT: When adding, removing, or changing skills, commands, or capabilities, you MUST update both `rules/agent-handler.md` and `skills/using-handler/SKILL.md`.** The rules file is loaded automatically at every session start — it introduces agent-handler to the user with a quick reference of available skills and useful CLI commands. The skill is invoked once per session and teaches the agent how to emit events and use handler. Both must stay current.

## Watchers

External event watchers poll GitHub and Jira APIs for changes to subscribed resources. They run as one-shot commands scheduled via launchd (macOS) or cron (Linux).

- `config/` — Config file read/write and token validation
- `watcher/` — Shared framework (active resources, cursors, dedup) and scheduler
- `watcher/github/` — GitHub PR watcher using GraphQL API
- `watcher/jira/` — Jira issue watcher using REST API

When adding new watcher types:
- Create a new package under `watcher/<name>/`
- Add the service to `config.Config` and `config.IsServiceConfigured`
- Add the resource type mapping in `config.ResourceTypeToService`
- Add the service to `cmd/watcher/auth.go` prompts
- Add the service to `cmd/watcher/run.go` switch statement

## Handler Session

A session with `role = handler` acts as a command center. The `/handler` skill sets the role and starts a polling loop. The handler statusline shows global session counts and watcher status instead of per-session inbox.

Key commands: `handler triage` (what needs attention), `handler log --global` (cross-session timeline), `--to handler` in emit (role-based message routing).

The `role` column on the `sessions` table drives statusline behavior. `event_recipients` supports `recipient_type = 'role'` for role-based routing.

## .worktree-resources File

See [docs/worktree-resources.md](docs/worktree-resources.md) for the file format spec and integration guide.

## Design

Full design spec: `docs/superpowers/specs/2026-06-15-agent-handler-design.md`
Phase 1 implementation plan: `docs/superpowers/plans/2026-06-15-phase1-core-ledger.md`

## Key Conventions

- Event IDs are UUIDs (not auto-increment)
- All timestamps are ISO 8601 UTC
- Subscriptions use soft deletes (deleted_at field)
- Sessions are archived, never destroyed
- The CLI supports `--json` on all commands for machine-readable output
- The `handler` binary name is used everywhere — do not rename it without updating hooks and skills
