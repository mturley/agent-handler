# agent-handler

## Build and Install

```bash
make build    # builds to bin/handler
make install  # builds, copies to ~/.agent-handler/, symlinks skills + hooks, configures Claude settings
make clean    # removes bin/
```

Install is in the Makefile (not the binary) because it needs repo context to find skills and hooks. Uninstall is in the binary (`handler uninstall`) because it runs from the installed copy.

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

`make install` copies the binary, hooks, and skills into `~/.agent-handler/` and symlinks skills into `~/.claude/skills/`. This makes the installation independent of the source repo. `handler uninstall` reverses everything.

When adding or removing skills, update:
- The `SKILLS` variable in `Makefile`
- The `skillNames` slice in `cmd/uninstall.go`

When adding or removing hooks, update:
- The `HOOKS` variable and the python3 hook configuration block in `Makefile`
- The `removeHooks()` function in `cmd/uninstall.go`

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
