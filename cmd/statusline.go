package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
	gitpkg "github.com/mturley/agent-handler/git"
	"github.com/mturley/agent-handler/terminal"
	"github.com/mturley/agent-handler/watcher"
	"github.com/spf13/cobra"
)

// ANSI color constants
const (
	colorCyan       = "\033[36m"
	colorYellow     = "\033[33m"
	colorGreen      = "\033[32m"
	colorRed        = "\033[31m"
	colorPurple     = "\033[35m"
	colorBlue       = "\033[34m"
	colorBoldWhite  = "\033[1;37m"
	colorBoldGreen  = "\033[1;32m"
	colorDim        = "\033[2m"
	colorDimGreen   = "\033[2;32m"
	colorClaudeOrange = "\033[38;2;218;119;86m"
	colorUnderline  = "\033[4m"
	colorReset      = "\033[0m"
)

var statuslineCmd = &cobra.Command{
	Use:   "statusline",
	Short: "Output statusline info for a session",
	RunE:  runStatusline,
}

var (
	slSessionID string
	slFromHook  bool
)

func init() {
	statuslineCmd.GroupID = "agent"
	rootCmd.AddCommand(statuslineCmd)
	statuslineCmd.Flags().StringVar(&slSessionID, "session", "", "session ID")
	statuslineCmd.Flags().BoolVar(&slFromHook, "from-hook", false, "read session data from stdin JSON (statusline hook mode)")
}

