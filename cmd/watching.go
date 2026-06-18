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
	rows, err := d.Conn().Query(`
		SELECT DISTINCT sub.resource_type, sub.resource_id, COALESCE(sub.resource_url, ''),
			GROUP_CONCAT(DISTINCT COALESCE(s.session_name, s.branch)) as sessions
		FROM subscriptions sub
		JOIN sessions s ON s.session_id = sub.session_id
		WHERE sub.deleted_at IS NULL AND s.status = 'active'
		GROUP BY sub.resource_type, sub.resource_id
		ORDER BY sub.resource_type, sub.resource_id
	`)
	if err != nil {
		return fmt.Errorf("failed to query subscriptions: %w", err)
	}
	defer rows.Close()

	type globalSub struct {
		ResourceType string `json:"resource_type"`
		ResourceID   string `json:"resource_id"`
		ResourceURL  string `json:"resource_url,omitempty"`
		Sessions     string `json:"sessions"`
	}

	var results []globalSub
	for rows.Next() {
		var gs globalSub
		rows.Scan(&gs.ResourceType, &gs.ResourceID, &gs.ResourceURL, &gs.Sessions)
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

	if len(results) == 0 {
		fmt.Println("No active subscriptions across any session")
	} else {
		fmt.Printf("Watched resources across all sessions (%d):\n\n", len(results))
		for _, gs := range results {
			fmt.Printf("  %s:%s\n", gs.ResourceType, gs.ResourceID)
			if gs.ResourceURL != "" {
				fmt.Printf("    URL: %s\n", gs.ResourceURL)
			}
			if gs.Sessions != "" {
				fmt.Printf("    Sessions: %s\n", gs.Sessions)
			}
			fmt.Println()
		}
	}

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
