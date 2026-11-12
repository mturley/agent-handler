package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	wcfg "github.com/mturley/watcher/config"
	"gopkg.in/yaml.v3"
)

// Config represents the agent-handler configuration
type Config struct {
	GitHub       *wcfg.GitHubBehavior `yaml:"github,omitempty"`
	Jira         *wcfg.JiraBehavior   `yaml:"jira,omitempty"`
	Statusline   *StatuslineConfig    `yaml:"statusline,omitempty"`
	Experimental *ExperimentalConfig  `yaml:"experimental,omitempty"`
	Reminders    *RemindersConfig     `yaml:"reminders,omitempty"`
	AutoWake     *AutoWakeConfig      `yaml:"auto_wake,omitempty"`
	Debug        bool                 `yaml:"debug,omitempty"`
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

// WatcherAuthPath returns the path of handler's watcher credentials file,
// next to handler's config.yaml (so it honours HANDLER_HOME).
func WatcherAuthPath() string {
	return filepath.Join(filepath.Dir(DefaultPath()), "watcher-auth.yaml")
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
		return jiraResourceURL(resourceID)
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

// jiraResourceURL builds a Jira browse URL from the host in the watcher auth
// file. Returns "" if Jira is not configured there.
func jiraResourceURL(resourceID string) string {
	ac, err := wcfg.Load(WatcherAuthPath())
	if err != nil {
		return ""
	}
	creds, err := ac.Jira()
	if err != nil || creds.Host == "" {
		return ""
	}
	host := strings.TrimPrefix(creds.Host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimRight(host, "/")
	return "https://" + host + "/browse/" + resourceID
}

// slackResourceURL builds a Slack archive permalink from a "<channel>:<ts>"
// resource ID and the WorkspaceDomain in handler's watcher-auth.yaml. Returns "" if the domain is unset/unreadable or the ID is
// malformed.
func slackResourceURL(resourceID string) string {
	parts := strings.SplitN(resourceID, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	sc, err := wcfg.Load(WatcherAuthPath())
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

// SeedJiraCustomFields sets jira.custom_fields in the YAML file at path, but
// only when that key is absent or empty. It edits the parsed node tree so
// comments, key order, unknown keys and the file's mode are left alone; a
// missing file is created with mode 0600. It reports whether it wrote.
func SeedJiraCustomFields(path string, fields map[string]string) (bool, error) {
	if len(fields) == 0 {
		return false, nil
	}
	mode := os.FileMode(0600)
	var doc yaml.Node
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if fi, serr := os.Stat(path); serr == nil {
			mode = fi.Mode().Perm()
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return false, err
		}
	case os.IsNotExist(err):
	default:
		return false, err
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode}
	}
	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return false, fmt.Errorf("%s: top level is not a mapping", path)
	}
	jira := mapValue(root, "jira")
	if jira == nil {
		jira = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, scalarNode("jira"), jira)
	} else if jira.Kind == yaml.ScalarNode && jira.Tag == "!!null" {
		*jira = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	} else if jira.Kind != yaml.MappingNode {
		return false, fmt.Errorf("%s: jira is not a mapping", path)
	}
	if cf := mapValue(jira, "custom_fields"); cf != nil && len(cf.Content) > 0 {
		return false, nil
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	cf := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, k := range keys {
		cf.Content = append(cf.Content, scalarNode(k), scalarNode(fields[k]))
	}
	if existing := mapValue(jira, "custom_fields"); existing != nil {
		*existing = *cf
	} else {
		jira.Content = append(jira.Content, scalarNode("custom_fields"), cf)
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, out, mode); err != nil {
		return false, err
	}
	return true, nil
}

func mapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func scalarNode(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}
