package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the agent-handler configuration
type Config struct {
	Services Services `yaml:"services"`
}

// Services contains configuration for external services
type Services struct {
	GitHub *GitHubConfig `yaml:"github,omitempty"`
	Jira   *JiraConfig   `yaml:"jira,omitempty"`
}

// GitHubConfig contains GitHub API configuration
type GitHubConfig struct {
	Token string `yaml:"token"`
}

// JiraConfig contains Jira API configuration
type JiraConfig struct {
	URL          string   `yaml:"url"`
	Email        string   `yaml:"email"`
	Token        string   `yaml:"token"`
	BotUsernames []string `yaml:"bot_usernames,omitempty"`
}

// DefaultPath returns the default configuration file path
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".agent-handler", "config.yaml")
}

// Read reads configuration from the specified path
// Returns an empty Config if the file doesn't exist (not an error)
func Read(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Write writes configuration to the specified path with 0600 permissions
// Creates parent directories if needed
func Write(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// IsServiceConfigured checks if a service has a non-empty token
func (c *Config) IsServiceConfigured(service string) bool {
	switch service {
	case "github":
		return c.Services.GitHub != nil && c.Services.GitHub.Token != ""
	case "jira":
		return c.Services.Jira != nil && c.Services.Jira.Token != ""
	default:
		return false
	}
}

// ResourceTypeToService maps resource types to service names
func ResourceTypeToService(resourceType string) string {
	switch resourceType {
	case "pr":
		return "github"
	case "jira":
		return "jira"
	default:
		return ""
	}
}
