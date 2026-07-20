package api

import "net/http"

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cmux": s.CmuxAvailable,
	})
}
