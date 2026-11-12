package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	wcfg "github.com/mturley/watcher/config"
)

func TestReadWriteConfig(t *testing.T) {
	testPath := filepath.Join(t.TempDir(), "config.yaml")

	cfg := &Config{
		GitHub: &wcfg.GitHubBehavior{BotUsernames: []string{"*ci-bot*"}},
		Jira: &wcfg.JiraBehavior{
			BotUsernames: []string{"bot1", "bot2"},
			CustomFields: map[string]string{"epic_key": "customfield_10014"},
		},
	}
	if err := Write(testPath, cfg); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	info, err := os.Stat(testPath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("Expected file mode 0600, got %o", info.Mode().Perm())
	}

	got, err := Read(testPath)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if got.GitHub == nil || len(got.GitHub.BotUsernames) != 1 || got.GitHub.BotUsernames[0] != "*ci-bot*" {
		t.Errorf("GitHub behavior not round-tripped: %+v", got.GitHub)
	}
	if got.Jira == nil || len(got.Jira.BotUsernames) != 2 || got.Jira.BotUsernames[1] != "bot2" {
		t.Fatalf("Jira bot usernames not round-tripped: %+v", got.Jira)
	}
	if got.Jira.CustomFields["epic_key"] != "customfield_10014" {
		t.Errorf("custom fields not round-tripped: %v", got.Jira.CustomFields)
	}
}

func TestReadMissingFile(t *testing.T) {
	cfg, err := Read("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("Expected no error for missing file, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("Expected non-nil config")
	}
	if cfg.GitHub != nil || cfg.Jira != nil {
		t.Error("Expected nil behavior blocks for missing file")
	}
}

func TestWatcherAuthPathHonorsHandlerHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HANDLER_HOME", dir)
	if got, want := WatcherAuthPath(), filepath.Join(dir, "watcher-auth.yaml"); got != want {
		t.Errorf("WatcherAuthPath() = %q, want %q", got, want)
	}
}

func TestDefaultResourceURL(t *testing.T) {
	t.Setenv("HANDLER_HOME", t.TempDir())
	seed := &wcfg.Config{Services: wcfg.Services{Jira: &wcfg.JiraConfig{
		Host: "x.atlassian.net", Email: "e@example.com", Token: "t",
	}}}
	if err := seed.Save(WatcherAuthPath()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg := &Config{}

	tests := []struct {
		name         string
		resourceType string
		resourceID   string
		expected     string
	}{
		{"PR", "pr", "owner/repo#123", "https://github.com/owner/repo/pull/123"},
		{"PR with org", "pr", "my-org/my-repo#42", "https://github.com/my-org/my-repo/pull/42"},
		{"PR missing hash", "pr", "owner/repo", ""},
		{"PR empty number", "pr", "owner/repo#", ""},
		{"Jira", "jira", "PROJ-456", "https://x.atlassian.net/browse/PROJ-456"},
		{"Unknown type", "other", "chan-123", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cfg.DefaultResourceURL(tt.resourceType, tt.resourceID); got != tt.expected {
				t.Errorf("DefaultResourceURL(%s, %s) = %q, want %q", tt.resourceType, tt.resourceID, got, tt.expected)
			}
		})
	}
}

func TestDefaultResourceURLJiraUsesAuthHost(t *testing.T) {
	t.Setenv("HANDLER_HOME", t.TempDir())
	if got := (&Config{}).DefaultResourceURL("jira", "PROJ-1"); got != "" {
		t.Errorf("no auth file: got %q, want empty", got)
	}
	seed := &wcfg.Config{Services: wcfg.Services{Jira: &wcfg.JiraConfig{
		Host: "https://x.atlassian.net/", Email: "e@example.com", Token: "t",
	}}}
	if err := seed.Save(WatcherAuthPath()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if got, want := (&Config{}).DefaultResourceURL("jira", "PROJ-1"), "https://x.atlassian.net/browse/PROJ-1"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestJiraCustomFieldsConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `jira:
  custom_fields:
    epic_key: "customfield_10014"
    blocked: "customfield_10517"
    story_points: "customfield_10028"
`
	os.WriteFile(path, []byte(content), 0600)

	cfg, err := Read(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if cfg.Jira == nil || len(cfg.Jira.CustomFields) != 3 {
		t.Fatalf("expected 3 custom fields, got %+v", cfg.Jira)
	}
	if cfg.Jira.CustomFields["epic_key"] != "customfield_10014" {
		t.Errorf("epic_key = %q", cfg.Jira.CustomFields["epic_key"])
	}
}

func TestSeedJiraCustomFieldsPreservesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	input := `# my hand-written comment
services:
  github:
    token: t
auto_wake:
  enabled: false
`
	if err := os.WriteFile(path, []byte(input), 0640); err != nil {
		t.Fatal(err)
	}
	wrote, err := SeedJiraCustomFields(path, map[string]string{"epic_key": "customfield_10014"})
	if err != nil || !wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	data, _ := os.ReadFile(path)
	out := string(data)
	for _, line := range []string{"# my hand-written comment", "services:", "  github:", "    token: t", "auto_wake:", "  enabled: false"} {
		if !strings.Contains(out, line+"\n") {
			t.Errorf("line %q lost from:\n%s", line, out)
		}
	}
	if strings.Index(out, "services:") > strings.Index(out, "auto_wake:") {
		t.Errorf("key order changed:\n%s", out)
	}
	cfg, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Jira == nil || cfg.Jira.CustomFields["epic_key"] != "customfield_10014" {
		t.Errorf("jira.custom_fields not seeded: %+v", cfg.Jira)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0640 {
		t.Errorf("mode = %o, want 640", fi.Mode().Perm())
	}
}

func TestSeedJiraCustomFieldsKeepsExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	input := "jira:\n    custom_fields:\n        a: customfield_1 # keep\n"
	os.WriteFile(path, []byte(input), 0600)
	wrote, err := SeedJiraCustomFields(path, map[string]string{"b": "customfield_2"})
	if err != nil || wrote {
		t.Fatalf("wrote=%v err=%v, want false,nil", wrote, err)
	}
	if data, _ := os.ReadFile(path); string(data) != input {
		t.Errorf("file changed:\n%s", data)
	}
}

func TestSeedJiraCustomFieldsCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yaml")
	wrote, err := SeedJiraCustomFields(path, map[string]string{"a": "customfield_1"})
	if err != nil || !wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0600 {
		t.Errorf("stat=%v err=%v, want mode 600", fi, err)
	}
}

