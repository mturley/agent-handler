# Watcher data-migration runbook

This runbook covers `handler setup --migrate-watcher`, the one-time command
that copies handler's legacy events/subscriptions/resource data into the
`watcher` library's own tables (`watcher_*`) and flips the
`handler_meta.watcher_migrated` marker, switching the inbox onto the new
read path.

Run this **once**, by hand, against your real `~/.agent-handler/handler.db`.
It is not run automatically by `handler setup` (that flag must be passed
explicitly), and it refuses to run twice — a second invocation just prints
"watcher data already migrated (nothing to do)".

## Pre-flight

1. Stop both watchers so the database is idle during the copy:

   ```
   handler watcher stop github
   handler watcher stop jira
   ```

2. Confirm neither is still running:

   ```
   handler watching
   ```

   `handler setup --migrate-watcher` also refuses to run on its own if it
   detects a running github/jira watcher scheduler, but stopping them first
   avoids that error entirely.

## Build and install the new binary

From the repo root:

```
go build ./...
make install NONINTERACTIVE=1
```

(`make build && make install` also works if you want the web UI rebuilt
too; see the top-level `README.md` / `Makefile` for the full build target.
`NONINTERACTIVE=1` skips `make install`'s own `handler setup` prompt so you
control exactly when the migration runs, in the next step.)

## Run the migration

```
handler setup --migrate-watcher
```

This will, in order:

1. Refuse and exit if the migration has already run (checks the marker).
2. Refuse and exit if the github or jira watcher is still running (skip
   this by stopping them first, per Pre-flight above).
3. Back up the database file to `<dbpath>.backup-<UTC timestamp>` (e.g.
   `~/.agent-handler/handler.db.backup-20260810T170423Z`) and print the
   backup path. Aborts with an error if the backup can't be written —
   nothing is migrated in that case.
4. Print pre-migration row counts from the legacy tables.
5. Copy `events` (source in `github`/`jira` only), `event_resources`,
   `resource_state`, `resource_relationships`, `subscriptions`, and the
   legacy `watcher_status` table into their `watcher_*` counterparts,
   preserving original ids and timestamps. The original tables are left
   in place, untouched, for rollback.
6. Print a PASS/FAIL table comparing rows inserted per target table
   against the matching source count.
7. Best-effort copy GitHub/Jira credentials from handler's
   `~/.agent-handler/config.yaml` into the watcher library's
   `~/.config/watcher/auth.yaml`, for any service present in the handler
   config but not yet configured in `auth.yaml`. This step never fails the
   migration — if it can't complete, it prints a note to run
   `handler watcher auth` manually.
8. Set `handler_meta.watcher_migrated = 1`.

## Verify

```
handler health
handler watching
handler status
handler log --global
```

Unread counts should look the same as before the migration — the inbox now
reads resource-routed events from the `watcher_*` tables (the marker is
set), while agent-routed events continue to come from handler's own
tables. Spot-check a session or two that had unread PR/Jira notifications
before the migration.

## Resume the watchers

```
handler watcher start github
handler watcher start jira
```

## Rollback

If anything looks wrong after verifying:

1. Stop the watchers again:

   ```
   handler watcher stop github
   handler watcher stop jira
   ```

2. Restore the pre-migration database file over the current one. The
   default database path is `db.DefaultPath()`, typically
   `~/.agent-handler/handler.db`:

   ```
   cp ~/.agent-handler/handler.db.backup-<timestamp> ~/.agent-handler/handler.db
   ```

   The old (legacy) tables and query path are retained by design in this
   phase — the restored database still has everything the pre-migration
   binary expects, since the migration only ever *added* rows to the
   `watcher_*` tables and set one marker; it never deleted or rewrote the
   legacy tables.

3. Check out the prior handler commit (the one before this migration
   command shipped), rebuild, and reinstall:

   ```
   git checkout <pre-2b-commit>
   go build ./...
   make install NONINTERACTIVE=1
   ```

   That binary reads the legacy tables directly and doesn't know about the
   `watcher_migrated` marker, so it works unmodified against the restored
   database.

4. Resume the watchers:

   ```
   handler watcher start github
   handler watcher start jira
   ```

5. Once the root cause is understood and fixed, re-build the 2b binary and
   re-run `handler setup --migrate-watcher` from the top of this runbook —
   it's safe to retry against the restored (pre-migration) database, since
   the marker was never set on it.

## Local development against an unreleased watcher library

Handler pins a tagged `github.com/mturley/watcher` version (see `go.mod`).
When a handler change needs a matching, not-yet-released library change,
develop the two together with a temporary local `replace` — do NOT commit
it:

1. Point handler at your local watcher checkout:

   ```
   go mod edit -replace github.com/mturley/watcher=/Users/mturley/git/watcher
   ```

2. Make the coupled changes in both repos; build/test handler against the
   local library.

3. When the library change is ready, commit + tag a new library version
   (e.g. `v0.2.2`) and push it.

4. Re-pin handler to the released version and drop the replace:

   ```
   go get github.com/mturley/watcher@v0.2.2
   go mod edit -dropreplace github.com/mturley/watcher
   go mod tidy
   ```

Never commit the `replace` line — a committed replace makes the branch
unbuildable on any machine without that exact local path.
