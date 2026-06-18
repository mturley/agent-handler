package cmd

import (
	"fmt"

	"github.com/mturley/agent-handler/config"
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

	// Query unread count
	unreadCount, breakdown, err := d.UnreadCountForSession(slSessionID)
	if err != nil {
		return fmt.Errorf("failed to query unread count: %w", err)
	}

	cmd_color := "\033[36m" // cyan
	reset_color := "\033[0m"

	// Output line 1: inbox status
	if unreadCount == 0 {
		fmt.Printf("%s/inbox%s: No new messages\n", cmd_color, reset_color)
	} else {
		// Build breakdown string
		var breakdownParts []string
		for eventType, count := range breakdown {
			breakdownParts = append(breakdownParts, fmt.Sprintf("%d %s", count, eventType))
		}
		breakdownStr := ""
		if len(breakdownParts) > 0 {
			breakdownStr = fmt.Sprintf(" (%s)", breakdownParts[0])
			if len(breakdownParts) > 1 {
				for i := 1; i < len(breakdownParts); i++ {
					breakdownStr += fmt.Sprintf(", %s", breakdownParts[i])
				}
			}
		}
		fmt.Printf("%s/inbox%s: %d unread%s\n", cmd_color, reset_color, unreadCount, breakdownStr)
	}

	// Output line 2: inbox mode
	// TODO: Detect if polling is stopped for auto mode
	// For now, just show the mode
	active := "\033[1;32m"  // bold green
	dim := "\033[2m"       // dim
	reset := "\033[0m"

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

	// Count by type
	prCount := 0
	jiraCount := 0
	for _, sub := range subs {
		if sub.ResourceType == "pr" {
			prCount++
		} else if sub.ResourceType == "jira" {
			jiraCount++
		}
	}

	// Build subscription summary
	subParts := []string{}
	if prCount > 0 {
		if prCount == 1 {
			subParts = append(subParts, "1 PR")
		} else {
			subParts = append(subParts, fmt.Sprintf("%d PRs", prCount))
		}
	}
	if jiraCount > 0 {
		if jiraCount == 1 {
			subParts = append(subParts, "1 Jira")
		} else {
			subParts = append(subParts, fmt.Sprintf("%d Jira", jiraCount))
		}
	}

	subSummary := ""
	if len(subParts) == 0 {
		subSummary = "no active subscriptions"
	} else {
		subSummary = subParts[0]
		for i := 1; i < len(subParts); i++ {
			subSummary += fmt.Sprintf(", %s", subParts[i])
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
			if d.HasWatcherError(svc) {
				services = append(services, fmt.Sprintf("%s %s✗%s%s", svc, red, reset, dim))
			} else {
				services = append(services, fmt.Sprintf("%s %s✓%s%s", svc, green, reset, dim))
			}
		}
	}

	if len(services) > 0 {
		watcherStatus = " | watchers: "
		watcherStatus += services[0]
		for i := 1; i < len(services); i++ {
			watcherStatus += " " + services[i]
		}
	}

	fmt.Printf("%s/watching%s: %s%s%s%s\n", cmd_color, reset_color, dim, subSummary, watcherStatus, reset)

	return nil
}
