package db

import (
	"time"
)

// This file is the single source of truth for "what events are in a session's
// inbox." Every unread query (list, count, breakdown, resources, direct count,
// auto-delivered, reminder lines) composes the builder defined here rather
// than repeating the join/where logic. Change the definition of "unread" here
// and every caller follows.
//
// The canonical predicate: an event is in session S's inbox when
//   - its timestamp is after S's cursor, AND
//   - it is not a bookkeeping type (watch_started/watcher_error), AND
//   - S has not explicitly dismissed it, AND
//   - it is routed to S — broadcast, addressed to S's session/branch/role, or
//     references a resource S subscribes to.
//
// Resource-routed events live in the watcher_* tables and agent-routed events
// live in handler's own tables. The inbox is the UNION of two arms:
//   - AGENT arm: handler's `events`, routed via event_recipients/broadcast/
//     branch/role. (No subscription/event_resources join — resource routing
//     moved to the watcher arm.)
//   - WATCHER arm: `watcher_events` joined to `watcher_subscriptions` through
//     `watcher_event_resources`, scoped to this session's subscriber.
// Both arms apply the cursor, excluded-types, and dismissal filters. A
// dismissed watcher event lives in watcher_events, so the dismissal exclusion
// must run on the watcher arm too, not only the agent arm.
//
// Callers supply their own SELECT / GROUP BY / ORDER BY around whichever form
// inboxSelect emits.

// --- watcher-migration marker -------------------------------------------

// watcherMigrationDone reports whether a prior handler-owned data migration
// left its marker — i.e. whether handler_meta has the row watcher_migrated=1.
// It defaults to false when the table or row is absent, or the value is
// anything but "1".
//
// The marker is RETIRED as a detection signal (legacy detection is now
// schema-based; see db/legacy_guard.go, and the inbox UNION is unconditional).
// It survives here for one purpose only: `handler setup --migrate-watcher`
// reads it to distinguish a 2b-migrated database (marker set, legacy tables
// retained) — which only needs the legacy tables dropped — from a truly
// unmigrated one that needs a full copy. Nothing writes the marker anymore, so
// there is no setter; on fresh 2c installs handler_meta is never created and
// this simply returns false.
func (db *DB) watcherMigrationDone() bool {
	var value string
	err := db.conn.QueryRow(
		`SELECT value FROM handler_meta WHERE key = 'watcher_migrated'`,
	).Scan(&value)
	if err != nil {
		return false
	}
	return value == "1"
}

// WatcherMigrationDone is the exported form of watcherMigrationDone, for use
// by package cmd (specifically the `handler setup --migrate-watcher` data
// migration in cmd/migrate_watcher.go), which cannot call the unexported
// method directly.
func (db *DB) WatcherMigrationDone() bool { return db.watcherMigrationDone() }

// --- UNION path ----------------------------------------------------------

// agentArmSQL is the agent arm of the inbox UNION: handler's own `events`
// routed via recipients/broadcast/branch/role. Resource routing is dropped
// here (it lives in the watcher arm). The projected columns are the caller's
// `cols`, expressed against alias `e`. Placeholders, in order:
//   cursor, dismissSessionID, recipientSessionID, branch, repoBranch, role
const agentArmFrom = `
		FROM events e
		LEFT JOIN event_recipients er ON e.id = er.event_id
		WHERE e.ts > ?
		` + inboxExcludedTypesSQL + `
		` + dismissedExclusionSQL + `
		AND (
		    e.broadcast = 1
		    OR (er.recipient_type = 'session' AND er.recipient_value = ?)
		    OR (er.recipient_type = 'branch' AND (er.recipient_value = ? OR er.recipient_value = ?))
		    OR (er.recipient_type = 'role' AND er.recipient_value = ?)
		)`

// watcherArmFrom is the watcher arm of the inbox UNION: subscription-routed
// `watcher_events`. Placeholders, in order:
//   subscriber, now (lease expiry), cursor, dismissSessionID
const watcherArmFrom = `
		FROM watcher_events e
		JOIN watcher_event_resources eres ON eres.event_id = e.id
		JOIN watcher_subscriptions s ON s.resource_type = eres.resource_type AND s.resource_id = eres.resource_id
		WHERE s.subscriber = ? AND s.deleted_at IS NULL AND (s.expires_at IS NULL OR s.expires_at > ?)
		  AND e.ts > ?
		  ` + inboxExcludedTypesSQL + `
		  ` + dismissedExclusionSQL

// --- UNION builder --------------------------------------------------------

// inboxCols describes the column projections for an inbox query. The two arms
// of the UNION read from different tables (handler `events` vs
// `watcher_events`), so their per-arm SELECT lists may differ (e.g.
// watcher_events has no session_id/broadcast columns — the watcher arm must
// project literals). Both arms MUST expose the same output column NAMES so the
// outer query can order/aggregate uniformly.
//
//   - agent:  the SELECT list for the agent arm (references e/er).
//   - watcher: the SELECT list for the watcher arm (references e/eres/s).
//   - outer:  the SELECT list the outer query projects from the UNION subquery
//             (references the subquery alias `e`). Defaults to a passthrough
//             when the outer projection is identical to the arms' output.
type inboxCols struct {
	agent   string
	watcher string
	outer   string
}

