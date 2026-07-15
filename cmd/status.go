package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
	watcherPkg "github.com/mturley/agent-handler/watcher"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "List sessions and their status",
	RunE:  runStatus,
}

var (
	statusAll   bool
	statusLimit int
)

func init() {
	statusCmd.GroupID = "human"
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().BoolVar(&statusAll, "all", false, "include archived sessions")
	statusCmd.Flags().IntVar(&statusLimit, "limit", 20, "maximum number of sessions to show")
}

func runStatus(cmd *cobra.Command, args []string) error {
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	sessions, err := d.ListSessions(statusAll, statusLimit, 0)
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	type sessionStatus struct {
		SessionID    string         `json:"session_id"`
		SessionName  string         `json:"session_name"`
		Branch       string         `json:"branch"`
		PID          int            `json:"pid"`
		Status       string         `json:"status"`
		DisplayState string         `json:"display_state"`
		InboxMode    string         `json:"inbox_mode"`
		Peekable     bool           `json:"peekable"`
		TerminalType string         `json:"terminal_type,omitempty"`
		UnreadCount  int            `json:"unread_count"`
		LastActive   string         `json:"last_active"`
		Breakdown    map[string]int `json:"unread_breakdown,omitempty"`
	}

	statuses := []sessionStatus{}

	for _, s := range sessions {
		// Determine display state
		displayState := "archived"
		if s.Status != "archived" {
			processAlive := discover.IsSessionProcess(s.PID, s.SessionID)
			if !processAlive {
				displayState = "dead"
			} else {
				// Check if heartbeat is recent (within 5 minutes)
				lastActive, err := time.Parse(time.RFC3339, s.LastActive)
				if err == nil {
					age := time.Since(lastActive)
					if age < 5*time.Minute {
						displayState = "active"
					} else {
						displayState = "idle"
					}
				} else {
					displayState = "idle"
				}
			}
		}

		// Query unread count (skip for archived/dead sessions)
		var unreadCount int
		var breakdown map[string]int
		if displayState == "active" || displayState == "idle" {
			unreadCount, breakdown, err = d.UnreadCountForSession(s.SessionID)
			if err != nil {
				unreadCount = 0
				breakdown = nil
			}
		}

		statuses = append(statuses, sessionStatus{
			SessionID:    s.SessionID,
			SessionName:  s.SessionName,
			Branch:       s.Branch,
			PID:          s.PID,
			Status:       s.Status,
			DisplayState: displayState,
			InboxMode:    s.InboxMode,
			Peekable:     s.TerminalType != "",
			TerminalType: s.TerminalType,
			UnreadCount:  unreadCount,
			LastActive:   s.LastActive,
			Breakdown:    breakdown,
		})
	}

	if jsonOutput {
		type jsonStatus struct {
			sessionStatus
			Repo          string `json:"repo"`
			CmuxWorkspace string `json:"cmux_workspace,omitempty"`
		}
		var jsonStatuses []jsonStatus
		for i, st := range statuses {
			jsonStatuses = append(jsonStatuses, jsonStatus{
				sessionStatus: st,
				Repo:          sessions[i].Repo,
				CmuxWorkspace: sessions[i].CmuxWorkspaceName,
			})
		}
		data, err := json.MarshalIndent(jsonStatuses, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		if len(statuses) == 0 {
			fmt.Println("No sessions found")
			return nil
		}

		dim := "\033[2m"
		reset := "\033[0m"
		bold := "\033[1m"
		green := "\033[32m"
		yellow := "\033[33m"
		red := "\033[31m"

		// Group sessions: repo → workspace → sessions
		type sessionEntry struct {
			status  sessionStatus
			session db.Session
		}

		// Collect repos in order of first appearance, workspaces within each repo
		type workspaceGroup struct {
			name    string
			entries []sessionEntry
		}
		type repoGroup struct {
			name       string
			workspaces []*workspaceGroup
			wsIndex    map[string]int
		}
		var repoOrder []*repoGroup
		repoIndex := make(map[string]int)

		for i, st := range statuses {
			s := sessions[i]
			ri, exists := repoIndex[s.Repo]
			if !exists {
				ri = len(repoOrder)
				repoIndex[s.Repo] = ri
				repoOrder = append(repoOrder, &repoGroup{name: s.Repo, wsIndex: make(map[string]int)})
			}
			rg := repoOrder[ri]

			wsName := s.CmuxWorkspaceName
			wi, wsExists := rg.wsIndex[wsName]
			if !wsExists {
				wi = len(rg.workspaces)
				rg.wsIndex[wsName] = wi
				rg.workspaces = append(rg.workspaces, &workspaceGroup{name: wsName})
			}
			rg.workspaces[wi].entries = append(rg.workspaces[wi].entries, sessionEntry{status: st, session: s})
		}

		for ri, rg := range repoOrder {
			if ri > 0 {
				fmt.Println()
			}
			fmt.Printf("%s%s%s\n", bold, rg.name, reset)

			for _, wg := range rg.workspaces {
				if wg.name != "" {
					fmt.Printf("  %s[%s]%s\n", dim, wg.name, reset)
				}

				for _, e := range wg.entries {
					st := e.status
					stateColor := dim
					switch st.DisplayState {
					case "active":
						stateColor = green
					case "idle":
						stateColor = yellow
					case "dead":
						stateColor = red
					}

					name := st.SessionName
					if name == "" {
						name = st.SessionID[:8]
					}

					peekableStr := ""
					if st.Peekable {
						peekableStr = fmt.Sprintf(" %s👁%s", dim, reset)
					}

					indent := "  "
					if wg.name != "" {
						indent = "    "
					}

					fmt.Printf("%s%s%s%s %s%s%s%s\n", indent, bold, name, reset, stateColor, st.DisplayState, reset, peekableStr)

					if st.Branch != name {
						fmt.Printf("%s%s@ %s%s\n", indent, dim, st.Branch, reset)
					}

					if st.UnreadCount > 0 {
						parts := ""
						for eventType, count := range st.Breakdown {
							if parts != "" {
								parts += ", "
							}
							parts += fmt.Sprintf("%d %s", count, eventType)
						}
						fmt.Printf("%s%d unread (%s)\n", indent, st.UnreadCount, parts)
					}

					lastActive, parseErr := time.Parse(time.RFC3339, st.LastActive)
					if parseErr == nil {
						ago := time.Since(lastActive).Truncate(time.Second)
						fmt.Printf("%s%sLast active: %s ago  |  ID: %s%s\n", indent, dim, formatDuration(ago), st.SessionID, reset)
					}
				}
			}
		}

		// Watcher and resource summary
		fmt.Printf("\n%s─── Watchers ───%s\n", dim, reset)
		cfg, _ := config.Read(config.DefaultPath())
		for _, svc := range []string{"github", "jira"} {
			status := fmt.Sprintf("%s✗ not configured%s", red, reset)
			if cfg != nil && cfg.IsServiceConfigured(svc) {
				if watcherPkg.IsInstalled(svc) {
					lastRun := watcherPkg.LastRunTime(svc)
					runInfo := "never"
					nextInfo := ""
					if lastRun != nil {
						runInfo = formatDuration(time.Since(*lastRun)) + " ago"
						interval := watcherPkg.InstalledInterval(svc)
						if interval > 0 {
							nextRun := lastRun.Add(time.Duration(interval) * time.Second)
							if nextRun.After(time.Now()) {
								nextInfo = fmt.Sprintf(", next: %s", formatDuration(time.Until(nextRun)))
							} else {
								nextInfo = ", next: any moment"
							}
						}
					}
					if watcherPkg.IsRunning(svc) {
						if d.HasWatcherError(svc) {
							errMsg := ""
							if ws, err := d.GetWatcherStatus(svc); err == nil && ws != nil && ws.LastErrorMessage != "" {
								errMsg = fmt.Sprintf("\n  %s         %s%s", dim, ws.LastErrorMessage, reset)
							}
							status = fmt.Sprintf("%s✗ error%s %s(last run: %s%s)%s%s", red, reset, dim, runInfo, nextInfo, reset, errMsg)
						} else {
							status = fmt.Sprintf("%s✓ running%s %s(last run: %s%s)%s", green, reset, dim, runInfo, nextInfo, reset)
						}
					} else {
						status = fmt.Sprintf("%s⏸ stopped%s %s(last run: %s — run 'handler watcher start')%s", yellow, reset, dim, runInfo, reset)
					}
				} else {
					status = fmt.Sprintf("%s✓ configured%s %s(not installed — run 'handler watcher install %s')%s", yellow, reset, dim, svc, reset)
				}
			}
			fmt.Printf("  %-8s %s\n", svc, status)
		}

		// Active resources being watched
		allSubs := make(map[string]int)
		for _, s := range sessions {
			if s.Status == "archived" {
				continue
			}
			subs, _ := d.ListSubscriptions(s.SessionID, false)
			for _, sub := range subs {
				key := sub.ResourceType + ":" + sub.ResourceID
				allSubs[key]++
			}
		}
		if len(allSubs) > 0 {
			byType := make(map[string]int)
			for key := range allSubs {
				parts := strings.SplitN(key, ":", 2)
				byType[parts[0]]++
			}
			var typeSummary []string
			for t, c := range byType {
				typeSummary = append(typeSummary, fmt.Sprintf("%d %s", c, t))
			}
			fmt.Printf("\n%s─── Resources ───%s\n", dim, reset)
			fmt.Printf("  %s%s%s being watched across all sessions\n", bold, strings.Join(typeSummary, ", "), reset)
			fmt.Printf("  %sRun 'handler watching --global' for details%s\n", dim, reset)
		}

		// Count dead sessions
		deadCount := 0
		for _, st := range statuses {
			if st.DisplayState == "dead" {
				deadCount++
			}
		}
		if deadCount > 0 {
			fmt.Printf("\n  %s%d dead session(s). Run 'handler cleanup' to archive.%s\n", dim, deadCount, reset)
		}
	}

	return nil
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