func TestSeedJiraCustomFieldsFillsEmptyBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("jira:\n    bot_usernames: [bot]\n    custom_fields: {}\n"), 0600)
	wrote, err := SeedJiraCustomFields(path, map[string]string{"a": "customfield_1"})
	if err != nil || !wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	cfg, _ := Read(path)
	if cfg.Jira.CustomFields["a"] != "customfield_1" || len(cfg.Jira.BotUsernames) != 1 {
		t.Errorf("unexpected: %+v", cfg.Jira)
	}
}

func TestDefaultResourceURL_Slack(t *testing.T) {
	t.Setenv("HANDLER_HOME", t.TempDir())
	seed := &wcfg.Config{Services: wcfg.Services{Slack: &wcfg.SlackConfig{
		Token: "xoxc-x", Cookie: "d", WorkspaceDomain: "myteam.slack.com",
	}}}
	if err := seed.Save(WatcherAuthPath()); err != nil {
		t.Fatalf("seed auth.yaml: %v", err)
	}
	c := &Config{}
	want := "https://myteam.slack.com/archives/C0123ABCD/p1699999999000100"
	if got := c.DefaultResourceURL("slack", "C0123ABCD:1699999999.000100"); got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if got := c.DefaultResourceURL("slack", "nocolon"); got != "" {
		t.Errorf("malformed: got %q want \"\"", got)
	}
}

func TestDefaultResourceURL_Slack_SchemeTolerated(t *testing.T) {
	t.Setenv("HANDLER_HOME", t.TempDir())
	seed := &wcfg.Config{Services: wcfg.Services{Slack: &wcfg.SlackConfig{
		Token: "x", Cookie: "d", WorkspaceDomain: "https://myteam.slack.com/",
	}}}
	if err := seed.Save(WatcherAuthPath()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	c := &Config{}
	want := "https://myteam.slack.com/archives/C0123ABCD/p1699999999000100"
	if got := c.DefaultResourceURL("slack", "C0123ABCD:1699999999.000100"); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestDefaultResourceURL_Slack_NoDomain(t *testing.T) {
	t.Setenv("HANDLER_HOME", t.TempDir())
	c := &Config{}
	if got := c.DefaultResourceURL("slack", "C0:1699999999.000100"); got != "" {
		t.Errorf("no domain: got %q want \"\"", got)
	}
}

func TestResourceTypeToService_Slack(t *testing.T) {
	if got := ResourceTypeToService("slack"); got != "slack" {
		t.Errorf("got %q want slack", got)
	}
}

func TestStatuslineShowContext(t *testing.T) {
	tr := true
	fa := false

	tests := []struct {
		name     string
		config   *Config
		expected bool
	}{
		{"nil statusline", &Config{}, true},
		{"nil show_context", &Config{Statusline: &StatuslineConfig{}}, true},
		{"explicit true", &Config{Statusline: &StatuslineConfig{ShowContext: &tr}}, true},
		{"explicit false", &Config{Statusline: &StatuslineConfig{ShowContext: &fa}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.StatuslineShowContext(); got != tt.expected {
				t.Errorf("StatuslineShowContext() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestStatuslineShowGit(t *testing.T) {
	tr := true
	fa := false

	tests := []struct {
		name     string
		config   *Config
		expected bool
	}{
		{"nil statusline", &Config{}, true},
		{"nil show_git", &Config{Statusline: &StatuslineConfig{}}, true},
		{"explicit true", &Config{Statusline: &StatuslineConfig{ShowGit: &tr}}, true},
		{"explicit false", &Config{Statusline: &StatuslineConfig{ShowGit: &fa}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.StatuslineShowGit(); got != tt.expected {
				t.Errorf("StatuslineShowGit() = %v, want %v", got, tt.expected)
			}
		})
	}
}
