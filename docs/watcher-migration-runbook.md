# Watcher data-migration runbook

This runbook covers `handler setup --migrate-watcher`, the one-time command
that copies handler's legacy events/subscriptions/resource data into the
`watcher` library's own tables (`watcher_*`), **drops the legacy tables**,
and **purges** the now-duplicated github/jira rows from the legacy `events`
table — switching handler onto the `watcher_*` tables as its sole source of
truth for subscriptions, resource state, resource relationships, and
resource-routed events.

This is a **one-way structural cleanup**. It is not run automatically by
`handler setup` (that flag must be passed explicitly). Detection of "needs
migrating" is purely schema-based: if none of the four legacy tables
(`subscriptions`, `resource_state`, `resource_relationships`,
`watcher_status`) exist, a re-run just prints "watcher data already
migrated (nothing to do)" and does nothing further.

## If you already ran the Phase 2b migration

Phase 2b's version of this command copied data into `watcher_*` but
**retained** the legacy tables. If you ran it, your database still has the
legacy tables even though your data already lives in `watcher_*`. Running
`handler setup --migrate-watcher` again on this (2c) binary detects that
state — legacy tables present, but the data already copied — and **skips
the copy** (no double-insert), then finishes the cleanup: it drops the
legacy tables and purges the leftover github/jira duplicate rows from
`events`. You only need to run it once more; it prints
"Detected a prior (Phase 2b) migration" instead of the full pre-migration
row-count report when this path is taken.

Until you run it, commands are blocked with a "Legacy database found" error
directing you here.

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

1. Refuse and exit if the legacy tables are already gone (nothing to do).
2. Refuse and exit if the github or jira watcher is still running (skip
   this by stopping them first, per Pre-flight above).
3. Back up the database file to `<dbpath>.backup-<UTC timestamp>` (e.g.
   `~/.agent-handler/handler.db.backup-20260810T170423Z`) and print the
   backup path. Aborts with an error if the backup can't be written —
   nothing is migrated or dropped in that case. **This backup is the only
   rollback path** — see Rollback below.
4. **Full migration** (legacy tables present, no prior Phase 2b run
   detected): print pre-migration row counts from the legacy tables, then
   copy `events` (source in `github`/`jira` only), `event_resources`,
   `resource_state`, `resource_relationships`, `subscriptions`, and the
   legacy `watcher_status` table into their `watcher_*` counterparts,
   preserving original ids and timestamps, and print a PASS/FAIL table
   comparing rows inserted per target table against the matching source
   count. **Finish path** (a prior Phase 2b run already copied the data):
   skip the copy entirely and go straight to the next step.
5. **Drop the four legacy tables** (`subscriptions`, `resource_state`,
   `resource_relationships`, `watcher_status`) in the same transaction as
   the copy (or immediately, on the finish path). They no longer exist in
   the database after this step.
6. **Purge** the github/jira-sourced rows (and their `event_resources`
   rows) from the legacy `events` table — that data now lives in
   `watcher_events`/`watcher_event_resources`. Agent/handler-sourced events
   are untouched. Prints "Purged N migrated github/jira rows from the
   legacy events table." This step is best-effort: if it fails, the
   migration prints a warning and continues rather than aborting — the
   leftover rows are harmless duplicates, and the copy (and any table
   drops) already succeeded.
7. Best-effort copy GitHub/Jira credentials from handler's
   `~/.agent-handler/config.yaml` into the watcher library's
   `~/.config/watcher/auth.yaml`, for any service present in the handler
   config but not yet configured in `auth.yaml`. This step never fails the
   migration — if it can't complete, it prints a note to run
   `handler watcher auth` manually.
8. Best-effort seed Jira behavior settings (custom fields, bot usernames)
   into the watcher library's `config.yaml`.

## Verify

```
handler health
handler watching
handler status
handler log --global
```

Unread counts should look the same as before the migration — the inbox
reads resource-routed events from the `watcher_*` tables, while
agent-routed events continue to come from handler's own `events` table.
Spot-check a session or two that had unread PR/Jira notifications before
the migration.

## Resume the watchers

```
handler watcher start github
handler watcher start jira
```

## Rollback

Because this migration drops the legacy tables and purges migrated `events`
rows, **the pre-migration backup file is the only way back** — there is no
"tables are still there, just downgrade the binary" fallback anymore. If
anything looks wrong after verifying:

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

   Restoring the backup brings back the legacy tables and the pre-purge
   `events` rows together, exactly as they were before the migration ran.

3. Check out the prior handler commit (the one before this migration
   command shipped its 2c behavior), rebuild, and reinstall — the restored
   database only works correctly with a binary that still knows how to read
   the legacy tables directly:

   ```
   git checkout <pre-2c-commit>
   go build ./...
   make install NONINTERACTIVE=1
   ```

4. Resume the watchers:

   ```
   handler watcher start github
   handler watcher start jira
   ```

5. Once the root cause is understood and fixed, re-build the 2c binary and
   re-run `handler setup --migrate-watcher` from the top of this runbook —
   it's safe to retry against the restored (pre-migration) database, since
   the legacy tables are back in place and the schema-based detection will
   run the full migration path again.

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
   (e.g. `v0.2.3`) and push it.

4. Re-pin handler to the released version and drop the replace:

   ```
   go get github.com/mturley/watcher@v0.2.3
   go mod edit -dropreplace github.com/mturley/watcher
   go mod tidy
   ```

Never commit the `replace` line — a committed replace makes the branch
unbuildable on any machine without that exact local path.
