package config

import (
	"os"
	"path/filepath"
	"strings"

	wcfg "github.com/mturley/watcher/config"
	"gopkg.in/yaml.v3"
)

// Config represents the agent-handler configuration
type Config struct {
	Services     Services            `yaml:"services"`
	Statusline   *StatuslineConfig   `yaml:"statusline,omitempty"`
	Experimental *ExperimentalConfig `yaml:"experimental,omitempty"`
	Reminders    *RemindersConfig    `yaml:"reminders,omitempty"`
	AutoWake     *AutoWakeConfig     `yaml:"auto_wake,omitempty"`
	Debug        bool                `yaml:"debug,omitempty"`
}

// AutoWakeConfig controls automatic rate-limit wake jobs: when a session's
// 5-hour rate limit usage crosses the threshold, hooks ask the session to
// schedule a one-shot cron job that resumes its work after the limit resets.
type AutoWakeConfig struct {
	Enabled          *bool `yaml:"enabled,omitempty"`
	ThresholdPercent *int  `yaml:"threshold_percent,omitempty"`
}

// ExperimentalConfig contains flags for experimental features
type ExperimentalConfig struct {
	CostDisplay *bool `yaml:"cost_display,omitempty"`
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
	URL          string            `yaml:"url"`
	Email        string            `yaml:"email"`
	Token        string            `yaml:"token"`
	BotUsernames []string          `yaml:"bot_usernames,omitempty"`
	CustomFields map[string]string `yaml:"custom_fields,omitempty"`
}

// StatuslineConfig controls which extra lines appear in the statusline
type StatuslineConfig struct {
	ShowContext *bool `yaml:"show_context,omitempty"`
	ShowGit     *bool `yaml:"show_git,omitempty"`
}

// ExperimentalCostDisplay returns whether enhanced cost display is enabled (default true).
// When enabled, the statusline shows true session cost (with reset adjustment) and today's spend,
// and the handler session shows aggregate cost across all sessions.
// When disabled, the statusline shows the raw cost from Claude Code's stdin.
// Cost data is always recorded to the database regardless of this setting.
func (c *Config) ExperimentalCostDisplay() bool {
	if c.Experimental == nil || c.Experimental.CostDisplay == nil {
		return true
	}
	return *c.Experimental.CostDisplay
}

// StatuslineShowContext returns whether the model/context/cost line is shown (default true)
func (c *Config) StatuslineShowContext() bool {
	return c.Statusline == nil || c.Statusline.ShowContext == nil || *c.Statusline.ShowContext
}

// StatuslineShowGit returns whether the git status line is shown (default true)
func (c *Config) StatuslineShowGit() bool {
	return c.Statusline == nil || c.Statusline.ShowGit == nil || *c.Statusline.ShowGit
}

// DefaultPath returns the default configuration file path
// Respects HANDLER_HOME env var for testing
func DefaultPath() string {
	if dir := os.Getenv("HANDLER_HOME"); dir != "" {
		return filepath.Join(dir, "config.yaml")
	}
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
	case "slack":
		return "slack"
	default:
		return ""
	}
}

// DefaultResourceURL constructs a URL for a resource from its type and ID.
// For PRs, resourceID is "owner/repo#123" → "https://github.com/owner/repo/pull/123".
// For Jira, resourceID is "PROJECT-123" → "{jira_base_url}/browse/PROJECT-123".
// Returns empty string if the URL cannot be constructed.
func (c *Config) DefaultResourceURL(resourceType, resourceID string) string {
	switch resourceType {
	case "pr":
		return prResourceURL(resourceID)
	case "jira":
		if c.Services.Jira != nil && c.Services.Jira.URL != "" {
			return strings.TrimRight(c.Services.Jira.URL, "/") + "/browse/" + resourceID
		}
		return ""
	case "slack":
		return slackResourceURL(resourceID)
	default:
		return ""
	}
}

// prResourceURL converts "owner/repo#123" to "https://github.com/owner/repo/pull/123"
func prResourceURL(resourceID string) string {
	idx := strings.LastIndex(resourceID, "#")
	if idx < 0 {
		return ""
	}
	repo := resourceID[:idx]
	num := resourceID[idx+1:]
	if repo == "" || num == "" {
		return ""
	}
	return "https://github.com/" + repo + "/pull/" + num
}

// slackResourceURL builds a Slack archive permalink from a "<channel>:<ts>"
// resource ID and the WorkspaceDomain in the shared watcher auth.yaml (the
// source of truth for Slack config — handler's own config has no Slack
// section). Returns "" if the domain is unset/unreadable or the ID is
// malformed.
func slackResourceURL(resourceID string) string {
	parts := strings.SplitN(resourceID, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	sc, err := wcfg.Load(wcfg.DefaultPath())
	if err != nil {
		return ""
	}
	creds, err := sc.Slack()
	if err != nil || creds.WorkspaceDomain == "" {
		return ""
	}
	// Tolerate a stored domain with a scheme and/or trailing slash.
	domain := strings.TrimPrefix(creds.WorkspaceDomain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimRight(domain, "/")

	channel, ts := parts[0], parts[1]
	tsDigits := strings.ReplaceAll(ts, ".", "")
	return "https://" + domain + "/archives/" + channel + "/p" + tsDigits
}

// AutoWakeOnRateLimit reports whether automatic rate-limit wake jobs are
// enabled (default true). When disabled, every wake hook path is a no-op.
func (c *Config) AutoWakeOnRateLimit() bool {
	if c.AutoWake == nil || c.AutoWake.Enabled == nil {
		return true
	}
	return *c.AutoWake.Enabled
}

// defaultAutoWakeThreshold is the 5h usage percentage at which a session is
// asked to schedule a wake job. High enough to avoid scheduling for limits that
// are never reached, low enough to leave budget for the CronCreate call itself.
const defaultAutoWakeThreshold = 90

// AutoWakeThresholdPercent returns the 5h usage percentage that triggers a wake
// job (default 90). Values outside 1-100 are nonsense — they would either never
// fire or fire constantly — so they fall back to the default.
func (c *Config) AutoWakeThresholdPercent() int {
	if c.AutoWake == nil || c.AutoWake.ThresholdPercent == nil {
		return defaultAutoWakeThreshold
	}
	v := *c.AutoWake.ThresholdPercent
	if v < 1 || v > 100 {
		return defaultAutoWakeThreshold
	}
	return v
}
