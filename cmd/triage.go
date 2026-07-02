package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

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
		if s.PID > 0 && !discover.IsProcessAlive(s.PID) {
			output.DeadSessions = append(output.DeadSessions, deadSession{
				SessionID:  s.SessionID,
				SessionName: s.SessionName,
				LastActive: s.LastActive,
			})
			output.SessionsDead++
			output.SessionsActive--
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

	if len(output.BlockedSessions) == 0 && len(output.SessionsWithUnread) == 0 &&
		len(output.WatcherErrors) == 0 && len(output.DeadSessions) == 0 &&
		output.EventsSinceLastCheck == 0 {
		fmt.Println("\nAll clear - nothing needs attention.")
	}

	return nil
}
