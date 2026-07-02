package cmd

import (
	"fmt"
	"time"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
	"github.com/mturley/agent-handler/watcher"
	"github.com/spf13/cobra"
)

var statuslineCmd = &cobra.Command{
	Use:   "statusline",
	Short: "Output statusline info for a session",
	RunE:  runStatusline,
}

var slSessionID string

func init() {
	statuslineCmd.GroupID = "agent"
	rootCmd.AddCommand(statuslineCmd)
	statuslineCmd.Flags().StringVar(&slSessionID, "session", "", "session ID")
	statuslineCmd.MarkFlagRequired("session")
}

func runStatusline(cmd *cobra.Command, args []string) error {
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	// Get session
	session, err := d.GetSession(slSessionID)
	if err != nil || session == nil || session.Status == "archived" {
		fmt.Println("Session not registered with handler. Send a prompt to register.")
		return nil
	}

	// Check if this is a handler session
	if session.Role == "handler" {
		return runHandlerStatusline(cmd, d, session)
	}

	// Query unread count
	unreadCount, breakdown, err := d.UnreadCountForSession(slSessionID)
	if err != nil {
		return fmt.Errorf("failed to query unread count: %w", err)
	}

	// Query direct message count
	directCount, err := d.DirectCountForSession(slSessionID)
	if err != nil {
		return fmt.Errorf("failed to query direct count: %w", err)
	}

	cmd_color := "\033[36m" // cyan
	reset_color := "\033[0m"
	yellow := "\033[33m" // yellow

	// Output line 1: inbox status
	if unreadCount == 0 {
		fmt.Printf("%s/inbox%s: No new messages", cmd_color, reset_color)
	} else {
		// Build breakdown string
		var breakdownParts []string
		for eventType, count := range breakdown {
			breakdownParts = append(breakdownParts, fmt.Sprintf("%d %s", count, watcher.EventType(eventType).DisplayName()))
		}
		breakdownStr := ""
		if len(breakdownParts) > 0 {
			joined := breakdownParts[0]
			for i := 1; i < len(breakdownParts); i++ {
				joined += fmt.Sprintf(", %s", breakdownParts[i])
			}
			breakdownStr = fmt.Sprintf(" (%s)", joined)
		}
		fmt.Printf("%s/inbox%s: %s● %d unread%s%s", cmd_color, reset_color, yellow, unreadCount, reset_color, breakdownStr)
	}

	// Add direct message indicator if > 0
	if directCount > 0 {
		fmt.Printf(" | %s● %d direct%s", yellow, directCount, reset_color)
	}
	fmt.Println()

	// Auto-delivered count (only in auto mode, after /inbox line)
	if session.InboxMode == "auto" {
		autoCount, err := d.AutoDeliveredCount(slSessionID)
		if err == nil && autoCount > 0 {
			fmt.Printf("%s  ● %d auto-delivered since last prompt%s\n", yellow, autoCount, reset_color)
		}
	}

	// Output line 2: inbox mode
	// TODO: Detect if polling is stopped for auto mode
	// For now, just show the mode
	active := "\033[1;32m"  // bold green
	dim := "\033[2m"       // dim
	reset := "\033[0m"

	modes := map[string]string{"manual": "manual", "on-submit": "on-submit", "auto": "auto"}
	rendered := ""
	for i, mode := range []string{"on-submit", "manual", "auto"} {
		if i > 0 {
			rendered += fmt.Sprintf("%s | %s", dim, reset)
		}
		if session.InboxMode == mode {
			rendered += fmt.Sprintf("%s%s%s", active, modes[mode], reset)
		} else {
			rendered += fmt.Sprintf("%s%s%s", dim, modes[mode], reset)
		}
	}
	fmt.Printf("%s/inbox-mode%s: %s\n", cmd_color, reset_color, rendered)

	// Output line 3: active subscriptions and watcher status
	subs, err := d.ListSubscriptions(slSessionID, false)
	if err != nil {
		return fmt.Errorf("failed to query subscriptions: %w", err)
	}

	// Count subscriptions by type
	prCount := 0
	jiraCount := 0
	for _, sub := range subs {
		if sub.ResourceType == "pr" {
			prCount++
		} else if sub.ResourceType == "jira" {
			jiraCount++
		}
	}

	// Check which resource types have unread events
	prUnread := false
	jiraUnread := false
	for eventType := range breakdown {
		switch watcher.EventType(eventType) {
		case watcher.EventTypePRComment, watcher.EventTypePRReviewComment, watcher.EventTypePRReviewRequested, watcher.EventTypePRApproved,
			watcher.EventTypePRClosed, watcher.EventTypePRMerged, watcher.EventTypePRReopened, watcher.EventTypePRNewCommits,
			watcher.EventTypeCICheckPassed, watcher.EventTypeCICheckFailed:
			prUnread = true
		case watcher.EventTypeJiraComment, watcher.EventTypeJiraStatusChange, watcher.EventTypeJiraAssigned,
			watcher.EventTypeJiraDescChanged, watcher.EventTypeJiraLabelsChanged:
			jiraUnread = true
		case watcher.EventTypeWatcherError:
			// Check if this error is for PR or Jira subscriptions
			if prCount > 0 {
				prUnread = true
			}
			if jiraCount > 0 {
				jiraUnread = true
			}
		}
	}

	// Build subscription summary (normal weight, not dim)
	subParts := []string{}
	if prCount > 0 {
		label := "1 PR"
		if prCount > 1 {
			label = fmt.Sprintf("%d PRs", prCount)
		}
		if prUnread {
			subParts = append(subParts, fmt.Sprintf("%s● %s%s", yellow, label, reset))
		} else {
			subParts = append(subParts, label)
		}
	}
	if jiraCount > 0 {
		label := "1 Jira"
		if jiraCount > 1 {
			label = fmt.Sprintf("%d Jira", jiraCount)
		}
		if jiraUnread {
			subParts = append(subParts, fmt.Sprintf("%s● %s%s", yellow, label, reset))
		} else {
			subParts = append(subParts, label)
		}
	}

	subSummary := ""
	if len(subParts) == 0 {
		subSummary = fmt.Sprintf("%sno active subscriptions%s", dim, reset)
	} else {
		subSummary = subParts[0]
		for i := 1; i < len(subParts); i++ {
			subSummary += ", " + subParts[i]
		}
	}

	// Check watcher status
	cfg, err := config.Read(config.DefaultPath())
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	watcherStatus := ""
	green := "\033[32m" // green
	red := "\033[31m"   // red
	services := []string{}
	for _, svc := range []string{"github", "jira"} {
		if cfg.IsServiceConfigured(svc) && watcher.IsInstalled(svc) {
			lastRun := watcher.LastRunTime(svc)
			ago := ""
			if lastRun != nil {
				ago = fmt.Sprintf(" (%s ago)", formatDuration(time.Since(*lastRun)))
			}
			if d.HasWatcherError(svc) {
				services = append(services, fmt.Sprintf("%s✗%s%s %s%s", red, reset, dim, svc, ago))
			} else {
				services = append(services, fmt.Sprintf("%s✓%s%s %s%s", green, reset, dim, svc, ago))
			}
		}
	}

	if len(services) > 0 {
		watcherStatus = " | "
		for i, s := range services {
			if i > 0 {
				watcherStatus += " "
			}
			watcherStatus += s
		}
	}

	fmt.Printf("%s/watching%s: %s%s%s%s\n", cmd_color, reset_color, subSummary, dim, watcherStatus, reset)

	return nil
}

