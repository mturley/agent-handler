package db

// This file is the single source of truth for "what events are in a session's
// inbox." Every unread query (list, count, breakdown, resources, direct count,
// auto-delivered, reminder lines) composes the fragments defined here rather
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
// Callers supply their own SELECT / GROUP BY / ORDER BY around these fragments.

// inboxJoinSQL is the FROM+JOIN clause shared by every routed-inbox query.
// It references alias `e` for events and joins recipients, resources, and the
// session's live subscriptions. Its single bound parameter is the session ID
// (for the subscription join). Use inboxScopeArgs to build the full arg slice.
const inboxJoinSQL = `
	FROM events e
	LEFT JOIN event_recipients er ON e.id = er.event_id
	LEFT JOIN event_resources eres ON e.id = eres.event_id
	LEFT JOIN subscriptions s ON s.resource_type = eres.resource_type AND s.resource_id = eres.resource_id AND s.session_id = ? AND s.deleted_at IS NULL`

// inboxWhereSQL is the WHERE body shared by every routed-inbox query (without
// the leading "WHERE"). Its bound parameters, in order, are:
//   cursor, dismissSessionID, recipientSessionID, branch, repoBranch, role
// Combined with the join's leading sessionID param, the full ordered arg list
// is produced by inboxScopeArgs.
const inboxWhereSQL = `e.ts > ?
	` + inboxExcludedTypesSQL + `
	` + dismissedExclusionSQL + `
	AND (
	    e.broadcast = 1
	    OR (er.recipient_type = 'session' AND er.recipient_value = ?)
	    OR (er.recipient_type = 'branch' AND (er.recipient_value = ? OR er.recipient_value = ?))
	    OR (er.recipient_type = 'role' AND er.recipient_value = ?)
	    OR s.id IS NOT NULL
	)`

// inboxScopeArgs returns the argument slice for a query built from
// inboxJoinSQL + " WHERE " + inboxWhereSQL, in the exact order the two
// fragments' placeholders appear:
//   subscription-join sessionID, cursor, dismissal sessionID,
//   recipient sessionID, branch, repoBranch, role
func inboxScopeArgs(session *Session, cursor string) []interface{} {
	repoBranch := session.Repo + ":" + session.Branch
	return []interface{}{
		session.SessionID, // subscription join
		cursor,            // e.ts > ?
		session.SessionID, // dismissal exclusion
		session.SessionID, // recipient_type = 'session'
		session.Branch,    // recipient_type = 'branch' (branch)
		repoBranch,        // recipient_type = 'branch' (repo:branch)
		session.Role,      // recipient_type = 'role'
	}
}
