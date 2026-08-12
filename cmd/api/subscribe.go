package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
)

var (
	// https://github.com/owner/repo/pull/123
	githubPRRe = regexp.MustCompile(`^https?://github\.com/([^/]+/[^/]+)/pull/(\d+)`)
	// owner/repo#123
	ownerRepoNumRe = regexp.MustCompile(`^([^/\s]+/[^/\s#]+)#(\d+)$`)
	// A Jira issue key like RHOAIENG-12345
	jiraKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9]+-\d+$`)
	// .../browse/RHOAIENG-12345
	jiraBrowseRe = regexp.MustCompile(`/browse/([A-Z][A-Z0-9]+-\d+)`)
)

// parseResourceInput turns a user-pasted string (a GitHub PR URL, a Jira URL, a
// Jira key, an owner/repo#N ref, or an explicit type:id) into a resource
// (type, id, url). Returns an error the UI can surface for unrecognized input.
func parseResourceInput(cfg *config.Config, input string) (resType, resID, resURL string, err error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", "", "", fmt.Errorf("enter a PR or Jira link")
	}

	switch {
	case githubPRRe.MatchString(s):
		m := githubPRRe.FindStringSubmatch(s)
		resType, resID = "pr", m[1]+"#"+m[2]
		resURL = "https://github.com/" + m[1] + "/pull/" + m[2]
	case jiraBrowseRe.MatchString(s):
		m := jiraBrowseRe.FindStringSubmatch(s)
		resType, resID = "jira", m[1]
		resURL = s
	case ownerRepoNumRe.MatchString(s):
		m := ownerRepoNumRe.FindStringSubmatch(s)
		resType, resID = "pr", m[1]+"#"+m[2]
	case jiraKeyRe.MatchString(s):
		resType, resID = "jira", s
	case strings.HasPrefix(s, "pr:") || strings.HasPrefix(s, "jira:"):
		i := strings.Index(s, ":")
		resType, resID = s[:i], s[i+1:]
	default:
		return "", "", "", fmt.Errorf("unrecognized resource — paste a GitHub PR URL (…/pull/123) or a Jira issue link/key (e.g. RHOAIENG-123)")
	}

	if resID == "" {
		return "", "", "", fmt.Errorf("could not parse a resource id from %q", input)
	}
	if resURL == "" && cfg != nil {
		resURL = cfg.DefaultResourceURL(resType, resID)
	}
	return resType, resID, resURL, nil
}

type subscribeRequest struct {
	SessionID string `json:"session_id"`
	Input     string `json:"input"`
}

func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	var req subscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	if req.SessionID == "" || strings.TrimSpace(req.Input) == "" {
		writeError(w, http.StatusBadRequest, "session_id and input are required")
		return
	}

	cfg, _ := config.Read(config.DefaultPath())
	resType, resID, resURL, err := parseResourceInput(cfg, req.Input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Require the backing service to be configured, mirroring `handler subscribe`.
	if service := config.ResourceTypeToService(resType); service != "" {
		if !config.ServiceConfiguredForWatching(service) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%s is not configured for watching. Run `handler watcher auth %s`.", service, service))
			return
		}
	}

	writableDB, err := db.Open(db.DefaultPath())
	if err != nil {
		s.Logger.Printf("Error opening writable DB: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer writableDB.Close()

	var urlPtr *string
	if resURL != "" {
		urlPtr = &resURL
	}
	if err := writableDB.Subscribe(db.Subscription{
		ID:           uuid.New().String(),
		SessionID:    req.SessionID,
		ResourceType: resType,
		ResourceID:   resID,
		ResourceURL:  urlPtr,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		s.Logger.Printf("Error subscribing %s to %s:%s: %v", req.SessionID, resType, resID, err)
		writeError(w, http.StatusInternalServerError, "Failed to subscribe")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"resource_type": resType,
		"resource_id":   resID,
	})
}

type unsubscribeRequest struct {
	SessionID    string `json:"session_id"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
}

func (s *Server) handleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var req unsubscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	if req.SessionID == "" || req.ResourceType == "" || req.ResourceID == "" {
		writeError(w, http.StatusBadRequest, "session_id, resource_type and resource_id are required")
		return
	}

	writableDB, err := db.Open(db.DefaultPath())
	if err != nil {
		s.Logger.Printf("Error opening writable DB: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to open database")
		return
	}
	defer writableDB.Close()

	if err := writableDB.Unsubscribe(req.SessionID, req.ResourceType, req.ResourceID); err != nil {
		s.Logger.Printf("Error unsubscribing %s from %s:%s: %v", req.SessionID, req.ResourceType, req.ResourceID, err)
		writeError(w, http.StatusInternalServerError, "Failed to unsubscribe")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
