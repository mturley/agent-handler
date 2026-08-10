package watcher

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mturley/agent-handler/config"
	wcfg "github.com/mturley/watcher/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func init() {
	WatcherCmd.AddCommand(authCmd)
}

var authCmd = &cobra.Command{
	Use:   "auth [service]",
	Short: "Configure authentication for external services",
	Long: `Configure authentication tokens for external services (GitHub, Jira).
Run without arguments to configure all services interactively.
Specify 'github' or 'jira' to configure a specific service.

Credentials are stored in the watcher library config at ~/.config/watcher/auth.yaml.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAuth,
}

func runAuth(cmd *cobra.Command, args []string) error {
	authPath := wcfg.DefaultPath()
	cfg, err := wcfg.Load(authPath)
	if err != nil {
		return fmt.Errorf("failed to load credentials: %w", err)
	}

	// Determine which services to configure
	services := []string{}
	if len(args) == 0 {
		services = []string{"github", "jira"}
	} else {
		service := strings.ToLower(args[0])
		if service != "github" && service != "jira" {
			return fmt.Errorf("unknown service: %s (must be 'github' or 'jira')", args[0])
		}
		services = []string{service}
	}

	reader := bufio.NewReader(os.Stdin)
	modified := false

	for _, service := range services {
		switch service {
		case "github":
			if changed, err := configureGitHub(reader, cfg); err != nil {
				return err
			} else if changed {
				modified = true
			}
		case "jira":
			if changed, err := configureJira(reader, cfg); err != nil {
				return err
			} else if changed {
				modified = true
			}
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

func configureGitHub(reader *bufio.Reader, cfg *wcfg.Config) (bool, error) {
	fmt.Println("\n=== GitHub Configuration ===")

	// Check if already configured
	if cfg.Services.GitHub != nil && cfg.Services.GitHub.Token != "" {
		fmt.Println("GitHub is already configured.")
		username, err := config.ValidateGitHubToken(cfg.Services.GitHub.Token)
		if err != nil {
			fmt.Printf("⚠ Token validation failed: %v\n", err)
			fmt.Print("Would you like to reconfigure? (y/N): ")
			response, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(response)) != "y" {
				return false, nil
			}
		} else {
			fmt.Printf("✓ Valid token for user: %s\n", username)
			return false, nil
		}
	}

	fmt.Println("Create a personal access token at: https://github.com/settings/tokens")
	fmt.Println("Required scopes: repo")
	fmt.Print("\nEnter GitHub token (or press Enter to skip): ")

	token, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read token: %w", err)
	}

	token = strings.TrimSpace(token)
	if token == "" {
		fmt.Println("Skipped GitHub configuration.")
		return false, nil
	}

	// Validate token
	username, err := config.ValidateGitHubToken(token)
	if err != nil {
		return false, fmt.Errorf("token validation failed: %w", err)
	}

	fmt.Printf("✓ Valid token for user: %s\n", username)

	// Save token
	if cfg.Services.GitHub == nil {
		cfg.Services.GitHub = &wcfg.GitHubConfig{}
	}
	cfg.Services.GitHub.Token = token

	return true, nil
}

func configureJira(reader *bufio.Reader, cfg *wcfg.Config) (bool, error) {
	fmt.Println("\n=== Jira Configuration ===")

	// Check if already configured
	if cfg.Services.Jira != nil && cfg.Services.Jira.Token != "" {
		fmt.Println("Jira is already configured.")
		displayName, err := config.ValidateJiraToken(cfg.Services.Jira.Host, cfg.Services.Jira.Email, cfg.Services.Jira.Token)
		if err != nil {
			fmt.Printf("⚠ Token validation failed: %v\n", err)
			fmt.Print("Would you like to reconfigure? (y/N): ")
			response, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(response)) != "y" {
				return false, nil
			}
		} else {
			fmt.Printf("✓ Valid credentials for: %s\n", displayName)
			fmt.Printf("  Host: %s\n", cfg.Services.Jira.Host)
			fmt.Printf("  Email: %s\n", cfg.Services.Jira.Email)
			return false, nil
		}
	}

	fmt.Println("Create an API token at: https://id.atlassian.com/manage-profile/security/api-tokens")
	fmt.Print("\nEnter Jira base URL (e.g., https://your-domain.atlassian.net) or press Enter to skip: ")

	url, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read URL: %w", err)
	}

	url = strings.TrimSpace(url)
	if url == "" {
		fmt.Println("Skipped Jira configuration.")
		return false, nil
	}

	// Remove trailing slash if present
	url = strings.TrimSuffix(url, "/")

	fmt.Print("Enter Jira email: ")
	email, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read email: %w", err)
	}
	email = strings.TrimSpace(email)

	fmt.Print("Enter Jira API token: ")
	token, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read token: %w", err)
	}
	token = strings.TrimSpace(token)

	// Validate credentials
	displayName, err := config.ValidateJiraToken(url, email, token)
	if err != nil {
		return false, fmt.Errorf("credential validation failed: %w", err)
	}

	fmt.Printf("✓ Valid credentials for: %s\n", displayName)

	// Save credentials to the library config shape. The Jira host is stored
	// under services.jira.host (per the watcher library spec).
	if cfg.Services.Jira == nil {
		cfg.Services.Jira = &wcfg.JiraConfig{}
	}
	cfg.Services.Jira.Host = url
	cfg.Services.Jira.Email = email
	cfg.Services.Jira.Token = token

	// Custom fields are a behavior setting, not a credential, so they live in
	// the library's separate config.yaml rather than here in auth.yaml. Seed
	// defaults there if config.yaml doesn't already have any configured.
	if err := seedDefaultJiraCustomFields(); err != nil {
		fmt.Printf("  note: failed to seed default custom fields into config.yaml (%v)\n", err)
	}

	return true, nil
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
