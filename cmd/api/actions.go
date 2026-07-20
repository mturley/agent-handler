package api

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"time"

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
