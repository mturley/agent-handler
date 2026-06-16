# agent-handler

## Build and Install

```bash
make build      # builds to bin/handler
make install    # copies binary to $GOBIN, runs handler setup
make clean      # removes bin/
```

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
- `hooks/` — Shell scripts for Claude Code hooks (SessionStart, UserPromptSubmit, PreCompact).
- `skills/` — Claude Code skill markdown files. Each skill is a directory with a `SKILL.md`.

## Installation Model

`handler setup` extracts embedded skills and hooks to `~/.agent-handler/`, symlinks skills into `~/.claude/skills/`, and configures Claude Code hooks in settings.json. The binary goes to `$GOBIN` (via `go install` or `make install`). Data lives at `~/.agent-handler/`.

Skills and hooks are embedded into the binary at build time via `//go:embed` in `embedded.go`. The embed directives use glob patterns (`skills/*/SKILL.md`, `hooks/*.sh`), so new skills/hooks are picked up automatically as long as they follow the directory convention.

When adding or removing skills, update the `skillNames` slice in `cmd/uninstall.go` (install discovers skills from the embedded FS, but uninstall needs the list to know what to clean up).

When adding or removing hooks, update `configureHooks()` in `cmd/install.go` and `removeHooks()` in `cmd/uninstall.go`.

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
