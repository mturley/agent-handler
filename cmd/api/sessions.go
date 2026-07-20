package api

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"time"

	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
)

type enrichedSession struct {
	SessionID          string         `json:"session_id"`
	SessionName        string         `json:"session_name"`
	Branch             string         `json:"branch"`
	Repo               string         `json:"repo"`
	DisplayState       string         `json:"display_state"`
	InboxMode          string         `json:"inbox_mode"`
	Peekable           bool           `json:"peekable"`
	TerminalType       string         `json:"terminal_type,omitempty"`
	UnreadCount        int            `json:"unread_count"`
	UnreadBreakdown    map[string]int `json:"unread_breakdown,omitempty"`
	LastActive         string         `json:"last_active"`
	LastPrompt         string         `json:"last_prompt,omitempty"`
	CmuxWorkspace      string         `json:"cmux_workspace,omitempty"`
	CmuxWorkspaceColor string         `json:"cmux_workspace_color,omitempty"`
	NeedsInput         bool           `json:"needs_input"`
	PID                int            `json:"pid"`
	Status             string         `json:"status"`
	SubscriptionCount  int            `json:"subscriptions_count"`
	CmuxOrder          int            `json:"cmux_order"`
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.DB.ListSessions(false, 1000, 0)
	if err != nil {
		s.Logger.Printf("Error listing sessions: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to list sessions")
		return
	}

	// Build cmux ordering map (surface ID → ordinal)
	cmuxOrder := buildCmuxOrderMap()

	enriched := make([]enrichedSession, len(sessions))
	for i, session := range sessions {
		enriched[i] = s.enrichSession(session)
		if order, ok := cmuxOrder[session.TerminalID]; ok {
			enriched[i].CmuxOrder = order
		} else {
			enriched[i].CmuxOrder = 999999
		}
	}

	writeJSON(w, http.StatusOK, enriched)
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	session, err := s.DB.GetSession(sessionID)
	if err != nil {
		s.Logger.Printf("Error getting session %s: %v", sessionID, err)
		writeError(w, http.StatusNotFound, "Session not found")
		return
	}

	enriched := s.enrichSession(*session)
	writeJSON(w, http.StatusOK, enriched)
}

func (s *Server) handleSessionPeek(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	peekState, err := s.DB.GetPeekState(sessionID)
	if err != nil {
		s.Logger.Printf("Error getting peek state for %s: %v", sessionID, err)
		writeError(w, http.StatusInternalServerError, "Failed to get peek state")
		return
	}

	if peekState == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"content":      "",
			"needs_input":  false,
			"reason":       "",
			"updated_at":   "",
		})
		return
	}

	writeJSON(w, http.StatusOK, peekState)
}

func (s *Server) handleSessionInbox(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	events, err := s.DB.UnreadForSession(sessionID)
	if err != nil {
		s.Logger.Printf("Error getting inbox for %s: %v", sessionID, err)
		writeError(w, http.StatusInternalServerError, "Failed to get inbox")
		return
	}

	writeJSON(w, http.StatusOK, events)
}

// buildCmuxOrderMap queries cmux for workspace and surface ordering,
// returning a map of surface UUID → ordinal position.
func buildCmuxOrderMap() map[string]int {
	result := make(map[string]int)

	// Get workspace list (ordered as they appear in cmux)
	wsOut, err := exec.Command("cmux", "workspace", "list", "--json").Output()
	if err != nil {
		return result
	}
	var wsData struct {
		Workspaces []struct {
			Ref string `json:"ref"`
		} `json:"workspaces"`
	}
	if err := json.Unmarshal(wsOut, &wsData); err != nil {
		return result
	}

	// For each workspace, get surface ordering
	for wsIdx, ws := range wsData.Workspaces {
		surfOut, err := exec.Command("cmux", "list-pane-surfaces",
			"--workspace", ws.Ref, "--id-format", "uuids").Output()
		if err != nil {
			continue
		}
		// Parse surface list — each line starts with optional "* " then "UUID"
		for surfIdx, line := range splitLines(string(surfOut)) {
			uuid := extractSurfaceUUID(line)
			if uuid != "" {
				result[uuid] = wsIdx*1000 + surfIdx
			}
		}
	}

	return result
}

func splitLines(s string) []string {
	var lines []string
	for _, line := range splitString(s, '\n') {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func splitString(s string, sep byte) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

func extractSurfaceUUID(line string) string {
	// Lines look like: "* UUID  ..." or "  UUID  ..."
	// UUID is 36 chars: 8-4-4-4-12
	for i := 0; i <= len(line)-36; i++ {
		candidate := line[i : i+36]
		if isUUID(candidate) {
			return candidate
		}
	}
	return ""
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// enrichSession computes derived fields for a session
func (s *Server) enrichSession(session db.Session) enrichedSession {
	// Compute display_state
	displayState := "archived"
	if session.Status != "archived" {
		processAlive := discover.IsSessionProcess(session.PID, session.SessionID)
		if !processAlive {
			displayState = "dead"
		} else {
			displayState = "idle"
			if session.LastPrompt != "" {
				if lastPrompt, err := time.Parse(time.RFC3339, session.LastPrompt); err == nil {
					if time.Since(lastPrompt) < 24*time.Hour {
						displayState = "active"
					}
				}
			}
		}
	}

	// Fetch unread count and breakdown
	var unreadCount int
	var breakdown map[string]int
	if displayState == "active" || displayState == "idle" {
		unreadCount, breakdown, _ = s.DB.UnreadCountForSession(session.SessionID)
	}

	// Fetch peek state
	needsInput := false
	peekState, _ := s.DB.GetPeekState(session.SessionID)
	if peekState != nil {
		needsInput = peekState.NeedsInput
	}

	// Fetch subscriptions count
	subscriptionCount := 0
	subs, _ := s.DB.ListSubscriptions(session.SessionID, false)
	if subs != nil {
		subscriptionCount = len(subs)
	}

	return enrichedSession{
		SessionID:          session.SessionID,
		SessionName:        session.SessionName,
		Branch:             session.Branch,
		Repo:               session.Repo,
		DisplayState:       displayState,
		InboxMode:          session.InboxMode,
		Peekable:           session.TerminalType != "",
		TerminalType:       session.TerminalType,
		UnreadCount:        unreadCount,
		UnreadBreakdown:    breakdown,
		LastActive:         session.LastActive,
		LastPrompt:         session.LastPrompt,
		CmuxWorkspace:      session.CmuxWorkspaceName,
		CmuxWorkspaceColor: session.CmuxWorkspaceColor,
		NeedsInput:         needsInput,
		PID:                session.PID,
		Status:             session.Status,
		SubscriptionCount:  subscriptionCount,
	}
}
