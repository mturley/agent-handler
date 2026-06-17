package watcher

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mturley/agent-handler/config"
	"github.com/spf13/cobra"
)

func init() {
	WatcherCmd.AddCommand(authCmd)
}

var authCmd = &cobra.Command{
	Use:   "auth [service]",
	Short: "Configure authentication for external services",
	Long: `Configure authentication tokens for external services (GitHub, Jira).
Run without arguments to configure all services interactively.
Specify 'github' or 'jira' to configure a specific service.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAuth,
}

func runAuth(cmd *cobra.Command, args []string) error {
	configPath := config.DefaultPath()
	cfg, err := config.Read(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
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
		if err := config.Write(configPath, cfg); err != nil {
			return fmt.Errorf("failed to write config: %w", err)
		}
		fmt.Println("\nConfiguration saved.")
	}

	return nil
}

func configureGitHub(reader *bufio.Reader, cfg *config.Config) (bool, error) {
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
		cfg.Services.GitHub = &config.GitHubConfig{}
	}
	cfg.Services.GitHub.Token = token

	return true, nil
}

func configureJira(reader *bufio.Reader, cfg *config.Config) (bool, error) {
	fmt.Println("\n=== Jira Configuration ===")

	// Check if already configured
	if cfg.Services.Jira != nil && cfg.Services.Jira.Token != "" {
		fmt.Println("Jira is already configured.")
		displayName, err := config.ValidateJiraToken(cfg.Services.Jira.URL, cfg.Services.Jira.Email, cfg.Services.Jira.Token)
		if err != nil {
			fmt.Printf("⚠ Token validation failed: %v\n", err)
			fmt.Print("Would you like to reconfigure? (y/N): ")
			response, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(response)) != "y" {
				return false, nil
			}
		} else {
			fmt.Printf("✓ Valid credentials for: %s\n", displayName)
			fmt.Printf("  URL: %s\n", cfg.Services.Jira.URL)
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

	// Save credentials
	if cfg.Services.Jira == nil {
		cfg.Services.Jira = &config.JiraConfig{}
	}
	cfg.Services.Jira.URL = url
	cfg.Services.Jira.Email = email
	cfg.Services.Jira.Token = token

	return true, nil
}
