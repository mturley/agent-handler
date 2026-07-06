package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/discover"
	"github.com/spf13/cobra"
)

var triageCmd = &cobra.Command{
	Use:   "triage",
	Short: "Aggregate what needs attention across all sessions",
	RunE:  runTriage,
}

func init() {
	triageCmd.GroupID = "human"
	rootCmd.AddCommand(triageCmd)
}

type triageOutput struct {
	SessionsActive        int                   `json:"sessions_active"`
	SessionsBlocked       int                   `json:"sessions_blocked"`
	SessionsDead          int                   `json:"sessions_dead"`
	BlockedSessions       []blockedSession      `json:"blocked_sessions"`
	SessionsWithUnread    []sessionUnread       `json:"sessions_with_unread"`
	WatcherErrors         []watcherError        `json:"watcher_errors"`
	EventsSinceLastCheck  int                   `json:"events_since_last_check"`
	DeadSessions          []deadSession         `json:"dead_sessions"`
	SessionResources      []sessionResource     `json:"session_resources"`
	StaleResources        []staleResource       `json:"stale_resources"`
}

type blockedSession struct {
	SessionID    string `json:"session_id"`
	SessionName  string `json:"session_name"`
	Branch       string `json:"branch"`
	BlockedSince string `json:"blocked_since"`
}

type sessionUnread struct {
	SessionID    string         `json:"session_id"`
	SessionName  string         `json:"session_name"`
	UnreadCount  int            `json:"unread_count"`
	UnreadTypes  map[string]int `json:"unread_types"`
}

type watcherError struct {
	Name             string `json:"name"`
	LastErrorMessage string `json:"last_error_message"`
}

type deadSession struct {
	SessionID  string `json:"session_id"`
	SessionName string `json:"session_name"`
	LastActive string `json:"last_active"`
}

type sessionResource struct {
	SessionID    string            `json:"session_id"`
	SessionName  string            `json:"session_name"`
	Resources    []resourceDetail  `json:"resources"`
}

type resourceDetail struct {
	ResourceType     string          `json:"resource_type"`
	ResourceID       string          `json:"resource_id"`
	ResourceURL      *string         `json:"resource_url,omitempty"`
	State            json.RawMessage `json:"state"`
	WatcherUpdatedAt string          `json:"watcher_updated_at,omitempty"`
}

type staleResource struct {
	ResourceType     string `json:"resource_type"`
	ResourceID       string `json:"resource_id"`
	WatcherUpdatedAt string `json:"watcher_updated_at"`
	StaleMinutes     int    `json:"stale_minutes"`
}

