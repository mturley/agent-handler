package api

import (
	"net/http"

	"github.com/mturley/agent-handler/db"
)

// handleSessionCrons returns the Claude Code cron jobs tracked for a session.
//
// These are recorded by the PostToolUse hook and reconciled every turn against
// the Stop hook's session_crons snapshot, so the list reflects what the session
// currently has scheduled — jobs that fired and auto-deleted are already gone.
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
	if crons == nil {
		crons = []db.SessionCron{}
	}

	writeJSON(w, http.StatusOK, crons)
}
