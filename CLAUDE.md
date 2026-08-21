# agent-handler

## Build and Install

```bash
make build      # builds web UI + bin/handler
make install    # builds first, then atomically installs binary and runs setup
make clean      # removes bin/
```

`make install` runs `make build` first, then uses atomic rename so it's safe to run while handler is actively running. Use `NONINTERACTIVE=1 make install` to skip confirmation prompts and watcher setup. Always use `NONINTERACTIVE=1` when installing from a Claude session.

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
- `worktreeinterop/` — CLI-level interop with the `worktree` binary: shells out to
  `worktree resources list --json` (auto-watch a worktree's primary resources at
  session registration) and `worktree resources add` (propagate `/watch`). Gated on
  `exec.LookPath("worktree")`, best-effort — a no-op when worktree isn't installed.
  (Replaced the old `.worktree-resources` file reader in Phase 5; there is no
  `worktree/` package and no `.worktree-resources` file anymore.)
- `hooks/` — Shell scripts for Claude Code hooks (SessionStart, UserPromptSubmit, PreCompact). See `docs/claude-hook-stdin.md` for the JSON fields available on stdin for each hook type.
- `skills/` — Claude Code skill markdown files. Each skill is a directory with a `SKILL.md`.

## Installation Model

`handler setup` extracts embedded skills and hooks to `~/.agent-handler/`, symlinks skills into `~/.claude/skills/`, and configures Claude Code hooks in settings.json. The binary goes to `$GOBIN` (via `go install` or `make install`). Data lives at `~/.agent-handler/`.

Skills and hooks are embedded into the binary at build time via `//go:embed` in `embedded.go`. The embed directives use glob patterns (`skills/*/SKILL.md`, `hooks/*.sh`), so new skills/hooks are picked up automatically as long as they follow the directory convention.

When adding or removing skills, update the `skillNames` slice in `cmd/uninstall.go` (install discovers skills from the embedded FS, but uninstall needs the list to know what to clean up).

When adding or removing hooks, update `configureHooks()` in `cmd/setup.go` and `removeHooks()` in `cmd/uninstall.go`.

**IMPORTANT: When adding, removing, or changing skills, commands, or capabilities, you MUST update `rules/agent-handler.md`.** This rules file is loaded automatically at every session start — it introduces agent-handler to the user, lists available skills and CLI commands, and contains the emit event reference. It must stay current.

## cmux Integration

cmux keyboard shortcut actions are defined in `cmd/cmux_config.go` (`handlerCmuxActions` map). `handler setup` installs them to `~/.config/cmux/cmux.json` and `handler uninstall` removes them.

When adding or removing cmux keyboard shortcut actions:
- Update `handlerCmuxActions` and `handlerCmuxActionIDs` in `cmd/cmux_config.go`
- Update the statusline rendering in `cmd/statusline.go` — `renderAwaitingLine()` shows the awaiting shortcut in context, and `renderCmuxShortcutsLine()` shows a summary at the bottom. Both read shortcuts dynamically from `GetCmuxShortcuts()`, but the display text is hardcoded and must be updated to describe new actions.
- Update the setup summary in `cmd/setup.go` (the cmux actions section of the "will do" list)

## Watchers (watcher library)

External event watchers poll GitHub, Jira, and **Slack** APIs for changes to
subscribed resources, emitting events into the watcher DB. They run as one-shot
commands scheduled via launchd (macOS) or cron (Linux). Slack became a
first-class watcher in Phase 6 (thread replies → session inboxes; 5m poll
interval). The canonical service list is `watcher.KnownWatchers`
(`{"github","jira","slack"}`) — iterate it rather than hardcoding the set, so a
new watcher type appears across `install`/`run`/`status`/`watching`/`triage` at
once.

**The polling engine, schema, DB layer, and per-source pollers live in a
separate library — `github.com/mturley/watcher` — NOT in this repo.** Handler
is a *consumer* of that library. As of Phase 2b, the old in-tree
`watcher/github/` and `watcher/jira/` packages no longer exist here; handler
pins a released version of the library (see `go.mod`) and calls into it.

### IMPORTANT: cross-repo coordination

