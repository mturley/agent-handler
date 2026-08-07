package api

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"time"

	"github.com/google/uuid"
	"github.com/mturley/agent-handler/db"
)

type switchRequest struct {
	SessionID string `json:"session_id"`
}

type peekRequest struct {
	SessionID string `json:"session_id"`
}

type dismissInboxRequest struct {
	SessionID string `json:"session_id"`
}

type dismissEventRequest struct {
	SessionID string `json:"session_id"`
	EventID   string `json:"event_id"`
}

func (s *Server) handleSwitch(w http.ResponseWriter, r *http.Request) {
	var req switchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	if req.SessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	// Run handler switch --session <id>
	cmd := exec.Command("handler", "switch", "--session", req.SessionID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		s.Logger.Printf("Error switching to session %s: %v\nOutput: %s", req.SessionID, err, string(output))
		writeError(w, http.StatusInternalServerError, "Failed to switch session: "+string(output))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"output":  string(output),
	})
}

func (s *Server) handleForcePeek(w http.ResponseWriter, r *http.Request) {
	var req peekRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	if req.SessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	// Run handler peek --session <id> --json
	cmd := exec.Command("handler", "peek", "--session", req.SessionID, "--json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		s.Logger.Printf("Error peeking session %s: %v\nOutput: %s", req.SessionID, err, string(output))
		writeError(w, http.StatusInternalServerError, "Failed to peek session: "+string(output))
		return
	}

	// Parse the JSON output
	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		s.Logger.Printf("Error parsing peek output for %s: %v", req.SessionID, err)
		writeError(w, http.StatusInternalServerError, "Failed to parse peek output")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleDismissInbox(w http.ResponseWriter, r *http.Request) {
	var req dismissInboxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	if req.SessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	// Open a writable DB connection (server's DB is read-only)
	writableDB, err := db.Open(db.DefaultPath())
	if err != nil {
		s.Logger.Printf("Error opening writable DB: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer writableDB.Close()

	// Advance both cursors
	now := time.Now().UTC().Format(time.RFC3339)
	if err := writableDB.AdvanceBothCursors(req.SessionID, now); err != nil {
		s.Logger.Printf("Error advancing cursors for %s: %v", req.SessionID, err)
		writeError(w, http.StatusInternalServerError, "Failed to dismiss inbox")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

func (s *Server) handleDismissEvent(w http.ResponseWriter, r *http.Request) {
	var req dismissEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	if req.SessionID == "" || req.EventID == "" {
		writeError(w, http.StatusBadRequest, "session_id and event_id are required")
		return
	}

	writableDB, err := db.Open(db.DefaultPath())
	if err != nil {
		s.Logger.Printf("Error opening writable DB: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer writableDB.Close()

	if err := writableDB.DismissEvent(req.SessionID, req.EventID); err != nil {
		s.Logger.Printf("Error dismissing event %s for %s: %v", req.EventID, req.SessionID, err)
		writeError(w, http.StatusInternalServerError, "Failed to dismiss event")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

type addReminderRequest struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
}

func (s *Server) handleAddReminder(w http.ResponseWriter, r *http.Request) {
	var req addReminderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	if req.SessionID == "" || req.Title == "" {
		writeError(w, http.StatusBadRequest, "session_id and title are required")
		return
	}

	writableDB, err := db.Open(db.DefaultPath())
	if err != nil {
		s.Logger.Printf("Error opening writable DB: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer writableDB.Close()

	// A reminder targets the session it belongs to (session_id + a session
	// recipient), mirroring `handler emit --type reminder`.
	evt := db.Event{
		ID:        uuid.New().String(),
		TS:        time.Now().UTC().Format(time.RFC3339),
		Source:    "web",
		SessionID: &req.SessionID,
		Type:      "reminder",
		Title:     req.Title,
	}
	recipients := []db.EventRecipient{
		{RecipientType: "session", RecipientValue: req.SessionID},
	}
	if err := writableDB.InsertEvent(evt, recipients, nil); err != nil {
		s.Logger.Printf("Error adding reminder for %s: %v", req.SessionID, err)
		writeError(w, http.StatusInternalServerError, "Failed to add reminder")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

type archiveSessionsRequest struct {
	SessionIDs []string `json:"session_ids"`
}

func (s *Server) handleArchiveSessions(w http.ResponseWriter, r *http.Request) {
	var req archiveSessionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	if len(req.SessionIDs) == 0 {
		writeError(w, http.StatusBadRequest, "session_ids is required")
		return
	}

	writableDB, err := db.Open(db.DefaultPath())
	if err != nil {
		s.Logger.Printf("Error opening writable DB: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer writableDB.Close()

	count, err := writableDB.ArchiveSessions(req.SessionIDs)
	if err != nil {
		s.Logger.Printf("Error archiving sessions: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to archive sessions")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"archived": count,
	})
}