func runHandlerStatusline(cmd *cobra.Command, d *db.DB, session *db.Session) error {
	cmd_color := "\033[36m" // cyan
	reset_color := "\033[0m"
	yellow := "\033[33m" // yellow
	green := "\033[32m" // green
	red := "\033[31m"   // red
	dim := "\033[2m"    // dim
	reset := "\033[0m"

	// Count active sessions (excluding self)
	sessions, err := d.ListSessions(false, 1000, 0)
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	activeCount := 0
	for _, s := range sessions {
		if s.Status == "active" && s.SessionID != session.SessionID {
			if s.PID > 0 && !discover.IsSessionProcess(s.PID, s.SessionID) {
				continue
			}
			activeCount++
		}
	}

	// Count blocked sessions
	blockedCount := 0
	blockedRows, err := d.Query(`
		SELECT COUNT(*) FROM (
			SELECT s.session_id
			FROM sessions s
			JOIN events e ON e.session_id = s.session_id AND e.type = 'blocked'
			WHERE s.status = 'active'
			  AND NOT EXISTS (
			    SELECT 1 FROM events e2
			    WHERE e2.session_id = s.session_id AND e2.type = 'unblocked' AND e2.ts > e.ts
			  )
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to query blocked count: %w", err)
	}
	defer blockedRows.Close()
	if blockedRows.Next() {
		blockedRows.Scan(&blockedCount)
	}

	// Count events since handler's cursor
	cursor, err := d.GetCursor(session.SessionID)
	if err != nil {
		return fmt.Errorf("failed to get cursor: %w", err)
	}
	if cursor == "" {
		cursor = "1970-01-01T00:00:00Z"
	}

	newEventCount := 0
	err = d.QueryRow(`SELECT COUNT(*) FROM events WHERE ts > ?`, cursor).Scan(&newEventCount)
	if err != nil {
		return fmt.Errorf("failed to count new events: %w", err)
	}

	// Get direct message count
	directCount, err := d.DirectCountForSession(session.SessionID)
	if err != nil {
		return fmt.Errorf("failed to query direct count: %w", err)
	}

	// Line 1: Handler status
	fmt.Printf("%s/handler%s: %d active, %d blocked | %d new events",
		cmd_color, reset_color, activeCount, blockedCount, newEventCount)
	if directCount > 0 {
		fmt.Printf(" | %s● %d direct%s", yellow, directCount, reset_color)
	}
	fmt.Println()

	// Auto-delivered count for handler
	autoCount, err := d.AutoDeliveredCountAll(session.SessionID)
	if err == nil && autoCount > 0 {
		fmt.Printf("%s  ● %d consumed since last prompt%s\n", yellow, autoCount, reset_color)
	}

	// Line 3: Watching with GLOBAL resource count
	// Count all subscriptions across all sessions
	allSubs, err := d.Query(`
		SELECT resource_type, COUNT(*) as count
		FROM subscriptions
		WHERE deleted_at IS NULL
		GROUP BY resource_type
	`)
	if err != nil {
		return fmt.Errorf("failed to query global subscriptions: %w", err)
	}
	defer allSubs.Close()

	prCount := 0
	jiraCount := 0
	for allSubs.Next() {
		var resourceType string
		var count int
		if err := allSubs.Scan(&resourceType, &count); err != nil {
			return fmt.Errorf("failed to scan subscription count: %w", err)
		}
		if resourceType == "pr" {
			prCount += count
		} else if resourceType == "jira" {
			jiraCount += count
		}
	}

	// Build subscription summary
	subParts := []string{}
	if prCount > 0 {
		label := "1 PR"
		if prCount > 1 {
			label = fmt.Sprintf("%d PRs", prCount)
		}
		subParts = append(subParts, label)
	}
	if jiraCount > 0 {
		label := "1 Jira"
		if jiraCount > 1 {
			label = fmt.Sprintf("%d Jira", jiraCount)
		}
		subParts = append(subParts, label)
	}

	subSummary := ""
	if len(subParts) == 0 {
		subSummary = fmt.Sprintf("%sno active subscriptions%s", dim, reset)
	} else {
		subSummary = subParts[0]
		for i := 1; i < len(subParts); i++ {
			subSummary += ", " + subParts[i]
		}
	}

	// Check watcher status
	cfg, err := config.Read(config.DefaultPath())
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	watcherStatus := ""
	services := []string{}
	for _, svc := range []string{"github", "jira"} {
		if cfg.IsServiceConfigured(svc) && watcher.IsInstalled(svc) {
			lastRun := watcher.LastRunTime(svc)
			ago := ""
			if lastRun != nil {
				ago = fmt.Sprintf(" (%s ago)", formatDuration(time.Since(*lastRun)))
			}
			if d.HasWatcherError(svc) {
				services = append(services, fmt.Sprintf("%s✗%s%s %s%s", red, reset, dim, svc, ago))
			} else {
				services = append(services, fmt.Sprintf("%s✓%s%s %s%s", green, reset, dim, svc, ago))
			}
		}
	}

	if len(services) > 0 {
		watcherStatus = " | "
		for i, s := range services {
			if i > 0 {
				watcherStatus += " "
			}
			watcherStatus += s
		}
	}

	fmt.Printf("%s/watching%s: %s%s%s%s\n", cmd_color, reset_color, subSummary, dim, watcherStatus, reset)

	return nil
}
