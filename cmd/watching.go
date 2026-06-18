package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	watcherPkg "github.com/mturley/agent-handler/watcher"
	"github.com/spf13/cobra"
)

var watchingCmd = &cobra.Command{
	Use:   "watching",
	Short: "Show watched resources, watcher status, and recent errors",
	RunE:  runWatching,
}

func init() {
	watchingCmd.GroupID = "human"
	rootCmd.AddCommand(watchingCmd)
	watchingCmd.Flags().String("session-id", "", "session ID (auto-detected if omitted)")
	watchingCmd.Flags().Bool("global", false, "show all watched resources across all sessions")
}

func runWatching(cmd *cobra.Command, args []string) error {
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	global, _ := cmd.Flags().GetBool("global")
	if global {
		return runWatchingGlobal(d)
	}

	sessionID, err := resolveSessionID(cmd)
	if err != nil {
		return fmt.Errorf("could not determine session: %w", err)
	}

	// Subscriptions
	subs, _ := d.ListSubscriptions(sessionID, false)

	// Watcher status
	cfg, _ := config.Read(config.DefaultPath())

	type watcherStatus struct {
		Name             string `json:"name"`
		Configured       bool   `json:"configured"`
		Installed        bool   `json:"installed"`
		Running          bool   `json:"running"`
		LastRun          string `json:"last_run,omitempty"`
		NextRun          string `json:"next_run,omitempty"`
		IntervalSeconds  int    `json:"interval_seconds,omitempty"`
		HasError         bool   `json:"has_error"`
		LastErrorMessage string `json:"last_error_message,omitempty"`
	}

	var watchers []watcherStatus
	for _, name := range []string{"github", "jira"} {
		ws := watcherStatus{Name: name}
		if cfg != nil {
			ws.Configured = cfg.IsServiceConfigured(name)
		}
		ws.Installed = watcherPkg.IsInstalled(name)
		ws.Running = watcherPkg.IsRunning(name)
		if lastRun := watcherPkg.LastRunTime(name); lastRun != nil {
			ws.LastRun = lastRun.Format(time.RFC3339)
			interval := watcherPkg.InstalledInterval(name)
			if interval > 0 {
				ws.IntervalSeconds = interval
				nextRun := lastRun.Add(time.Duration(interval) * time.Second)
				ws.NextRun = nextRun.Format(time.RFC3339)
			}
		}
		ws.HasError = d.HasWatcherError(name)
		if ws.HasError {
			if dbStatus, err := d.GetWatcherStatus(name); err == nil && dbStatus != nil {
				ws.LastErrorMessage = dbStatus.LastErrorMessage
			}
		}
		watchers = append(watchers, ws)
	}

	if jsonOutput {
		output := map[string]interface{}{
			"subscriptions": subs,
			"watchers":      watchers,
		}
		data, _ := json.MarshalIndent(output, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	// Text output
	if len(subs) == 0 {
		fmt.Println("No resources are currently being watched in this session.")
	} else {
		fmt.Printf("Watched resources (%d):\n", len(subs))
		for _, sub := range subs {
			url := ""
			if sub.ResourceURL != nil {
				url = fmt.Sprintf("  %s", *sub.ResourceURL)
			}
			fmt.Printf("  %s:%s%s\n", sub.ResourceType, sub.ResourceID, url)
		}
	}

	fmt.Println()
	fmt.Println("Watchers:")
	for _, ws := range watchers {
		if !ws.Configured {
			fmt.Printf("  %s: not configured\n", ws.Name)
			continue
		}
		if !ws.Installed {
			fmt.Printf("  %s: configured but not installed\n", ws.Name)
			continue
		}
		lastRun := "never"
		nextRun := ""
		if ws.LastRun != "" {
			if t, err := time.Parse(time.RFC3339, ws.LastRun); err == nil {
				lastRun = formatDuration(time.Since(t)) + " ago"
			}
		}
		if ws.NextRun != "" {
			if t, err := time.Parse(time.RFC3339, ws.NextRun); err == nil {
				if t.After(time.Now()) {
					nextRun = fmt.Sprintf(", next: %s", formatDuration(time.Until(t)))
				} else {
					nextRun = ", next: any moment"
				}
			}
		}
		state := "running"
		if !ws.Running {
			state = "stopped"
		}
		if ws.HasError {
			fmt.Printf("  %s: %s, last run %s%s — ERROR\n", ws.Name, state, lastRun, nextRun)
			if ws.LastErrorMessage != "" {
				fmt.Printf("    %s\n", ws.LastErrorMessage)
			}
		} else {
			fmt.Printf("  %s: %s, last run %s%s\n", ws.Name, state, lastRun, nextRun)
		}
	}

	return nil
}

func runWatchingGlobal(d *db.DB) error {
	type sessionInfo struct {
		SessionID   string `json:"session_id"`
		SessionName string `json:"session_name,omitempty"`
		Branch      string `json:"branch"`
		LastActive  string `json:"last_active"`
	}

	type globalSub struct {
		ResourceType string        `json:"resource_type"`
		ResourceID   string        `json:"resource_id"`
		ResourceURL  string        `json:"resource_url,omitempty"`
		Sessions     []sessionInfo `json:"sessions"`
	}

	resRows, err := d.Conn().Query(`
		SELECT DISTINCT sub.resource_type, sub.resource_id, COALESCE(sub.resource_url, '')
		FROM subscriptions sub
		JOIN sessions s ON s.session_id = sub.session_id
		WHERE sub.deleted_at IS NULL AND s.status = 'active'
		ORDER BY sub.resource_type, sub.resource_id
	`)
	if err != nil {
		return fmt.Errorf("failed to query subscriptions: %w", err)
	}
	defer resRows.Close()

	var results []globalSub
	for resRows.Next() {
		var gs globalSub
		resRows.Scan(&gs.ResourceType, &gs.ResourceID, &gs.ResourceURL)

		sessRows, err := d.Conn().Query(`
			SELECT s.session_id, COALESCE(s.session_name, ''), s.branch, s.last_active
			FROM subscriptions sub
			JOIN sessions s ON s.session_id = sub.session_id
			WHERE sub.resource_type = ? AND sub.resource_id = ?
				AND sub.deleted_at IS NULL AND s.status = 'active'
			ORDER BY s.last_active DESC
		`, gs.ResourceType, gs.ResourceID)
		if err == nil {
			for sessRows.Next() {
				var si sessionInfo
				sessRows.Scan(&si.SessionID, &si.SessionName, &si.Branch, &si.LastActive)
				gs.Sessions = append(gs.Sessions, si)
			}
			sessRows.Close()
		}

		results = append(results, gs)
	}

	// Watcher status (same as per-session view)
	cfg, _ := config.Read(config.DefaultPath())
	type wsStatus struct {
		Name             string `json:"name"`
		Configured       bool   `json:"configured"`
		Installed        bool   `json:"installed"`
		Running          bool   `json:"running"`
		LastRun          string `json:"last_run,omitempty"`
		NextRun          string `json:"next_run,omitempty"`
		HasError         bool   `json:"has_error"`
		LastErrorMessage string `json:"last_error_message,omitempty"`
	}
	var watchers []wsStatus
	for _, name := range []string{"github", "jira"} {
		ws := wsStatus{Name: name}
		if cfg != nil {
			ws.Configured = cfg.IsServiceConfigured(name)
		}
		ws.Installed = watcherPkg.IsInstalled(name)
		ws.Running = watcherPkg.IsRunning(name)
		if lastRun := watcherPkg.LastRunTime(name); lastRun != nil {
			ws.LastRun = lastRun.Format(time.RFC3339)
			interval := watcherPkg.InstalledInterval(name)
			if interval > 0 {
				nextRun := lastRun.Add(time.Duration(interval) * time.Second)
				ws.NextRun = nextRun.Format(time.RFC3339)
			}
		}
		ws.HasError = d.HasWatcherError(name)
		if ws.HasError {
			if dbStatus, err := d.GetWatcherStatus(name); err == nil && dbStatus != nil {
				ws.LastErrorMessage = dbStatus.LastErrorMessage
			}
		}
		watchers = append(watchers, ws)
	}

	if jsonOutput {
		output := map[string]interface{}{
			"subscriptions": results,
			"watchers":      watchers,
		}
		data, _ := json.MarshalIndent(output, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	bold := "\033[1m"
	dim := "\033[2m"
	reset := "\033[0m"
	green := "\033[32m"
	red := "\033[31m"
	yellow := "\033[33m"

	if len(results) == 0 {
		fmt.Println("No active subscriptions across any session")
	} else {
		fmt.Printf("Watched resources across all sessions (%d):\n\n", len(results))
		for _, gs := range results {
			fmt.Printf("  %s%s:%s%s\n", bold, gs.ResourceType, gs.ResourceID, reset)
			if gs.ResourceURL != "" {
				fmt.Printf("  %s%s%s\n", dim, gs.ResourceURL, reset)
			}
			for _, si := range gs.Sessions {
				name := si.SessionName
				if name == "" {
					name = si.Branch
				}
				lastActive := si.LastActive
				if t, err := time.Parse(time.RFC3339, si.LastActive); err == nil {
					lastActive = formatDuration(time.Since(t)) + " ago"
				}
				fmt.Printf("  %s└ %s (%s) — %s%s\n", dim, name, si.SessionID[:12], lastActive, reset)
			}
			fmt.Println()
		}
	}

	fmt.Printf("%s─── Watchers ───%s\n", dim, reset)
	for _, ws := range watchers {
		if !ws.Configured {
			fmt.Printf("  %s%s✗%s %s%snot configured%s\n", ws.Name, red, reset, dim, " ", reset)
			continue
		}
		if !ws.Installed {
			fmt.Printf("  %s %s%s✗%s %s(not installed)%s\n", ws.Name, yellow, " ", reset, dim, reset)
			continue
		}
		lastRun := "never"
		nextRun := ""
		if ws.LastRun != "" {
			if t, err := time.Parse(time.RFC3339, ws.LastRun); err == nil {
				lastRun = formatDuration(time.Since(t)) + " ago"
			}
		}
		if ws.NextRun != "" {
			if t, err := time.Parse(time.RFC3339, ws.NextRun); err == nil {
				if t.After(time.Now()) {
					nextRun = fmt.Sprintf(", next: %s", formatDuration(time.Until(t)))
				} else {
					nextRun = ", next: any moment"
				}
			}
		}
		if ws.HasError {
			fmt.Printf("  %s✗%s %s %s(last run: %s%s)%s\n", red, reset, ws.Name, dim, lastRun, nextRun, reset)
			if ws.LastErrorMessage != "" {
				fmt.Printf("  %s  %s%s\n", dim, ws.LastErrorMessage, reset)
			}
		} else if !ws.Running {
			fmt.Printf("  %s⏸%s %s %s(stopped, last run: %s)%s\n", yellow, reset, ws.Name, dim, lastRun, reset)
		} else {
			fmt.Printf("  %s✓%s %s %s(last run: %s%s)%s\n", green, reset, ws.Name, dim, lastRun, nextRun, reset)
		}
	}

	return nil
}
