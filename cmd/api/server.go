package api

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"

	"github.com/mturley/agent-handler/db"
)

type Server struct {
	DB            *db.DB
	CmuxAvailable bool
	DevMode       bool
	WebFS         fs.FS
	Port          int
	Logger        *log.Logger
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("GET /api/capabilities", s.handleCapabilities)
	mux.HandleFunc("GET /api/sessions", s.handleSessions)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleSession)
	mux.HandleFunc("GET /api/sessions/{id}/peek", s.handleSessionPeek)
	mux.HandleFunc("GET /api/sessions/{id}/inbox", s.handleSessionInbox)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/stream", s.handleStream)
	mux.HandleFunc("POST /api/actions/switch", s.handleSwitch)
	mux.HandleFunc("POST /api/actions/peek", s.handleForcePeek)
	mux.HandleFunc("POST /api/actions/dismiss-inbox", s.handleDismissInbox)

	// Static files (skip in dev mode — Vite serves them)
	if !s.DevMode && s.WebFS != nil {
		mux.Handle("/", http.FileServer(http.FS(s.WebFS)))
	}

	addr := fmt.Sprintf(":%d", s.Port)
	s.Logger.Printf("Listening on http://localhost%s", addr)
	return http.ListenAndServe(addr, mux)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
