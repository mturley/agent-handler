package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mturley/agent-handler/config"
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
}

func runWatching(cmd *cobra.Command, args []string) error {
	sessionID, err := resolveSessionID(cmd)
	if err != nil {
		return fmt.Errorf("could not determine session: %w", err)
	}

	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	// Subscriptions
	subs, _ := d.ListSubscriptions(sessionID, false)

	// Watcher status
	cfg, _ := config.Read(config.DefaultPath())

	type watcherStatus struct {
		Name      string  `json:"name"`
		Configured bool   `json:"configured"`
		Installed  bool   `json:"installed"`
		Running    bool   `json:"running"`
		LastRun    string `json:"last_run,omitempty"`
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
		}
		watchers = append(watchers, ws)
	}

	// Recent errors (last 24 hours) for subscribed resources
	var recentErrors []struct {
		TS    string `json:"ts"`
		Title string `json:"title"`
		Body  string `json:"body,omitempty"`
	}

	if len(subs) > 0 {
		rows, err := d.Conn().Query(`
			SELECT DISTINCT e.ts, e.title, COALESCE(e.body, '')
			FROM events e
			JOIN event_resources er ON er.event_id = e.id
			JOIN subscriptions sub ON sub.resource_type = er.resource_type
				AND sub.resource_id = er.resource_id
				AND sub.session_id = ?
				AND sub.deleted_at IS NULL
			WHERE e.type = 'watcher_error'
				AND e.ts > datetime('now', '-24 hours')
			ORDER BY e.ts DESC
			LIMIT 10
		`, sessionID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var e struct {
					TS    string `json:"ts"`
					Title string `json:"title"`
					Body  string `json:"body,omitempty"`
				}
				rows.Scan(&e.TS, &e.Title, &e.Body)
				recentErrors = append(recentErrors, e)
			}
		}
	}

	if jsonOutput {
		output := map[string]interface{}{
			"subscriptions": subs,
			"watchers":      watchers,
			"recent_errors": recentErrors,
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
		if ws.LastRun != "" {
			if t, err := time.Parse(time.RFC3339, ws.LastRun); err == nil {
				lastRun = formatDuration(time.Since(t)) + " ago"
			}
		}
		state := "running"
		if !ws.Running {
			state = "stopped"
		}
		fmt.Printf("  %s: %s, last run %s\n", ws.Name, state, lastRun)
	}

	if len(recentErrors) > 0 {
		fmt.Printf("\nRecent errors (last 24h): %d\n", len(recentErrors))
		for _, e := range recentErrors {
			fmt.Printf("  [%s] %s\n", e.TS[:19], e.Title)
			if e.Body != "" {
				body := e.Body
				if len(body) > 120 {
					body = body[:120] + "..."
				}
				fmt.Printf("    %s\n", body)
			}
		}
	} else {
		fmt.Println("\nNo errors in the last 24 hours.")
	}

	return nil
}