// hookInput represents the JSON passed on stdin by Claude Code's statusline hook.
type hookInput struct {
	SessionID      string `json:"session_id"`
	SessionName    string `json:"session_name"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	Model          struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	ContextWindow struct {
		UsedPercentage    int `json:"used_percentage"`
		TotalInputTokens  int `json:"total_input_tokens"`
		TotalOutputTokens int `json:"total_output_tokens"`
	} `json:"context_window"`
	Cost struct {
		TotalCostUSD float64 `json:"total_cost_usd"`
	} `json:"cost"`
}

func recordCostSnapshot(wd *db.DB, input *hookInput) {
	now := time.Now().UTC().Format(time.RFC3339)
	today := time.Now().UTC().Format("2006-01-02")
	sessionID := input.SessionID
	reportedCost := input.Cost.TotalCostUSD
	reportedInput := input.ContextWindow.TotalInputTokens
	reportedOutput := input.ContextWindow.TotalOutputTokens

	snap, err := wd.GetCostSnapshot(sessionID)
	if err != nil {
		return
	}

	if snap == nil {
		wd.UpsertCostSnapshot(db.CostSnapshot{
			SessionID:         sessionID,
			ReportedCostUSD:   reportedCost,
			TotalInputTokens:  reportedInput,
			TotalOutputTokens: reportedOutput,
			Model:             input.Model.ID,
			UpdatedAt:         now,
		})
		if reportedCost > 0 {
			wd.UpsertDailyCost(sessionID, today, reportedCost, reportedInput, reportedOutput)
		}
		return
	}

	if reportedCost == snap.ReportedCostUSD {
		return
	}

	var costDelta float64
	var inputDelta, outputDelta int

	if reportedCost < snap.ReportedCostUSD {
		wd.InsertCostAdjustment(sessionID, snap.ReportedCostUSD, "restart_reset", now)
		costDelta = reportedCost
		inputDelta = reportedInput
		outputDelta = reportedOutput
	} else {
		costDelta = reportedCost - snap.ReportedCostUSD
		inputDelta = reportedInput - snap.TotalInputTokens
		outputDelta = reportedOutput - snap.TotalOutputTokens
	}

	wd.UpsertCostSnapshot(db.CostSnapshot{
		SessionID:         sessionID,
		ReportedCostUSD:   reportedCost,
		TotalInputTokens:  reportedInput,
		TotalOutputTokens: reportedOutput,
		Model:             input.Model.ID,
		UpdatedAt:         now,
	})
	if costDelta > 0 {
		wd.UpsertDailyCost(sessionID, today, costDelta, inputDelta, outputDelta)
	}
}

func runStatusline(cmd *cobra.Command, args []string) error {
	if slFromHook {
		return runStatuslineFromHook(cmd)
	}
	if slSessionID == "" {
		return fmt.Errorf("either --session or --from-hook is required")
	}
	return runStatuslineDirect(cmd)
}

// runStatuslineFromHook reads stdin JSON and produces the complete statusline.
func runStatuslineFromHook(cmd *cobra.Command) error {
	// Read stdin
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("failed to read stdin: %w", err)
	}

	var input hookInput
	if err := json.Unmarshal(data, &input); err != nil {
		return fmt.Errorf("failed to parse stdin JSON: %w", err)
	}

	if input.SessionID == "" {
		return fmt.Errorf("no session_id in stdin")
	}

	// Brief writable connection for heartbeat + cost tracking, then read-only for rendering
	if wd, err := openDB(); err == nil {
		now := time.Now().UTC().Format(time.RFC3339)
		wd.BumpLastActive(input.SessionID, now)
		termType, termID, workspaceID := terminal.Detect()
		syncSessionMetadata(wd, input.SessionID, input.SessionName, claudePID(), termType, termID, workspaceID)
		recordCostSnapshot(wd, &input)
		wd.Close()
	}

	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	session, err := d.GetSession(input.SessionID)
	if err != nil || session == nil || session.Status == "archived" {
		fmt.Println("Session not registered with handler. Say hello to register.")
		return nil
	}

	isHandler := session.Role == "handler"

	// Compute cost display values
	trueCost := input.Cost.TotalCostUSD
	todayCost := 0.0
	if input.Cost.TotalCostUSD > 0 {
		if adj, err := d.GetTotalAdjustment(input.SessionID); err == nil {
			trueCost += adj
		}
		today := time.Now().UTC().Format("2006-01-02")
		if dc, err := d.GetDailyCostForSession(input.SessionID, today); err == nil && dc != nil {
			todayCost = dc.CostUSD
		}
	}

	// Launch parallel goroutines for expensive operations
	var gitStatus *gitpkg.Status
	var awaitingNames []string
	var wg sync.WaitGroup

	// Git status (only for non-handler sessions)
	if !isHandler {
		wg.Add(1)
		go func() {
			defer wg.Done()
			gitStatus = gitpkg.GetStatus(input.CWD)
		}()
	}

	// Peek scan for awaiting approval
	wg.Add(1)
	go func() {
		defer wg.Done()
		awaitingNames = scanAwaitingApproval(d, session.SessionID)
	}()

	// While goroutines run, fetch handler data (fast SQLite queries)
	cfg, _ := config.Read(config.DefaultPath())
	if cfg == nil {
		cfg = &config.Config{}
	}

	// Wait for parallel work
	wg.Wait()

	// Assemble output
	var err2 error
	if isHandler {
		err2 = renderHandlerStatusline(d, session, cfg, &input, trueCost, todayCost, awaitingNames)
	} else {
		err2 = renderWorkerStatusline(d, session, cfg, &input, trueCost, todayCost, gitStatus, awaitingNames)
	}
	if err2 != nil {
		return err2
	}

	// Debug output when debug mode is enabled in config
	if cfg.Debug {
		renderDebugInfo(d, session)
	}

	return nil
}

// runStatuslineDirect is the legacy --session mode for direct CLI use.
func runStatuslineDirect(cmd *cobra.Command) error {
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	session, err := d.GetSession(slSessionID)
	if err != nil || session == nil || session.Status == "archived" {
		fmt.Println("not registered")
		return nil
	}

	// Direct mode just outputs the handler-specific lines (no git, no model)
	cfg, _ := config.Read(config.DefaultPath())
	if cfg == nil {
		cfg = &config.Config{}
	}

	if session.Role == "handler" {
		awaitingNames := scanAwaitingApproval(d, session.SessionID)
		return renderHandlerStatusline(d, session, cfg, nil, 0.0, 0.0, awaitingNames)
	}
	return renderWorkerStatusline(d, session, cfg, nil, 0.0, 0.0, nil, nil)
}

// scanAwaitingApproval checks all peekable sessions for approval prompts.
// Returns display names of sessions needing input (excluding selfSessionID).
func scanAwaitingApproval(d *db.DB, selfSessionID string) []string {
	awaiting := findSessionsAwaitingApproval(d)
	var names []string
	for _, s := range awaiting {
		if s.SessionID == selfSessionID {
			continue
		}
		name := s.SessionName
		if name == "" {
			name = s.SessionID[:8]
		}
		names = append(names, name)
	}
	return names
}

// renderWorkerStatusline outputs the complete statusline for a regular session.
func renderWorkerStatusline(d *db.DB, session *db.Session, cfg *config.Config, input *hookInput, trueCost float64, todayCost float64, gs *gitpkg.Status, awaitingNames []string) error {
	// Line 1: Git status
	if gs != nil && gs.InGit {
		fmt.Println(formatGitLine(gs))
	}

	// Line 2: Model/context/cost
	if input != nil && input.Model.DisplayName != "" {
		fmt.Println(formatModelLine(input, trueCost, todayCost))
	}

	// Handler lines (inbox, inbox-mode, watching)
	unreadCount, unreadMsg := renderInboxLine(d, session, false)
	renderAutoDeliveredLine(d, session)
	renderInboxModeLine(session)
	renderWatchingLine(d, session, cfg, false)

	// Dispatch terminal notification
	dispatchNotification(session, unreadCount, unreadMsg)

	// Awaiting approval
	var shortcuts *CmuxShortcuts
	if session.TerminalType == "cmux" {
		shortcuts = GetCmuxShortcuts()
	}
	renderAwaitingLine(session, awaitingNames, shortcuts)

	// Footer
	if session.TerminalType == "cmux" {
		renderCmuxShortcutsLine(shortcuts)
	}
	fmt.Printf("%sUse %s/done%s%s before closing the session to log a summary%s\n",
		colorDim, colorCyan, colorReset, colorDim, colorReset)

	return nil
}

// renderHandlerStatusline outputs the complete statusline for a handler session.
func renderHandlerStatusline(d *db.DB, session *db.Session, cfg *config.Config, input *hookInput, trueCost float64, todayCost float64, awaitingNames []string) error {
	// Count active sessions
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
	fmt.Printf("%s[Handler]%s %sSessions%s: %d active, %d blocked %s· %s/handler%s %sto summarize all sessions%s\n",
		colorPurple, colorReset, "\033[1m", colorReset, activeCount, blockedCount, colorDim, colorCyan, colorReset, colorDim, colorReset)

	// Model line (if from hook)
	if input != nil && input.Model.DisplayName != "" {
		fmt.Println(formatModelLine(input, trueCost, todayCost))
	}

	// Aggregate cost line
	today := time.Now().UTC().Format("2006-01-02")
	now := time.Now().UTC()
	monthStart := fmt.Sprintf("%04d-%02d-01", now.Year(), now.Month())
	monthEnd := fmt.Sprintf("%04d-%02d-%02d", now.Year(), now.Month(), daysInMonth(now.Year(), now.Month()))

	lastMonth := now.AddDate(0, -1, 0)
	lastMonthStart := fmt.Sprintf("%04d-%02d-01", lastMonth.Year(), lastMonth.Month())
	lastMonthEnd := fmt.Sprintf("%04d-%02d-%02d", lastMonth.Year(), lastMonth.Month(), daysInMonth(lastMonth.Year(), lastMonth.Month()))

	todayTotal, _, _, _ := d.QueryTotalCost(today, today)
	monthTotal, _, _, _ := d.QueryTotalCost(monthStart, monthEnd)
	lastMonthTotal, _, _, _ := d.QueryTotalCost(lastMonthStart, lastMonthEnd)

	if todayTotal > 0 || monthTotal > 0 {
		lastMonthName := lastMonth.Month().String()[:3]
		fmt.Printf("%sCost (all sessions)%s: $%.2f today · $%.2f this month · $%.2f %s\n",
			colorBoldWhite, colorReset, todayTotal, monthTotal, lastMonthTotal, lastMonthName)
	}

	// Inbox (global for handler)
	unreadCount, unreadMsg := renderInboxLine(d, session, true)
	renderAutoDeliveredLine(d, session)
	renderInboxModeLine(session)
	renderWatchingLine(d, session, cfg, true)

	// Dispatch terminal notification
	dispatchNotification(session, unreadCount, unreadMsg)

	// Awaiting approval
	var shortcuts *CmuxShortcuts
	if session.TerminalType == "cmux" {
		shortcuts = GetCmuxShortcuts()
	}
	renderAwaitingLine(session, awaitingNames, shortcuts)

	// Footer (cmux shortcuts)
	if session.TerminalType == "cmux" {
		renderCmuxShortcutsLine(shortcuts)
	}

	return nil
}

func renderAwaitingLine(session *db.Session, awaitingNames []string, shortcuts *CmuxShortcuts) {
	if len(awaitingNames) == 0 {
		return
	}
	count := len(awaitingNames)
	label := "session"
	if count > 1 {
		label = "sessions"
	}
	if shortcuts != nil && shortcuts.SwitchToAwaiting != "" {
		fmt.Printf("%s%d other %s awaiting approval%s %s· %s%s%s to auto-switch%s\n",
			colorYellow, count, label, colorReset, colorDim, colorCyan, shortcuts.SwitchToAwaiting, colorReset+colorDim, colorReset)
	} else {
		fmt.Printf("%s%d other %s awaiting approval%s\n", colorYellow, count, label, colorReset)
	}
	nameList := formatNameList(awaitingNames, 5)
	fmt.Printf("%s  ↳ %s%s%s\n", colorDim, colorYellow, nameList, colorReset)
}

func renderCmuxShortcutsLine(shortcuts *CmuxShortcuts) {
	if shortcuts == nil {
		fmt.Printf("%sRun %shandler setup%s%s from within cmux to set up keyboard shortcuts%s\n",
			colorDim, colorCyan, colorReset, colorDim, colorReset)
		return
	}
	var parts []string
	if shortcuts.SwitchToSession != "" {
		parts = append(parts, fmt.Sprintf("%s%s%s to switch sessions", colorCyan, shortcuts.SwitchToSession, colorReset+colorDim))
	}
	if shortcuts.FocusBack != "" && shortcuts.FocusForward != "" {
		parts = append(parts, fmt.Sprintf("%s%s%s and %s%s%s for focus back and forward",
			colorCyan, shortcuts.FocusBack, colorReset+colorDim,
			colorCyan, shortcuts.FocusForward, colorReset+colorDim))
	}
	if len(parts) > 0 {
		fmt.Printf("%s%s%s\n", colorDim, strings.Join(parts, " · "), colorReset)
	}
}

// --- Shared rendering helpers ---

func formatGitLine(gs *gitpkg.Status) string {
	var parts []string

	// Branch name with rebase indicator
	if gs.Rebasing {
		parts = append(parts, fmt.Sprintf("%srebasing%s %s%s%s", colorYellow, colorReset, colorBoldWhite, gs.Branch, colorReset))
	} else if gs.Branch == gs.DefaultBranch {
		parts = append(parts, fmt.Sprintf("on %s%s%s", colorBoldWhite, gs.Branch, colorReset))
	} else {
		parts = append(parts, fmt.Sprintf("%s%s%s", colorBoldWhite, gs.Branch, colorReset))
	}

	// Ahead with committed stats
	if gs.Ahead > 0 {
		ahead := fmt.Sprintf("%s↑%d%s", colorGreen, gs.Ahead, colorReset)
		if gs.CommittedAdds > 0 || gs.CommittedDels > 0 {
			ahead += fmt.Sprintf(" (%s+%d%s %s−%d%s)", colorGreen, gs.CommittedAdds, colorReset, colorRed, gs.CommittedDels, colorReset)
		}
		parts = append(parts, ahead)
	}

	// Dirty/clean
	dirty := gs.Modified + gs.Untracked
	if dirty > 0 {
		var dirtyParts []string
		if gs.Modified > 0 {
			dirtyParts = append(dirtyParts, fmt.Sprintf("%s%d modified%s", colorYellow, gs.Modified, colorReset))
		}
		if gs.Untracked > 0 {
			dirtyParts = append(dirtyParts, fmt.Sprintf("%s%d untracked%s", colorYellow, gs.Untracked, colorReset))
		}
		dirtyStr := strings.Join(dirtyParts, ", ")
		if gs.UncommittedAdds > 0 || gs.UncommittedDels > 0 {
			dirtyStr += fmt.Sprintf(" (%s+%d%s %s−%d%s)", colorGreen, gs.UncommittedAdds, colorReset, colorRed, gs.UncommittedDels, colorReset)
		}
		parts = append(parts, dirtyStr)
	} else {
		parts = append(parts, fmt.Sprintf("%sclean%s", colorDimGreen, colorReset))
	}

	// Behind
	if gs.Behind > 0 {
		parts = append(parts, fmt.Sprintf("%s↓%d behind %s%s", colorDim, gs.Behind, gs.DefaultBranch, colorReset))
	}

	result := parts[0]
	if gs.Ahead > 0 && len(parts) > 1 {
		result += " " + parts[1]
		if len(parts) > 2 {
			result += " · " + strings.Join(parts[2:], " ")
		}
	} else if len(parts) > 1 {
		result += " · " + strings.Join(parts[1:], " ")
	}

	return result
}

func formatModelLine(input *hookInput, trueCost float64, todayCost float64) string {
	pct := input.ContextWindow.UsedPercentage
	filled := pct * 20 / 100
	empty := 20 - filled

	bar := strings.Repeat("▓", filled) + strings.Repeat("░", empty)

	barColor := colorGreen
	if pct >= 80 {
		barColor = colorRed
	} else if pct >= 50 {
		barColor = colorYellow
	}

	costStr := fmt.Sprintf("$%.2f", trueCost)
	if todayCost > 0 {
		costStr += fmt.Sprintf(" ($%.2f today)", todayCost)
	}

	return fmt.Sprintf("%s%s%s %s%s%s %d%% ctx %s· %s%s",
		colorClaudeOrange, input.Model.DisplayName, colorReset,
		barColor, bar, colorReset,
		pct,
		colorDim, costStr, colorReset)
}

func renderInboxLine(d *db.DB, session *db.Session, global bool) (int, string) {
	var unreadCount int
	var breakdown map[string]int
	var err error

	if global {
		unreadCount, breakdown, err = d.GlobalUnreadCountForSession(session.SessionID)
	} else {
		unreadCount, breakdown, err = d.UnreadCountForSession(session.SessionID)
	}
	if err != nil {
		return 0, ""
	}

	directCount, _ := d.DirectCountForSession(session.SessionID)

	var notifyMsg string
	if unreadCount == 0 {
		noMsgLabel := "No new messages"
		if global {
			noMsgLabel = "No new events"
		}
		fmt.Printf("%s/inbox%s: %s %s· %s%s/message%s%s to talk to other sessions%s",
			colorCyan, colorReset, noMsgLabel, colorDim, colorDim, colorCyan, colorReset, colorDim, colorReset)
	} else {
		var breakdownParts []string
		for eventType, count := range breakdown {
			breakdownParts = append(breakdownParts, fmt.Sprintf("%d %s", count, watcher.EventType(eventType).DisplayName()))
		}
		breakdownStr := ""
		if len(breakdownParts) > 0 {
			breakdownStr = fmt.Sprintf(" (%s)", strings.Join(breakdownParts, ", "))
		}
		fmt.Printf("%s/inbox%s: %s● %d unread%s%s", colorCyan, colorReset, colorYellow, unreadCount, colorReset, breakdownStr)
		notifyMsg = fmt.Sprintf("%d unread%s", unreadCount, breakdownStr)
	}

	if directCount > 0 {
		fmt.Printf(" %s·%s %s● %d direct%s", colorDim, colorReset, colorYellow, directCount, colorReset)
	}
	if unreadCount > 0 {
		fmt.Printf(" %s· %s/inbox-clear%s%s to dismiss%s", colorDim, colorCyan, colorReset, colorDim, colorReset)
	}
	fmt.Println()
	return unreadCount, notifyMsg
}

func renderAutoDeliveredLine(d *db.DB, session *db.Session) {
	if session.InboxMode != "auto" {
		return
	}
	var autoCount int
	var err error
	if session.Role == "handler" {
		autoCount, err = d.AutoDeliveredCountAll(session.SessionID)
	} else {
		autoCount, err = d.AutoDeliveredCount(session.SessionID)
	}
	if err != nil || autoCount == 0 {
		return
	}
	msgWord := "messages"
	if autoCount == 1 {
		msgWord = "message"
	}
	fmt.Printf("%s  ● %d %s auto-delivered%s %s· %s/catchup%s %sfor a summary%s\n",
		colorYellow, autoCount, msgWord, colorReset, colorDim, colorCyan, colorReset, colorDim, colorReset)
}

func renderInboxModeLine(session *db.Session) {
	rendered := ""
	for i, mode := range []string{"manual", "on-submit", "auto"} {
		if i > 0 {
			rendered += fmt.Sprintf("%s | %s", colorDim, colorReset)
		}
		if session.InboxMode == mode {
			rendered += fmt.Sprintf("%s%s%s", colorBoldGreen, mode, colorReset)
		} else {
			rendered += fmt.Sprintf("%s%s%s", colorDim, mode, colorReset)
		}
	}
	fmt.Printf("%s/inbox-mode%s: %s\n", colorCyan, colorReset, rendered)
}

func renderWatchingLine(d *db.DB, session *db.Session, cfg *config.Config, global bool) {
	var prCount, jiraCount int
	var unreadResources map[string]bool
	var breakdown map[string]int

	if global {
		// Count all subscriptions across all sessions
		allSubs, err := d.Query(`
			SELECT resource_type, COUNT(*) as count
			FROM subscriptions
			WHERE deleted_at IS NULL
			GROUP BY resource_type
		`)
		if err == nil {
			defer allSubs.Close()
			for allSubs.Next() {
				var rt string
				var count int
				allSubs.Scan(&rt, &count)
				if rt == "pr" {
					prCount += count
				} else if rt == "jira" {
					jiraCount += count
				}
			}
		}
	} else {
		subs, err := d.ListSubscriptions(session.SessionID, false)
		if err == nil {
			for _, sub := range subs {
				if sub.ResourceType == "pr" {
					prCount++
				} else if sub.ResourceType == "jira" {
					jiraCount++
				}
			}
		}
		_, breakdown, _ = d.UnreadCountForSession(session.SessionID)
		unreadResources, _ = d.UnreadResourcesForSession(session.SessionID)
	}

	// Determine which resource types have unread events
	prUnread, jiraUnread := false, false
	if !global {
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
				if prCount > 0 {
					prUnread = true
				}
				if jiraCount > 0 {
					jiraUnread = true
				}
			}
		}
	}

	// Build subscription summary
	subParts := []string{}
	if prCount > 0 {
		label := "1 PR"
		if prCount > 1 {
			label = fmt.Sprintf("%d PRs", prCount)
		}
		if prUnread {
			subParts = append(subParts, fmt.Sprintf("%s● %s%s", colorYellow, label, colorReset))
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
			subParts = append(subParts, fmt.Sprintf("%s● %s%s", colorYellow, label, colorReset))
		} else {
			subParts = append(subParts, label)
		}
	}

	subSummary := ""
	if len(subParts) == 0 {
		subSummary = fmt.Sprintf("%sno active subscriptions%s", colorDim, colorReset)
	} else {
		subSummary = strings.Join(subParts, ", ")
	}

	// Watcher status
	watcherStatus := ""
	var services []string
	for _, svc := range []string{"github", "jira"} {
		if cfg.IsServiceConfigured(svc) && watcher.IsInstalled(svc) {
			lastRun := watcher.LastRunTime(svc)
			ago := ""
			if lastRun != nil {
				ago = fmt.Sprintf(" (%s ago)", formatDuration(time.Since(*lastRun)))
			}
			if d.HasWatcherError(svc) {
				services = append(services, fmt.Sprintf("%s✗%s%s %s%s", colorRed, colorReset, colorDim, svc, ago))
			} else {
				services = append(services, fmt.Sprintf("%s✓%s%s %s%s", colorGreen, colorReset, colorDim, svc, ago))
			}
		}
	}
	if len(services) > 0 {
		watcherStatus = " · " + strings.Join(services, " ")
	}

	// Resource links (worker only)
	middleSegment := ""
	if !global {
		subs, _ := d.ListSubscriptions(session.SessionID, false)
		if len(subs) > 0 {
			sort.Slice(subs, func(i, j int) bool {
				iKey := subs[i].ResourceType + ":" + subs[i].ResourceID
				jKey := subs[j].ResourceType + ":" + subs[j].ResourceID
				iUnread, jUnread := unreadResources[iKey], unreadResources[jKey]
				if iUnread != jUnread {
					return iUnread
				}
				return false
			})
			var links []string
			for _, sub := range subs {
				label := shortResourceLabel(sub.ResourceType, sub.ResourceID)
				resKey := sub.ResourceType + ":" + sub.ResourceID
				hasUnread := unreadResources[resKey]
				url := ""
				if sub.ResourceURL != nil {
					url = *sub.ResourceURL
				}
				if url == "" {
					url = cfg.DefaultResourceURL(sub.ResourceType, sub.ResourceID)
				}
				linkColor := colorBlue
				if hasUnread {
					linkColor = colorYellow
				}
				if url != "" {
					links = append(links, fmt.Sprintf("%s%s\033]8;;%s\033\\%s\033]8;;\033\\%s", linkColor, colorUnderline, url, label, colorReset))
				} else {
					links = append(links, fmt.Sprintf("%s%s%s", linkColor, label, colorReset))
				}
			}
			resourceLinks := strings.Join(links, fmt.Sprintf("%s, %s", colorDim, colorReset))
			if len(subs) <= 2 {
				middleSegment = fmt.Sprintf(" %s| %s%s", colorDim, colorReset, resourceLinks)
			} else {
				// Will be on a separate line
				fmt.Printf("%s/watching%s: %s%s%s%s%s\n", colorCyan, colorReset, subSummary, middleSegment, colorDim, watcherStatus, colorReset)
				fmt.Printf("%s  ↳ %s%s\n", colorDim, colorReset, resourceLinks)
				return
			}
		} else {
			middleSegment = fmt.Sprintf(" %s· %s/watch%s%s to follow PRs or Jira issues%s", colorDim, colorCyan, colorReset, colorDim, colorReset)
		}
	}

	fmt.Printf("%s/watching%s: %s%s%s%s%s\n", colorCyan, colorReset, subSummary, middleSegment, colorDim, watcherStatus, colorReset)
}

// shortResourceLabel returns a compact label for a resource.
func shortResourceLabel(resourceType, resourceID string) string {
	if resourceType == "pr" {
		if idx := strings.LastIndex(resourceID, "#"); idx >= 0 {
			return resourceID[idx:]
		}
	}
	return resourceID
}

func formatNameList(names []string, max int) string {
	if len(names) <= max {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:max], ", ") + fmt.Sprintf(", +%d more", len(names)-max)
}

func renderDebugInfo(d *db.DB, session *db.Session) {
	dim := colorDim
	reset := colorReset

	cursor, _ := d.GetCursor(session.SessionID)

	peekable := "no"
	if session.TerminalType != "" && session.TerminalID != "" {
		peekable = "yes"
	}

	state := session.Status
	if session.Status == "active" {
		if !discover.IsSessionProcess(session.PID, session.SessionID) {
			state = "dead"
		}
	}

	fmt.Printf("%s—%s\n", dim, reset)
	fmt.Printf("%s[debug] id=%s name=%q state=%s pid=%d%s\n",
		dim, session.SessionID[:12], session.SessionName, state, session.PID, reset)
	fmt.Printf("%s[debug] terminal=%s:%s peekable=%s%s\n",
		dim, session.TerminalType, session.TerminalID, peekable, reset)
	fmt.Printf("%s[debug] workspace=%q id=%s%s\n",
		dim, session.CmuxWorkspaceName, session.CmuxWorkspaceID, reset)
	fmt.Printf("%s[debug] role=%s cursor=%s%s\n",
		dim, session.Role, cursor, reset)
	fmt.Printf("%s—%s\n", dim, reset)
}
