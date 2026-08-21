package watcher

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/mturley/agent-handler/db"
	watcherlib "github.com/mturley/watcher"
	wcfg "github.com/mturley/watcher/config"
	wdb "github.com/mturley/watcher/db"
	wgithub "github.com/mturley/watcher/github"
	wjira "github.com/mturley/watcher/jira"
	wslack "github.com/mturley/watcher/slack"
	"github.com/spf13/cobra"
)

var runResources string

func init() {
	WatcherCmd.AddCommand(runCmd)
	runCmd.Flags().StringVar(&runResources, "resources", "", "Comma-separated resource IDs for catch-up mode")
}

var runCmd = &cobra.Command{
	Use:   "run <name>",
	Short: "Run a watcher once (one-shot poll)",
	Long: `Run a watcher once to poll for new events.

Valid watchers: github, jira, slack

Use --resources to poll specific resources instead of all active subscriptions.
Example: handler watcher run github --resources "owner/repo#123,owner/repo#456"`,
	Args: cobra.ExactArgs(1),
	RunE: runWatcher,
}

func runWatcher(cmd *cobra.Command, args []string) error {
	name := strings.ToLower(args[0])

	// Validate watcher name
	if name != "github" && name != "jira" && name != "slack" {
		return fmt.Errorf("unknown watcher: %s (must be 'github', 'jira', or 'slack')", name)
	}

	// Load credentials from the watcher library config (auth.yaml).
	creds, err := wcfg.Load(wcfg.DefaultPath())
	if err != nil {
		return fmt.Errorf("failed to load watcher credentials: %w", err)
	}

	// Open database
	d, err := db.Open(db.DefaultPath())
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	// Determine resources to poll
	resourceType := serviceToResourceType(name)
	var resources []watcherlib.Resource
	if runResources != "" {
		// Parse comma-separated resource IDs (catch-up mode)
		for _, id := range strings.Split(runResources, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				resources = append(resources, watcherlib.Resource{
					Type: resourceType,
					ID:   id,
				})
			}
		}
	} else {
		// Get all active resources
		resources, err = wdb.ActiveResources(d.Conn(), resourceType)
		if err != nil {
			return fmt.Errorf("failed to get active resources: %w", err)
		}
	}

	if len(resources) == 0 {
		fmt.Printf("No resources to poll for %s watcher.\n", name)
		return nil
	}

	fmt.Printf("Polling %d resources for %s watcher...\n", len(resources), name)

	// Open watcher log
	logger := openLog(name)

	// Run watcher-specific poll via the library pollers.
	switch name {
	case "github":
		gh, err := creds.GitHub()
		if err != nil {
			return fmt.Errorf("github credentials not available: %w", err)
		}
		return wgithub.Poll(d.Conn(), gh.Token, resources, logger)
	case "jira":
		jc, err := creds.Jira()
		if err != nil {
			return fmt.Errorf("jira credentials not available: %w", err)
		}
		// BotUsernames and CustomFields are non-credential behavior settings,
		// sourced from the watcher library's config.yaml (best-effort; absent
		// file yields none). config.yaml is the single source of truth —
		// `handler watcher auth` and the migration both write custom fields
		// there, so the poller does not read them from auth.yaml.
		var botUsernames []string
		var customFields map[string]string
		if behaviorCfg, cfgErr := wcfg.LoadConfig(wcfg.ConfigDefaultPath()); cfgErr == nil {
			botUsernames = behaviorCfg.JiraBotUsernames()
			customFields = behaviorCfg.JiraCustomFields()
		}
		auth := wjira.JiraAuth{
			URL:          jc.Host,
			Email:        jc.Email,
			Token:        jc.Token,
			CustomFields: customFields,
			BotUsernames: botUsernames,
		}
		return wjira.Poll(d.Conn(), auth, resources, logger)
	case "slack":
		sc, err := creds.Slack()
		if err != nil {
			return fmt.Errorf("slack credentials not available: %w", err)
		}
		auth := wslack.SlackAuth{
			Token:           sc.Token,
			Cookie:          sc.Cookie,
			WorkspaceDomain: sc.WorkspaceDomain,
		}
		return wslack.Poll(d.Conn(), auth, resources, logger)
	default:
		return fmt.Errorf("unknown watcher: %s", name)
	}
}

// serviceToResourceType maps service names to resource types
func serviceToResourceType(service string) string {
	switch service {
	case "github":
		return "pr"
	case "jira":
		return "jira"
	case "slack":
		return "slack"
	default:
		return ""
	}
}

// openLog opens an append-only log file for the named watcher, matching the
// path used by `handler watcher logs`: ~/.agent-handler/data/logs/watcher-<name>.log.
// Falls back to stderr if the log file cannot be opened.
func openLog(name string) *log.Logger {
	logDir := filepath.Join(db.HandlerHome(), "data", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return log.New(os.Stderr, fmt.Sprintf("[%s] ", name), log.LstdFlags)
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("watcher-%s.log", name))
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return log.New(os.Stderr, fmt.Sprintf("[%s] ", name), log.LstdFlags)
	}
	return log.New(logFile, "", log.LstdFlags)
}