func runTriage(cmd *cobra.Command, args []string) error {
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	output := triageOutput{
		BlockedSessions:    []blockedSession{},
		SessionsWithUnread: []sessionUnread{},
		WatcherErrors:      []watcherError{},
		DeadSessions:       []deadSession{},
		SessionResources:   []sessionResource{},
		StaleResources:     []staleResource{},
	}

	// Get all active sessions
	sessions, err := d.ListSessions(false, 1000, 0)
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	// Count active sessions
	for _, s := range sessions {
		if s.Status == "active" {
			output.SessionsActive++
		}
	}

	// Find blocked sessions
	rows, err := d.Query(`
		SELECT s.session_id, COALESCE(s.session_name,''), s.branch, e.ts as blocked_since
		FROM sessions s
		JOIN events e ON e.session_id = s.session_id AND e.type = 'blocked'
		WHERE s.status = 'active'
		  AND NOT EXISTS (
		    SELECT 1 FROM events e2
		    WHERE e2.session_id = s.session_id AND e2.type = 'unblocked' AND e2.ts > e.ts
		  )
	`)
	if err != nil {
		return fmt.Errorf("failed to query blocked sessions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var bs blockedSession
		if err := rows.Scan(&bs.SessionID, &bs.SessionName, &bs.Branch, &bs.BlockedSince); err != nil {
			return fmt.Errorf("failed to scan blocked session: %w", err)
		}
		output.BlockedSessions = append(output.BlockedSessions, bs)
		output.SessionsBlocked++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating blocked sessions: %w", err)
	}

	// Find sessions with unread events
	for _, s := range sessions {
		if s.Status != "active" {
			continue
		}
		count, breakdown, err := d.UnreadCountForSession(s.SessionID)
		if err != nil {
			return fmt.Errorf("failed to get unread count for session %s: %w", s.SessionID, err)
		}
		if count > 0 {
			output.SessionsWithUnread = append(output.SessionsWithUnread, sessionUnread{
				SessionID:   s.SessionID,
				SessionName: s.SessionName,
				UnreadCount: count,
				UnreadTypes: breakdown,
			})
		}
	}

	// Check watcher errors
	for _, svc := range []string{"github", "jira"} {
		if d.HasWatcherError(svc) {
			ws, err := d.GetWatcherStatus(svc)
			if err != nil {
				return fmt.Errorf("failed to get watcher status for %s: %w", svc, err)
			}
			if ws != nil {
				output.WatcherErrors = append(output.WatcherErrors, watcherError{
					Name:             svc,
					LastErrorMessage: ws.LastErrorMessage,
				})
			}
		}
	}

	// Count events since last check (for the current session)
	currentSessionID, err := resolveSessionID(cmd)
	if err == nil && currentSessionID != "" {
		cursor, err := d.GetCursor(currentSessionID)
		if err == nil {
			if cursor == "" {
				cursor = "1970-01-01T00:00:00Z"
			}
			var count int
			err = d.QueryRow(`SELECT COUNT(*) FROM events WHERE ts > ?`, cursor).Scan(&count)
			if err == nil {
				output.EventsSinceLastCheck = count
			}
		}
	}

	// Find dead sessions and adjust active count
	for _, s := range sessions {
		if s.Status != "active" {
			continue
		}
		if s.PID > 0 && !discover.IsSessionProcess(s.PID, s.SessionID) {
			output.DeadSessions = append(output.DeadSessions, deadSession{
				SessionID:  s.SessionID,
				SessionName: s.SessionName,
				LastActive: s.LastActive,
			})
			output.SessionsDead++
			output.SessionsActive--
		}
	}

	// Gather resource state per session
	staleThreshold := 5 * time.Minute
	seenResources := make(map[string]bool) // dedup stale resources across sessions

	for _, s := range sessions {
		if s.Status != "active" {
			continue
		}
		// Skip dead sessions
		isDead := false
		for _, ds := range output.DeadSessions {
			if ds.SessionID == s.SessionID {
				isDead = true
				break
			}
		}
		if isDead {
			continue
		}

		states, err := d.ListResourceStatesForSession(s.SessionID)
		if err != nil || len(states) == 0 {
			continue
		}

		sr := sessionResource{
			SessionID:   s.SessionID,
			SessionName: s.SessionName,
			Resources:   []resourceDetail{},
		}

		for _, rs := range states {
			rd := resourceDetail{
				ResourceType:     rs.ResourceType,
				ResourceID:       rs.ResourceID,
				ResourceURL:      rs.ResourceURL,
				State:            json.RawMessage(rs.StateJSON),
				WatcherUpdatedAt: rs.WatcherUpdatedAt,
			}
			sr.Resources = append(sr.Resources, rd)

			// Check staleness
			key := rs.ResourceType + ":" + rs.ResourceID
			if !seenResources[key] && rs.WatcherUpdatedAt != "" {
				seenResources[key] = true
				wut, err := time.Parse(time.RFC3339, rs.WatcherUpdatedAt)
				if err == nil && time.Since(wut) > staleThreshold {
					output.StaleResources = append(output.StaleResources, staleResource{
						ResourceType:     rs.ResourceType,
						ResourceID:       rs.ResourceID,
						WatcherUpdatedAt: rs.WatcherUpdatedAt,
						StaleMinutes:     int(time.Since(wut).Minutes()),
					})
				}
			}
		}

		output.SessionResources = append(output.SessionResources, sr)
	}

	// Trigger catch-up for stale resources (best-effort, non-blocking)
	if len(output.StaleResources) > 0 {
		cfg, _ := config.Read(config.DefaultPath())
		if cfg != nil {
			staleByService := make(map[string][]string)
			for _, sr := range output.StaleResources {
				svc := config.ResourceTypeToService(sr.ResourceType)
				if svc != "" && cfg.IsServiceConfigured(svc) {
					staleByService[svc] = append(staleByService[svc], sr.ResourceID)
				}
			}
			for svc, resources := range staleByService {
				resourceList := strings.Join(resources, ",")
				go func(s, r string) {
					exec.Command("handler", "watcher", "run", s, "--resources", r).Run()
				}(svc, resourceList)
			}
		}
	}

	// Output
	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(output)
	}

	// Text output
	fmt.Printf("Handler Triage\n\n")

	fmt.Printf("Sessions: %d active", output.SessionsActive)
	if output.SessionsBlocked > 0 {
		fmt.Printf(", %d blocked", output.SessionsBlocked)
	}
	if output.SessionsDead > 0 {
		fmt.Printf(", %d dead", output.SessionsDead)
	}
	fmt.Println()

	if len(output.BlockedSessions) > 0 {
		fmt.Println("\nBlocked Sessions:")
		for _, bs := range output.BlockedSessions {
			name := bs.SessionName
			if name == "" {
				name = bs.SessionID[:8]
			}
			fmt.Printf("  %s (%s) - blocked since %s\n", name, bs.Branch, bs.BlockedSince)
		}
	}

	if len(output.SessionsWithUnread) > 0 {
		fmt.Println("\nSessions with Unread Events:")
		for _, su := range output.SessionsWithUnread {
			name := su.SessionName
			if name == "" {
				name = su.SessionID[:8]
			}
			typeBreakdown := ""
			if len(su.UnreadTypes) > 0 {
				var parts []string
				for t, c := range su.UnreadTypes {
					parts = append(parts, fmt.Sprintf("%s: %d", t, c))
				}
				typeBreakdown = fmt.Sprintf(" (%s)", strings.Join(parts, ", "))
			}
			fmt.Printf("  %s - %d unread%s\n", name, su.UnreadCount, typeBreakdown)
		}
	}

	if len(output.WatcherErrors) > 0 {
		fmt.Println("\nWatcher Errors:")
		for _, we := range output.WatcherErrors {
			fmt.Printf("  %s: %s\n", we.Name, we.LastErrorMessage)
		}
	}

	if output.EventsSinceLastCheck > 0 {
		fmt.Printf("\nNew Events: %d since last check\n", output.EventsSinceLastCheck)
	}

	if len(output.DeadSessions) > 0 {
		fmt.Println("\nDead Sessions:")
		for _, ds := range output.DeadSessions {
			name := ds.SessionName
			if name == "" {
				name = ds.SessionID[:8]
			}
			fmt.Printf("  %s - last active %s\n", name, ds.LastActive)
		}
	}

	if len(output.SessionResources) > 0 {
		fmt.Println("\nSession Resources:")
		for _, sr := range output.SessionResources {
			name := sr.SessionName
			if name == "" {
				name = sr.SessionID[:8]
			}
			for _, r := range sr.Resources {
				fmt.Printf("  %s → %s:%s", name, r.ResourceType, r.ResourceID)
				if r.WatcherUpdatedAt != "" {
					wut, err := time.Parse(time.RFC3339, r.WatcherUpdatedAt)
					if err == nil {
						fmt.Printf(" (updated %s ago)", formatDuration(time.Since(wut)))
					}
				}
				fmt.Println()
			}
		}
	}

	if len(output.StaleResources) > 0 {
		fmt.Println("\nStale Resources (catch-up triggered):")
		for _, sr := range output.StaleResources {
			fmt.Printf("  %s:%s — last updated %dm ago\n", sr.ResourceType, sr.ResourceID, sr.StaleMinutes)
		}
	}

	if len(output.BlockedSessions) == 0 && len(output.SessionsWithUnread) == 0 &&
		len(output.WatcherErrors) == 0 && len(output.DeadSessions) == 0 &&
		output.EventsSinceLastCheck == 0 {
		fmt.Println("\nAll clear - nothing needs attention.")
	}

	return nil
}
