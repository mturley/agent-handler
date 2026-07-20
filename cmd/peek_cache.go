package cmd

import (
	"time"

	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
	"github.com/mturley/agent-handler/terminal"
)

// PeekScanWithCache returns cached peek states if the cache is fresh (within maxAge),
// otherwise performs a full cmux capture-pane scan on all peekable sessions and
// updates the cache.
func PeekScanWithCache(d *db.DB, maxAge time.Duration) []db.PeekState {
	age, err := d.PeekStatesAge()
	if err == nil && age <= maxAge {
		states, err := d.ListPeekStates()
		if err == nil {
			return states
		}
	}

	// Cache is stale — do a fresh scan
	sessions, err := d.ListSessions(false, 1000, 0)
	if err != nil {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	var results []db.PeekState

	for _, s := range sessions {
		if s.TerminalType == "" || s.TerminalID == "" || s.Role == "handler" {
			continue
		}
		if s.PID > 0 && !discover.IsSessionProcess(s.PID, s.SessionID) {
			continue
		}

		backend, err := terminal.NewBackend(s.TerminalType)
		if err != nil {
			continue
		}

		content, err := backend.Capture(s.TerminalID, 0)
		if err != nil {
			continue
		}

		needsInput, reason := terminal.NeedsInput(content)

		d.UpsertPeekState(s.SessionID, content, needsInput, reason, now)

		results = append(results, db.PeekState{
			SessionID:  s.SessionID,
			Content:    content,
			NeedsInput: needsInput,
			Reason:     reason,
			UpdatedAt:  now,
		})
	}

	return results
}

// findSessionsAwaitingApproval returns sessions that need input, using the peek cache.
func findSessionsAwaitingApproval(d *db.DB) []db.Session {
	states := PeekScanWithCache(d, 5*time.Second)

	sessions, err := d.ListSessions(false, 1000, 0)
	if err != nil {
		return nil
	}

	sessionMap := make(map[string]db.Session)
	for _, s := range sessions {
		sessionMap[s.SessionID] = s
	}

	var awaiting []db.Session
	for _, ps := range states {
		if ps.NeedsInput {
			if s, ok := sessionMap[ps.SessionID]; ok {
				awaiting = append(awaiting, s)
			}
		}
	}
	return awaiting
}
