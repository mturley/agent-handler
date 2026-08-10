package cmd

import (
	"testing"

	wcfg "github.com/mturley/watcher/config"
)

func TestServiceConfiguredForWatching(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WATCHER_HOME", dir)

	// No auth.yaml written yet: nothing should be configured.
	if serviceConfiguredForWatching("github") {
		t.Error("expected github to be unconfigured before auth.yaml exists")
	}
	if serviceConfiguredForWatching("jira") {
		t.Error("expected jira to be unconfigured before auth.yaml exists")
	}
	if serviceConfiguredForWatching("bogus") {
		t.Error("expected unknown service to be unconfigured")
	}

	// Write auth.yaml with only GitHub credentials.
	creds := &wcfg.Config{
		Services: wcfg.Services{
			GitHub: &wcfg.GitHubConfig{Token: "gh-token"},
		},
	}
	if err := creds.Save(wcfg.DefaultPath()); err != nil {
		t.Fatalf("failed to save auth.yaml: %v", err)
	}

	if !serviceConfiguredForWatching("github") {
		t.Error("expected github to be configured after writing a token")
	}
	if serviceConfiguredForWatching("jira") {
		t.Error("expected jira to remain unconfigured (no jira block written)")
	}
	if serviceConfiguredForWatching("bogus") {
		t.Error("expected unknown service to remain unconfigured")
	}
}
