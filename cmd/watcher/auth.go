package watcher

import (
	"fmt"
	"strings"

	"github.com/mturley/agent-handler/config"
	wcfg "github.com/mturley/watcher/config"
	"github.com/mturley/watcher/credsetup"
	"github.com/spf13/cobra"
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

Credentials are stored in ~/.agent-handler/watcher-auth.yaml.
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
	authPath := config.WatcherAuthPath()
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
		// live in handler's config.yaml. Seed defaults there if none are
		// configured.
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
// under jira.custom_fields in handler's config.yaml if none are configured
// there. It never overwrites existing values.
func seedDefaultJiraCustomFields() error {
	defaults := map[string]string{
		"blocked":          "customfield_10517",
		"blocked_reason":   "customfield_10483",
		"epic_key":         "customfield_10014",
		"flagged":          "customfield_10021",
		"story_points":     "customfield_10028",
		"git_pull_request": "customfield_10875",
	}
	wrote, err := config.SeedJiraCustomFields(config.DefaultPath(), defaults)
	if err != nil {
		return err
	}
	if !wrote {
		return nil
	}

	fmt.Println("")
	fmt.Println("  Default custom fields have been added to your config. These are common")
	fmt.Println("  field IDs for Jira Cloud — adjust them under jira.custom_fields in")
	fmt.Println("  ~/.agent-handler/config.yaml if your instance uses different field IDs.")

	return nil
}
