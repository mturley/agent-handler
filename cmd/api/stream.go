package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher.Flush()

	// Track latest event and session working state
	var lastEventTS string
	s.DB.QueryRow("SELECT MAX(ts) FROM events").Scan(&lastEventTS)
	var lastWorkingState string
	s.DB.QueryRow("SELECT COALESCE(GROUP_CONCAT(session_id || ':' || working, ','), '') FROM sessions WHERE status = 'active'").Scan(&lastWorkingState)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			// Check for new events
			var currentMaxTS string
			s.DB.QueryRow("SELECT MAX(ts) FROM events").Scan(&currentMaxTS)

			if currentMaxTS != "" && currentMaxTS != lastEventTS {
				lastEventTS = currentMaxTS
				data, _ := json.Marshal(map[string]string{"type": "events_new"})
				fmt.Fprintf(w, "event: events_new\ndata: %s\n\n", data)
			}

			// Check for session working state changes
			var currentWorkingState string
			s.DB.QueryRow("SELECT COALESCE(GROUP_CONCAT(session_id || ':' || working, ','), '') FROM sessions WHERE status = 'active'").Scan(&currentWorkingState)
			if currentWorkingState != lastWorkingState {
				lastWorkingState = currentWorkingState
			}

			// Always send heartbeat (triggers sessions refetch)
			data, err := json.Marshal(map[string]string{"type": "heartbeat"})
			if err == nil {
				fmt.Fprintf(w, "event: heartbeat\ndata: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}
