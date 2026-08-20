package watcher

import (
	"fmt"
	"strings"
	"time"

	watcherPkg "github.com/mturley/agent-handler/watcher"
	wcfg "github.com/mturley/watcher/config"
	"github.com/mturley/watcher/credsetup"
	"github.com/spf13/cobra"
)

// isServiceConfigured reports whether a service has credentials in the
// watcher library config (auth.yaml). Slack is included here (and in
// ServiceConfiguredForWatching) so config-test callers can report Slack's
// status, but Slack is deliberately NOT in knownWatchers/defaultIntervals
// below: handler has no Slack poller yet (that's a later phase), so it must
// never be offered for `handler watcher install`/`run`.
func isServiceConfigured(cfg *wcfg.Config, name string) bool {
	switch name {
	case "github":
		_, err := cfg.GitHub()
		return err == nil
	case "jira":
		_, err := cfg.Jira()
		return err == nil
	case "slack":
		_, err := cfg.Slack()
		return err == nil
	default:
		return false
	}
}

// defaultIntervals covers only the services handler can actually poll
// (github, jira). Slack is intentionally excluded — see isServiceConfigured.
var defaultIntervals = map[string]time.Duration{
	"github": 3 * time.Minute,
	"jira":   5 * time.Minute,
}

var installCmd = &cobra.Command{
	Use:   "install [name]",
	Short: "Set up and install watchers",
	Long: `With no arguments: runs auth for all services, then installs watchers
for all authenticated services.

With a name argument: installs a specific watcher (must already be authenticated).`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInstallWatcher,
}

func init() {
	installCmd.Flags().Duration("interval", 0, "polling interval (e.g. 3m, 5m)")
	WatcherCmd.AddCommand(installCmd)
}

func runInstallWatcher(cmd *cobra.Command, args []string) error {
	if len(args) == 1 {
		return installSingle(cmd, args[0])
	}
	return installAll(cmd)
}

func installAll(cmd *cobra.Command) error {
	// Run auth first, writing credentials to the watcher library config.
	// Scoped to github+jira only, matching knownWatchers below — handler has
	// no Slack poller to install yet, so Slack isn't offered here (use
	// `handler watcher auth slack` to configure Slack credentials).
	authPath := wcfg.DefaultPath()
	cfg, err := wcfg.Load(authPath)
	if err != nil {
		cfg = &wcfg.Config{}
	}

	prompter := newAuthPrompter()
	ghChanged, _ := credsetup.TestAndRepair(cfg, credsetup.GitHub, prompter)
	jiraChanged, _ := credsetup.TestAndRepair(cfg, credsetup.Jira, prompter)
	if jiraChanged {
		if err := seedDefaultJiraCustomFields(); err != nil {
			fmt.Printf("  note: failed to seed default custom fields into config.yaml (%v)\n", err)
		}
	}
	if ghChanged || jiraChanged {
		if err := cfg.Save(authPath); err != nil {
			return fmt.Errorf("failed to write credentials: %w", err)
		}
	}

	// Re-read config after auth
	cfg, _ = wcfg.Load(authPath)

	// Install watchers for authenticated services
	installed := 0
	for _, name := range knownWatchers {
		if isServiceConfigured(cfg, name) {
			if watcherPkg.IsInstalled(name) {
				fmt.Printf("  ✓ %s watcher already installed\n", name)
				installed++
				continue
			}
			interval := defaultIntervals[name]
			if err := watcherPkg.Install(name, int(interval.Seconds())); err != nil {
				fmt.Printf("  ⚠ Failed to install %s watcher: %v\n", name, err)
				continue
			}
			fmt.Printf("  ✓ Installed %s watcher (polling every %s)\n", name, interval)
			installed++
		}
	}

	if installed == 0 {
		fmt.Println("\nNo services configured. Watchers not installed.")
	} else {
		fmt.Printf("\nTo check status: handler watcher list\n")
		fmt.Printf("To stop:         handler watcher stop\n")
	}

	return nil
}

func installSingle(cmd *cobra.Command, name string) error {
	name = strings.ToLower(name)
	valid := false
	for _, w := range knownWatchers {
		if w == name {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("unknown watcher: %s (valid: %s)", name, strings.Join(knownWatchers, ", "))
	}

	cfg, err := wcfg.Load(wcfg.DefaultPath())
	if err != nil {
		return fmt.Errorf("reading credentials: %w", err)
	}

	if !isServiceConfigured(cfg, name) {
		return fmt.Errorf("%s is not configured. Run 'handler watcher auth %s' first", name, name)
	}

	interval, _ := cmd.Flags().GetDuration("interval")
	if interval == 0 {
		interval = defaultIntervals[name]
	}

	if err := watcherPkg.Install(name, int(interval.Seconds())); err != nil {
		return fmt.Errorf("installing watcher: %w", err)
	}

	fmt.Printf("✓ Installed %s watcher (polling every %s)\n", name, interval)
	fmt.Printf("\nTo check status: handler watcher list\n")
	fmt.Printf("To run now:      handler watcher run %s\n", name)
	fmt.Printf("To stop:         handler watcher stop\n")

	return nil
}
