package watcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	wcfg "github.com/mturley/watcher/config"
	"github.com/mturley/watcher/credsetup"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func init() {
	WatcherCmd.AddCommand(authCmd)
}

var authCmd = &cobra.Command{
	Use:   "auth [service]",
	Short: "Configure authentication for external services",
	Long: `Configure authentication tokens for external services (GitHub, Jira, Slack).
Run without arguments to configure all services interactively.
Specify 'github', 'jira', or 'slack' to configure a specific service.

Credentials are stored in the watcher library config at ~/.config/watcher/auth.yaml.
Tests each configured service's credentials and prompts to repair them if
missing or rejected (see github.com/mturley/watcher/credsetup).`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAuth,
}

// capitalize upper-cases the first byte of s (services are plain ASCII
// lowercase names like "github"/"jira"/"slack"), used only for the section
// header printed above each service's auth flow.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// authServiceByName maps the auth command's lowercase service argument to
// the corresponding credsetup.Service constant.
var authServiceByName = map[string]credsetup.Service{
	"github": credsetup.GitHub,
	"jira":   credsetup.Jira,
	"slack":  credsetup.Slack,
}

func runAuth(cmd *cobra.Command, args []string) error {
	authPath := wcfg.DefaultPath()
	cfg, err := wcfg.Load(authPath)
	if err != nil {
		return fmt.Errorf("failed to load credentials: %w", err)
	}

	// Determine which services to configure
	services := []credsetup.Service{}
	if len(args) == 0 {
		services = []credsetup.Service{credsetup.GitHub, credsetup.Jira, credsetup.Slack}
	} else {
		svc, ok := authServiceByName[strings.ToLower(args[0])]
		if !ok {
			return fmt.Errorf("unknown service: %s (must be 'github', 'jira', or 'slack')", args[0])
		}
		services = []credsetup.Service{svc}
	}

	prompter := newAuthPrompter()
	modified := false
	jiraChanged := false

	for _, service := range services {
		fmt.Printf("\n=== %s ===\n", capitalize(string(service)))
		changed, err := credsetup.TestAndRepair(cfg, service, prompter)
		if err != nil {
			return fmt.Errorf("%s: %w", service, err)
		}
		if changed {
			modified = true
			if service == credsetup.Jira {
				jiraChanged = true
			}
		}
	}

	if jiraChanged {
		// Custom fields are a behavior setting, not a credential, so they
		// live in the library's separate config.yaml rather than here in
		// auth.yaml. Seed defaults there if config.yaml doesn't already
		// have any configured.
		if err := seedDefaultJiraCustomFields(); err != nil {
			fmt.Printf("  note: failed to seed default custom fields into config.yaml (%v)\n", err)
		}
	}

	if modified {
		if err := cfg.Save(authPath); err != nil {
			return fmt.Errorf("failed to write credentials: %w", err)
		}
		fmt.Printf("\nCredentials saved to %s\n", authPath)
	}

	return nil
}

// seedDefaultJiraCustomFields writes a default set of Jira custom field IDs
// into the watcher library's behavior config file (config.yaml) if it
// doesn't already have any configured. It never overwrites existing values.
func seedDefaultJiraCustomFields() error {
	behaviorPath := wcfg.ConfigDefaultPath()
	bcfg, err := wcfg.LoadConfig(behaviorPath)
	if err != nil {
		return err
	}

	if len(bcfg.JiraCustomFields()) > 0 {
		return nil
	}

	if bcfg.Jira == nil {
		bcfg.Jira = &wcfg.JiraBehavior{}
	}
	bcfg.Jira.CustomFields = map[string]string{
		"blocked":          "customfield_10517",
		"blocked_reason":   "customfield_10483",
		"epic_key":         "customfield_10014",
		"flagged":          "customfield_10021",
		"story_points":     "customfield_10028",
		"git_pull_request": "customfield_10875",
	}

	if err := writeWatcherBehaviorConfig(bcfg); err != nil {
		return err
	}

	fmt.Println("")
	fmt.Println("  Default custom fields have been added to your config. These are common")
	fmt.Println("  field IDs for Jira Cloud — adjust them under jira.custom_fields in")
	fmt.Println("  ~/.config/watcher/config.yaml if your instance uses different field IDs.")

	return nil
}

// writeWatcherBehaviorConfig marshals cf to YAML and writes it to the
// watcher library's behavior config path (wcfg.ConfigDefaultPath()),
// creating the parent directory if needed. The watcher library has no
// exported writer for *wcfg.ConfigFile (only LoadConfig), so this does the
// marshal/write itself. config.yaml holds no credentials, so it is written
// with 0o644 permissions (matching the library's convention of not
// enforcing perms on this file, unlike auth.yaml's 0o600).
//
// This mirrors writeWatcherBehaviorConfig in cmd/migrate_watcher.go — the
// two call sites live in different packages (watcher vs. cmd) and the
// watcher library itself isn't a place to add this, so the small helper is
// duplicated rather than factored into a new shared package.
func writeWatcherBehaviorConfig(cf *wcfg.ConfigFile) error {
	path := wcfg.ConfigDefaultPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}
	data, err := yaml.Marshal(cf)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write config %s: %w", path, err)
	}
	return nil
}
