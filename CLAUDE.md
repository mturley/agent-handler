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

## .worktree-resources File

The `.worktree-resources` file lives in the root of a git worktree (gitignored). It declares which external resources this worktree cares about. Any tool can read or write this file — it's the portable contract between handler and other worktree-aware tools.

**Format:** One resource per line, `<type>:<id> <url>`:

```
pr:owner/repo#123 https://github.com/owner/repo/pull/123
jira:RHOAIENG-12345 https://redhat.atlassian.net/browse/RHOAIENG-12345
```

**Supported resource types:**
- `pr` — GitHub pull request. ID format: `owner/repo#number`
- `jira` — Jira issue. ID format: issue key (e.g. `RHOAIENG-12345`)

**Behavior:**
- `handler register` reads this file on session start and auto-subscribes to listed resources
- `handler subscribe` appends new entries
- `handler unsubscribe` removes entries
- Malformed lines (missing URL, empty lines) are silently skipped
- Duplicate entries are deduplicated on append

**Integration with other tools:**
Any tool that sets up a worktree can seed this file. For example, a worktree creation script that associates a worktree with a PR:
```bash
echo "pr:owner/repo#123 https://github.com/owner/repo/pull/123" >> .worktree-resources
```
Handler will pick up these subscriptions on the next session start in that worktree.

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