// inboxSelect returns the query that projects a session's inbox, starting at
// the caller's `selectKw` (e.g. "SELECT" or "SELECT DISTINCT"). It emits the
// UNION of the agent and watcher arms wrapped in an outer SELECT. Callers
// append their own GROUP BY / ORDER BY / extra predicates after the returned
// fragment.
//
// The outer SELECT reuses selectKw so DISTINCT still applies across the UNION
// (the agent arm's LEFT JOIN on recipients can duplicate an event row); the
// inner arms use a plain SELECT under UNION ALL.
func inboxSelect(selectKw string, cols inboxCols) string {
	return inboxSelectPred(selectKw, cols, "")
}

// inboxSelectPred is inboxSelect with an extra scalar predicate (without a
// leading keyword, e.g. "e.type = ?"). It becomes the outer query's
// "WHERE <pred>". The predicate may reference only columns exposed by the
// projection (`cols`).
func inboxSelectPred(selectKw string, cols inboxCols, extraPred string) string {
	outer := cols.outer
	if outer == "" {
		outer = cols.agent
	}
	q := selectKw + ` ` + outer + ` FROM (
		SELECT ` + cols.agent + agentArmFrom + `
		UNION ALL
		SELECT ` + cols.watcher + watcherArmFrom + `
	) e`
	if extraPred != "" {
		q += "\n\t\tWHERE " + extraPred
	}
	return q
}

// inboxEventCols projects a full Event row (the columns scanEvents expects).
// watcher_events lacks session_id and broadcast, so the watcher arm projects
// literals for those; the output column names match the agent arm.
var inboxEventCols = inboxCols{
	agent:   "e.id, e.ts, e.external_ts, e.source, e.session_id, e.type, e.title, e.body, e.author, e.author_type, e.broadcast, e.tags",
	watcher: "e.id, e.ts, e.external_ts, e.source, NULL AS session_id, e.type, e.title, e.body, e.author, e.author_type, 0 AS broadcast, e.tags",
	outer:   "e.id, e.ts, e.external_ts, e.source, e.session_id, e.type, e.title, e.body, e.author, e.author_type, e.broadcast, e.tags",
}

// inboxTypeCountCols powers the "count by type" breakdown. The arms project
// (type, id); the outer query aggregates COUNT(DISTINCT id) grouped by type.
var inboxTypeCountCols = inboxCols{
	agent:   "e.type, e.id",
	watcher: "e.type, e.id",
	outer:   "e.type, COUNT(DISTINCT e.id) as count",
}

// inboxCountCols powers a scalar COUNT(DISTINCT id). The arms project (id, ts)
// — ts is carried so callers can add an outer time-bound predicate (e.g.
// AutoDeliveredCount's "e.ts <= ?") — while the outer query counts DISTINCT id.
var inboxCountCols = inboxCols{
	agent:   "e.id, e.ts",
	watcher: "e.id, e.ts",
	outer:   "COUNT(DISTINCT e.id)",
}

// inboxResourcesSelect builds the query for UnreadResourcesForSession: the set
// of resource (type,id) pairs referenced by unread events. It cannot reuse the
// standard arms because the agent arm drops the event_resources join — here the
// agent arm re-adds it (agent events may still *reference* a resource even
// though resource ROUTING moved to the watcher arm), and the watcher arm reads
// its already-joined watcher_event_resources. Both arms filter out rows with a
// NULL resource_type. Placeholders match inboxArgs.
func inboxResourcesSelect() string {
	// Agent arm with the event_resources join re-added.
	agentResArm := `
		FROM events e
		LEFT JOIN event_recipients er ON e.id = er.event_id
		LEFT JOIN event_resources eres ON e.id = eres.event_id
		WHERE e.ts > ?
		` + inboxExcludedTypesSQL + `
		` + dismissedExclusionSQL + `
		AND eres.resource_type IS NOT NULL
		AND (
		    e.broadcast = 1
		    OR (er.recipient_type = 'session' AND er.recipient_value = ?)
		    OR (er.recipient_type = 'branch' AND (er.recipient_value = ? OR er.recipient_value = ?))
		    OR (er.recipient_type = 'role' AND er.recipient_value = ?)
		)`
	return `SELECT DISTINCT e.resource_type, e.resource_id FROM (
		SELECT eres.resource_type AS resource_type, eres.resource_id AS resource_id` + agentResArm + `
		UNION ALL
		SELECT eres.resource_type AS resource_type, eres.resource_id AS resource_id` + watcherArmFrom + `
	) e`
}

// inboxArgs returns the argument slice matching the query inboxSelect
// produces. Placeholders, in order (agent arm then watcher arm):
//   cursor, dismissSessionID, recipient sessionID, branch, repoBranch, role,
//   subscriber, now, cursor, dismissSessionID
func inboxArgs(session *Session, cursor string) []interface{} {
	repoBranch := session.Repo + ":" + session.Branch
	now := time.Now().UTC().Format(time.RFC3339)
	return []interface{}{
		// agent arm
		cursor,            // e.ts > ?
		session.SessionID, // dismissal exclusion
		session.SessionID, // recipient_type = 'session'
		session.Branch,    // recipient_type = 'branch' (branch)
		repoBranch,        // recipient_type = 'branch' (repo:branch)
		session.Role,      // recipient_type = 'role'
		// watcher arm
		handlerSubscriber(session.SessionID), // s.subscriber = ?
		now,                                   // s.expires_at > ?
		cursor,                                // e.ts > ?
		session.SessionID,                     // dismissal exclusion
	}
}
