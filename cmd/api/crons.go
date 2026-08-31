package api

import (
	"net/http"
	"time"

	"github.com/mturley/agent-handler/db"
	"github.com/robfig/cron/v3"
)

// sessionCronInfo is a tracked cron job enriched with its next fire time.
type sessionCronInfo struct {
	db.SessionCron
	// NextFireAt is RFC3339, or empty when the schedule cannot be parsed.
	NextFireAt string `json:"next_fire_at"`
}

// cronParser matches Claude Code's format: standard 5-field cron, no seconds
// field and no descriptors.
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// nextFireAt returns the next time the expression fires after `from`, as
// RFC3339 in `from`'s location. An unparseable expression yields "" rather than
// an error — a job with a bad schedule should still be listed.
func nextFireAt(expr string, from time.Time) string {
	if expr == "" {
		return ""
	}
	sched, err := cronParser.Parse(expr)
	if err != nil {
		return ""
	}
	next := sched.Next(from)
	if next.IsZero() {
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
