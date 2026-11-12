package config

import (
	"testing"

	wcfg "github.com/mturley/watcher/config"
)

func TestServiceConfiguredReadsHandlerWatcherAuth(t *testing.T) {
	t.Setenv("HANDLER_HOME", t.TempDir())

	for _, svc := range []string{"github", "jira", "slack", "bogus"} {
		if ServiceConfiguredForWatching(svc) {
			t.Errorf("%s configured with no watcher-auth.yaml", svc)
		}
	}

	check := func(auth wcfg.Services, want map[string]bool) {
		t.Helper()
		if err := (&wcfg.Config{Services: auth}).Save(WatcherAuthPath()); err != nil {
			t.Fatalf("save: %v", err)
		}
		for svc, w := range want {
			if got := ServiceConfiguredForWatching(svc); got != w {
				t.Errorf("ServiceConfiguredForWatching(%q) = %v, want %v", svc, got, w)
			}
		}
	}
	check(wcfg.Services{GitHub: &wcfg.GitHubConfig{Token: "gh"}},
		map[string]bool{"github": true, "jira": false, "slack": false, "bogus": false})
	check(wcfg.Services{Jira: &wcfg.JiraConfig{Host: "x.atlassian.net", Email: "e@example.com", Token: "t"}},
		map[string]bool{"github": false, "jira": true, "slack": false})
	check(wcfg.Services{Slack: &wcfg.SlackConfig{Token: "xoxc-x", Cookie: "d", WorkspaceDomain: "t.slack.com"}},
		map[string]bool{"github": false, "jira": false, "slack": true})
}
