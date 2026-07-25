# agent-handler

## Build and Install

```bash
make build      # builds to bin/handler
make install    # atomically installs binary, runs non-interactive setup
make clean      # removes bin/
```

`make install` uses atomic rename so it's safe to run while handler is actively running. Use `NONINTERACTIVE=1 make install` to skip confirmation prompts and watcher setup. Always use `NONINTERACTIVE=1` when installing from a Claude session.

**NEVER copy the binary directly with `cp`.** Always use `make install` — it handles atomic rename and setup. Direct `cp` corrupts the binary if it's replaced while running (handler is invoked by hooks every few seconds).

Building requires GHC and cabal (`brew install ghc cabal-install`), then `cabal build`.

Skills and hooks are embedded in the binary via Template Haskell (`file-embed`). `handler setup` extracts them to `~/.agent-handler/` and configures Claude Code. `handler uninstall` reverses everything including removing the binary.

## Test

```bash
cabal test
```

## Project Structure

- `app/Main.hs` — executable entry point; `src/Handler/Root.hs` is the command registry.
- `src/Handler/Cmd/` — CLI commands (optparse-applicative). Each module is one subcommand (or a small cluster).
- `src/Handler/Db/` — SQLite data layer (sqlite-simple). All DB access goes through typed functions here. Schema lives in `db/schema.sql` (embedded at build time).
- `src/Handler/Discover.hs` — Claude session ID/name discovery from JSONL, PID cache, process liveness.
- `src/Handler/Worktree.hs` — `.worktree-resources` file read/write.
- `hooks/` — Shell scripts for Claude Code hooks (SessionStart, UserPromptSubmit, PreCompact). See `docs/claude-hook-stdin.md` for the JSON fields available on stdin for each hook type.
- `skills/` — Claude Code skill markdown files. Each skill is a directory with a `SKILL.md`.

## Installation Model

`handler setup` extracts embedded skills and hooks to `~/.agent-handler/`, symlinks skills into `~/.claude/skills/`, and configures Claude Code hooks in settings.json. The binary goes to `/usr/local/bin` via `make install`. Data lives at `~/.agent-handler/`.

Skills and hooks are embedded into the binary at build time via `file-embed` in `src/Handler/Embedded.hs` (`embedDir` on `skills/`, `hooks/`, and `rules/`, filtered to the same patterns as the old go:embed globs), so new skills/hooks are picked up automatically as long as they follow the directory convention.

When adding or removing skills, update the `skillNames` list in `src/Handler/Cmd/Uninstall.hs` (install discovers skills from the embedded contents, but uninstall needs the list to know what to clean up).

When adding or removing hooks, update `configureHooks` in `src/Handler/Cmd/Setup.hs` and `removeHooks` in `src/Handler/Cmd/Uninstall.hs`.

**IMPORTANT: When adding, removing, or changing skills, commands, or capabilities, you MUST update `rules/agent-handler.md`.** This rules file is loaded automatically at every session start — it introduces agent-handler to the user, lists available skills and CLI commands, and contains the emit event reference. It must stay current.

## cmux Integration

cmux keyboard shortcut actions are defined in `src/Handler/CmuxConfig.hs` (`handlerCmuxActions`). `handler setup` installs them to `~/.config/cmux/cmux.json` and `handler uninstall` removes them.

When adding or removing cmux keyboard shortcut actions:
- Update `handlerCmuxActions` and `handlerCmuxActionIDs` in `src/Handler/CmuxConfig.hs`
- Update the statusline rendering in `src/Handler/Cmd/Statusline.hs` — `renderAwaitingLine` shows the awaiting shortcut in context, and `renderCmuxShortcutsLine` shows a summary at the bottom. Both read shortcuts dynamically from `getCmuxShortcuts`, but the display text is hardcoded and must be updated to describe new actions.
- Update the setup summary in `src/Handler/Cmd/Setup.hs` (the cmux actions section of the "will do" list)

## Watchers

External event watchers poll GitHub and Jira APIs for changes to subscribed resources. They run as one-shot commands scheduled via launchd (macOS) or cron (Linux).

- `src/Handler/Config.hs` — Config file read/write and token validation
- `src/Handler/Watcher/Framework.hs` + `Scheduler.hs` — Shared framework (active resources, cursors, dedup) and scheduler
- `src/Handler/Watcher/GitHub.hs` — GitHub PR watcher using GraphQL API
- `src/Handler/Watcher/Jira.hs` — Jira issue watcher using REST API

When adding new watcher types:
- Create a new module `src/Handler/Watcher/<Name>.hs`
- Add the service to `Config`/`Services` and `isServiceConfigured` in `src/Handler/Config.hs`
- Add the resource type mapping in `resourceTypeToService` in `src/Handler/Config.hs`
- Add the service to the auth prompts and the run dispatch in `src/Handler/Cmd/WatcherCli.hs`

## Handler Session

A session with `role = handler` acts as a command center. The `/handler` skill sets the role and starts a polling loop. The handler statusline shows global session counts and watcher status instead of per-session inbox.

Key commands: `handler triage` (what needs attention), `handler log --global` (cross-session timeline), `--to handler` in emit (role-based message routing).

The `role` column on the `sessions` table drives statusline behavior. `event_recipients` supports `recipient_type = 'role'` for role-based routing.

## .worktree-resources File

See [docs/worktree-resources.md](docs/worktree-resources.md) for the file format spec and integration guide.

## Design

Full design spec: `docs/superpowers/specs/2026-06-15-agent-handler-design.md`
Phase 1 implementation plan: `docs/superpowers/plans/2026-06-15-phase1-core-ledger.md`

## Web UI Development

The web UI lives in `ui/` (React + shadcn/ui + Tailwind v3). The API server is in `cmd/api/`.

**Dev server:** Run `make dev` to start both the API server and the Vite dev server via mprocs. The Vite dev server runs on **port 5173** — use this port when accessing the UI with Playwright or a browser. Do NOT start the dev server yourself; ask the user to run `make dev` if it isn't already running.

## Key Conventions

- Event IDs are UUIDs (not auto-increment)
- All timestamps are ISO 8601 UTC
- Subscriptions use soft deletes (deleted_at field)
- Sessions are archived, never destroyed
- The CLI supports `--json` on all commands for machine-readable output
- The `handler` binary name is used everywhere — do not rename it without updating hooks and skills
