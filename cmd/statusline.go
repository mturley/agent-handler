package cmd

import (
	"fmt"
	"strings"
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

	// Emit config prefix for the shell hook
	cfg, err := config.Read(config.DefaultPath())
	if err != nil {
		cfg = &config.Config{}
	}
	ctxFlag, gitFlag := 0, 0
	if cfg.StatuslineShowContext() {
		ctxFlag = 1
	}
	if cfg.StatuslineShowGit() {
		gitFlag = 1
	}
	fmt.Printf("__cfg:context=%d,git=%d\n", ctxFlag, gitFlag)

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

	dim := "\033[2m" // dim
	reset := "\033[0m"

	// Output line 1: inbox status
	if unreadCount == 0 {
		fmt.Printf("%s/inbox%s: No new messages %s— %s%s/message%s%s to talk to other sessions%s",
			cmd_color, reset_color, dim, dim, cmd_color, reset, dim, reset)
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

	modes := map[string]string{"manual": "manual", "on-submit": "on-submit", "auto": "auto"}
	rendered := ""
	for i, mode := range []string{"manual", "on-submit", "auto"} {
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

	// Build resource links
	resourceLinks := ""
	if len(subs) > 0 {
		var links []string
		for _, sub := range subs {
			label := shortResourceLabel(sub.ResourceType, sub.ResourceID)
			url := ""
			if sub.ResourceURL != nil {
				url = *sub.ResourceURL
			}
			if url == "" {
				url = cfg.DefaultResourceURL(sub.ResourceType, sub.ResourceID)
			}
			if url != "" {
				blue := "\033[34m"
				underline := "\033[4m"
				links = append(links, fmt.Sprintf("%s%s\033]8;;%s\033\\%s\033]8;;\033\\%s", blue, underline, url, label, reset))
			} else {
				links = append(links, fmt.Sprintf("%s%s%s", dim, label, reset))
			}
		}
		joined := links[0]
		for i := 1; i < len(links); i++ {
			joined += fmt.Sprintf("%s, %s", dim, reset) + links[i]
		}
		resourceLinks = joined
	}

	// The trailing segment after watcher status: resource links, /watch hint, or nothing
	trailingSegment := ""
	if resourceLinks != "" && len(subs) <= 4 {
		trailingSegment = fmt.Sprintf(" %s| %s%s", dim, reset, resourceLinks)
	} else if len(subs) == 0 {
		trailingSegment = fmt.Sprintf(" %s| %s/watch%s%s to follow PRs or Jira issues%s", dim, cmd_color, reset, dim, reset)
	}

	fmt.Printf("%s/watching%s: %s%s%s%s%s\n", cmd_color, reset_color, subSummary, dim, watcherStatus, reset, trailingSegment)
	if resourceLinks != "" && len(subs) > 4 {
		fmt.Printf("%s  ↳ %s%s\n", dim, reset, resourceLinks)
	}

	return nil
}

func runHandlerStatusline(cmd *cobra.Command, d *db.DB, session *db.Session) error {
	// Emit config prefix for the shell hook
	hcfg, err := config.Read(config.DefaultPath())
	if err != nil {
		hcfg = &config.Config{}
	}
	ctxFlag := 0
	if hcfg.StatuslineShowContext() {
		ctxFlag = 1
	}
	fmt.Printf("__cfg:context=%d,git=0\n", ctxFlag)

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

	// Line 1: Sessions overview
	fmt.Printf("%sSessions%s: %d active, %d blocked %s— %s/handler%s %sto summarize all sessions%s\n",
		"\033[1m", reset, activeCount, blockedCount, dim, cmd_color, reset, dim, reset)

	// Get direct message count
	directCount, err := d.DirectCountForSession(session.SessionID)
	if err != nil {
		return fmt.Errorf("failed to query direct count: %w", err)
	}

	// Get global unread count (handler sees all events, not just targeted ones)
	unreadCount, breakdown, err := d.GlobalUnreadCountForSession(session.SessionID)
	if err != nil {
		return fmt.Errorf("failed to query unread count: %w", err)
	}

	// Line 2: Inbox status (global — all events across all sessions)
	if unreadCount == 0 {
		fmt.Printf("%s/inbox%s: No new events %s— %s%s/message%s%s to talk to other sessions%s",
			cmd_color, reset_color, dim, dim, cmd_color, reset, dim, reset)
	} else {
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
	if directCount > 0 {
		fmt.Printf(" | %s● %d direct%s", yellow, directCount, reset_color)
	}
	fmt.Println()

	// Auto-delivered count (only in auto mode)
	if session.InboxMode == "auto" {
		autoCount, err := d.AutoDeliveredCount(session.SessionID)
		if err == nil && autoCount > 0 {
			fmt.Printf("%s  ● %d auto-delivered since last prompt%s\n", yellow, autoCount, reset_color)
		}
	}

	// Line 3: Inbox mode
	active := "\033[1;32m" // bold green
	modes := map[string]string{"manual": "manual", "on-submit": "on-submit", "auto": "auto"}
	rendered := ""
	for i, mode := range []string{"manual", "on-submit", "auto"} {
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
	watcherStatus := ""
	services := []string{}
	for _, svc := range []string{"github", "jira"} {
		if hcfg.IsServiceConfigured(svc) && watcher.IsInstalled(svc) {
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

// shortResourceLabel returns a compact label for a resource.
// PRs: "owner/repo#123" → "#123", Jira: "RHOAIENG-456" → "RHOAIENG-456"
func shortResourceLabel(resourceType, resourceID string) string {
	if resourceType == "pr" {
		if idx := strings.LastIndex(resourceID, "#"); idx >= 0 {
			return resourceID[idx:]
		}
	}
	return resourceID
}
