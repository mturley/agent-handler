package api

import (
	"net/http"
	"time"

	"github.com/mturley/agent-handler/cronsched"
	"github.com/mturley/agent-handler/db"
)

// sessionCronInfo is a tracked cron job enriched with its next fire time.
type sessionCronInfo struct {
	db.SessionCron
	// NextFireAt is RFC3339, or empty when the schedule cannot be parsed.
	NextFireAt string `json:"next_fire_at"`
}

// nextFireAt returns the next time the expression fires after `from`, as
// RFC3339 in `from`'s location. An unparseable expression yields "" rather than
// an error — a job with a bad schedule should still be listed.
func nextFireAt(expr string, from time.Time) string {
	next, ok := cronsched.Next(expr, from)
	if !ok {
		return ""
	}
	return next.Format(time.RFC3339)
}

// handleSessionCrons returns the Claude Code cron jobs tracked for a session.
//
// These are recorded by the PostToolUse hook and reconciled every turn against
// the Stop hook's session_crons snapshot, so the list reflects what the session
// currently has scheduled — jobs that fired and auto-deleted are already gone.
//
// Cron expressions are interpreted in local time, matching how Claude Code
// schedules them.
func (s *Server) handleSessionCrons(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session id is required")
		return
	}

	crons, err := s.DB.ListSessionCrons(sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list cron jobs")
		return
	}

	now := time.Now()
	out := make([]sessionCronInfo, 0, len(crons))
	for _, c := range crons {
		out = append(out, sessionCronInfo{
			SessionCron: c,
			NextFireAt:  nextFireAt(c.Schedule, now),
		})
	}

	writeJSON(w, http.StatusOK, out)
}

// cronsFingerprint summarises every tracked cron job across all sessions. The
// SSE stream compares it between ticks and emits crons_changed only when it
// differs, so a page sitting on the Cron jobs tab does not refetch on every
// heartbeat.
//
// It covers the mutable fields, not just the job ids: reconciliation can
// rewrite a schedule in place without changing the job count.
func cronsFingerprint(d *db.DB) string {
	var fp string
	d.QueryRow(`
		SELECT COALESCE(GROUP_CONCAT(session_id || ':' || job_id || ':' || schedule || ':' || recurring, ','), '')
		FROM (SELECT * FROM session_crons ORDER BY session_id, job_id)
	`).Scan(&fp)
	return fp
}

// resourcesFingerprint summarises the subscription set across all sessions, so
// the stream can emit resources_changed when a resource is watched or unwatched
// — including by a watcher or another session, which the UI would otherwise not
// see until remount.
//
// It covers deleted_at because unsubscribing is a soft delete: the row stays,
// only the timestamp appears.
//
// Unread COUNTS are not covered here; those move when events arrive, which the
// existing events_new signal already reports.
func resourcesFingerprint(d *db.DB) string {
	var fp string
	d.QueryRow(`
		SELECT COALESCE(GROUP_CONCAT(id || ':' || subscriber || ':' || resource_type || ':' ||
		                             resource_id || ':' || COALESCE(deleted_at, ''), ','), '')
		FROM (SELECT * FROM watcher_subscriptions ORDER BY id)
	`).Scan(&fp)
	return fp
}