**The watcher library is maintained locally at `~/git/watcher`** (module
`github.com/mturley/watcher`, GitHub `mturley/watcher`). Any handler change
that requires new or changed poller behavior, schema, event types, dedup
logic, or DB APIs is **cross-repo work**:

1. Make the change in `~/git/watcher`, with tests (`go test ./...` there).
2. Commit and cut a new library release tag (e.g. `v0.2.5`) and push it.
3. In this repo, re-pin the new version: `go get github.com/mturley/watcher@vX.Y.Z`
   (or edit `go.mod` + `go mod tidy`), then rebuild/re-test.
4. Rebuild and reinstall handler (`NONINTERACTIVE=1 make install`).

Do NOT attempt to work around a library limitation with a local patch in this
repo — fix it in the library and re-pin. The library is also consumed by
`worktree` (`~/git/worktree`, now an active consumer — its web UI + resource
tracking are built on the same library), so its behavior must stay correct for
multiple consumers. A library change made for one consumer must not break the
other; after releasing a new tag, re-pin in BOTH repos as needed.

Poller/source bugs (missing event types, dedup false-positives, GraphQL query
gaps) are library bugs — diagnose and fix them in `~/git/watcher/github/` or
`~/git/watcher/jira/`.

### Local handler-side watcher glue

- `config/` — Config file read/write and token validation
- `watcher/scheduler.go` — thin glue that resolves active resources and invokes
  the library's pollers (the shared framework concepts now live in the library;
  this directory only wires handler's config/DB into it)

## Handler Session

A session with `role = handler` acts as a command center. The `/handler` skill sets the role and starts a polling loop. The handler statusline shows global session counts and watcher status instead of per-session inbox.

Key commands: `handler triage` (what needs attention), `handler log --global` (cross-session timeline), `--to handler` in emit (role-based message routing).

The `role` column on the `sessions` table drives statusline behavior. `event_recipients` supports `recipient_type = 'role'` for role-based routing.

## worktree integration (CLI-level, Phase 5)

Handler integrates with the `worktree` CLI at the **CLI level only** — it shells
out to the `worktree` binary via the `worktreeinterop/` package (auto-watch a
worktree's primary resources at session registration; propagate `/watch` via
`worktree resources add`; `/unwatch` stays handler-only). Best-effort and gated on
`worktree` being installed. This replaced the old `.worktree-resources` FILE
mechanism — that file, its reader package, and `docs/worktree-resources.md` are
obsolete (the doc is retained only as historical context; do not treat it as
current).

> **IMPORTANT — separate DB, shared SCHEMA only.** Handler has its OWN watcher
> database (`~/.agent-handler/data/handler.db`). The `worktree` tool, the library's
> other consumer, has a DIFFERENT database file
> (`${XDG_DATA_HOME:-~/.local/share}/worktree/worktree.db`). They share only the
> watcher library's *schema* and code — never rows. Handler's subscriptions
> (subscriber `handler:<session>`) and worktree's (subscriber `worktree:<path>`)
> live in physically separate SQLite files; there is no shared table, no cross-tool
> row, no joint query. Do not reason about "worktree's subscriptions" as if they
> were in handler's DB — they are not. The only handler↔worktree coupling is the
> CLI interop above (plus the shared library schema). A change to handler's DB
> cannot affect worktree's data directly, and vice versa.

## Design

Full design spec: `docs/superpowers/specs/2026-06-15-agent-handler-design.md`
Phase 1 implementation plan: `docs/superpowers/plans/2026-06-15-phase1-core-ledger.md`

## Web UI Development

The web UI lives in `ui/` (React + shadcn/ui + Tailwind v3). The API server is in `cmd/api/`.

**Dev server:** Run `make dev` to start both the Go API server and the Vite dev server via mprocs. The Vite dev server runs on **port 5173** — use this port when accessing the UI with Playwright or a browser. Do NOT start the dev server yourself; ask the user to run `make dev` if it isn't already running.

## Key Conventions

- Event IDs are UUIDs (not auto-increment)
- All timestamps are ISO 8601 UTC
- Subscriptions use soft deletes (deleted_at field)
- Sessions are archived, never destroyed
- The CLI supports `--json` on all commands for machine-readable output
- The `handler` binary name is used everywhere — do not rename it without updating hooks and skills
