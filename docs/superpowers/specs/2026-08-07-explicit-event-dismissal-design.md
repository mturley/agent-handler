# Explicit per-event dismissal

## Context

Sessions accumulate unread events. Currently the only way to clear unreads is
to advance the cursor ("Dismiss all" in the web UI, or `/inbox`/`/inbox-clear`
in the CLI), which marks *everything* up to a timestamp as seen. There is no way
to dismiss a single noisy event (e.g. a CI bundle) while leaving other unreads
intact.

This spec adds an explicit, per-session, per-event dismissal mechanism that
excludes chosen events from every "unread" surface — statusline, CLI `/inbox`,
web UI counts, resource unread dots — without advancing the cursor.

## Goal

- Dismiss a single event for a single session.
- Dismissed events are excluded from all unread queries everywhere.
- Dismissal is independent of the cursor: advancing the cursor is unaffected,
  and dismissing does not move the cursor.
- Bound storage: dismissal rows for events already behind the cursor are
  redundant and get pruned when the cursor advances past them.
- Enhance the web UI inbox modal: larger, expand/collapse chevrons,
  pretty-printed event types, and a per-event dismiss button with inline
  confirmation matching the existing "Dismiss all" pattern.

## Data model

New table:

```sql
CREATE TABLE IF NOT EXISTS dismissed_events (
    session_id   TEXT NOT NULL,
    event_id     TEXT NOT NULL,
    dismissed_at TEXT NOT NULL,
    PRIMARY KEY (session_id, event_id)
);

CREATE INDEX IF NOT EXISTS idx_dismissed_events_session ON dismissed_events(session_id);
```

Per-session dismissal composes cleanly with the existing recipient/subscription
joins and requires no change to the `events` table. The same event dismissed by
different sessions is tracked independently.

`schema.sql` gets the `CREATE TABLE IF NOT EXISTS` for fresh installs.
`runMigrations` in `db/db.go` also runs the same `CREATE TABLE IF NOT EXISTS`
as a no-op safety net for existing databases (this is the first use of the
migration hook now that we care about future public releases). The developer's
existing DB gets the `CREATE TABLE` run directly once.

## Query exclusion

Every unread query in `db/events.go` gains one clause:

```sql
AND NOT EXISTS (
    SELECT 1 FROM dismissed_events d
    WHERE d.session_id = ? AND d.event_id = e.id
)
```

Affected queries (all keyed on `session_id`, so the extra bind param is the
session ID already in scope):

- `UnreadForSession` — CLI `/inbox` listing, web UI inbox modal
- `UnreadCountForSession` — statusline count + breakdown, web UI badge/breakdown
- `UnreadResourcesForSession` — resource unread dots
- `HumanUnreadCountForSession` — `findSessionsWithUnreads` (alert card, statusline
  "other sessions with unreads")

`GlobalUnreadForSession` / `GlobalUnreadCountForSession` (handler global timeline)
and `DirectCountForSession` are intentionally **not** filtered — the global
timeline shows everything, and dismissal is a per-session inbox concept. (If we
later want direct counts to honor dismissal we can revisit; out of scope here.)

## Cursor-advance cleanup

When a cursor advances (`AdvanceBothCursors`, and any other cursor-advance path),
after moving the cursor delete dismissal rows for events now behind it:

```sql
DELETE FROM dismissed_events
WHERE session_id = ?
  AND event_id IN (
    SELECT id FROM events WHERE ts <= ?   -- new cursor value
  )
```

These rows are redundant (the event is already excluded by `ts > cursor`), so
pruning keeps `dismissed_events` bounded to "events dismissed while still ahead
of the cursor." Use the lower of the two cursors (last_seen vs human_seen) as the
safe pruning threshold so a row is only removed once it's behind *both*.

## DB methods

- `DismissEvent(sessionID, eventID string) error` — upsert a row
  (`INSERT ... ON CONFLICT DO NOTHING`), stamped with current UTC time.
- Cleanup logic added inside the cursor-advance methods (not a public method).

No `UndismissEvent` for now (YAGNI — the cursor-advance prune and "Dismiss all"
already clear things; re-surfacing a single dismissed event isn't a requested
need).

## Backend endpoint

`POST /api/inbox/dismiss-event` with body `{ "session_id": "...", "event_id": "..." }`.
Handler opens a writable DB (server DB is read-only), calls `DismissEvent`,
returns `{ "success": true }`. Mirrors `handleDismissInbox` structure.

## CLI parity

New hidden-ish subcommand `handler dismiss-event --session-id <id> --event <id>`
so the capability exists outside the web UI and for scripting. Follows the
existing cobra command pattern; resolves session ID via `resolveSessionID` when
`--session-id` is omitted.

## Frontend: InboxDialog enhancements

File: `ui/src/components/InboxDialog.tsx`

- **Size**: widen to `max-w-4xl`; increase `ScrollArea` max height (~`600px`).
- **Chevron affordance**: each event row shows a `ChevronRight` (collapsed) /
  `ChevronDown` (expanded) icon so it's clear the row expands. Rows with no body
  still show the row but the chevron is omitted or disabled.
- **Pretty event types**: replace raw `ev.type` in the badge with
  `formatEventType(ev.type)` (already used elsewhere).
- **Per-event dismiss**: each row gets a small trash/dismiss button. Clicking it
  swaps that row's trailing controls into an inline confirm ("Dismiss? Confirm /
  Cancel"), matching the "Dismiss all" pattern. Confirm calls
  `dismissEvent(sessionId, ev.id)` → invalidates `queryKeys.inbox(sessionId)` and
  `["sessions"]`. Track per-row confirm state by event id (a `Set<string>` or
  `string | null`), independent of the expand state.

API client: add `dismissEvent(sessionId, eventId)` in `ui/src/api/client.ts`
posting to the new endpoint.

## Files to modify

- `db/schema.sql` — add `dismissed_events` table + index
- `db/db.go` — `runMigrations` creates the table (no-op safety net)
- `db/events.go` — add exclusion clause to the four unread queries; add
  `DismissEvent`
- `db/cursors.go` (or wherever `AdvanceBothCursors` lives) — prune dismissed rows
  behind the cursor
- `cmd/api/actions.go` — `handleDismissEvent`
- `cmd/api/server.go` — route registration
- `cmd/dismiss_event.go` — new CLI subcommand
- `ui/src/api/client.ts` — `dismissEvent`
- `ui/src/components/InboxDialog.tsx` — size, chevrons, pretty types, per-event
  dismiss with inline confirm

## Verification

- Dismiss a single event in the web UI modal → it disappears from the modal, the
  session's unread count drops by one, the resource dot clears if it was the only
  unread for that resource, and the statusline count for that session drops.
- `/inbox` in the CLI for that session no longer lists the dismissed event.
- "Dismiss all" still advances the cursor and clears the modal.
- After dismissing an event then advancing the cursor past it, the
  `dismissed_events` row is pruned (verify with a direct query).
- Handler global timeline still shows the dismissed event (not filtered there).
- Fresh install (schema.sql) and existing DB (migration) both have the table.
