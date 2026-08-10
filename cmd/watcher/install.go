package watcher

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	watcherPkg "github.com/mturley/agent-handler/watcher"
	wcfg "github.com/mturley/watcher/config"
	"github.com/spf13/cobra"
)

// isServiceConfigured reports whether a service has credentials in the
// watcher library config (auth.yaml).
func isServiceConfigured(cfg *wcfg.Config, name string) bool {
	switch name {
	case "github":
		_, err := cfg.GitHub()
		return err == nil
	case "jira":
		_, err := cfg.Jira()
		return err == nil
	default:
		return false
	}
}

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
	authPath := wcfg.DefaultPath()
	cfg, err := wcfg.Load(authPath)
	if err != nil {
		cfg = &wcfg.Config{}
	}

	reader := bufio.NewReader(os.Stdin)
	ghChanged, _ := configureGitHub(reader, cfg)
	jiraChanged, _ := configureJira(reader, cfg)
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
