package watcher

import (
	"path/filepath"
	"testing"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	wcfg "github.com/mturley/watcher/config"
	wdb "github.com/mturley/watcher/db"
)

func TestKnownWatchersIncludesSlack(t *testing.T) {
	found := false
	for _, w := range knownWatchers {
		if w == "slack" {
			found = true
		}
	}
	if !found {
		t.Errorf("knownWatchers missing slack: %v", knownWatchers)
	}
}

func TestServiceToResourceType_Slack(t *testing.T) {
	if got := serviceToResourceType("slack"); got != "slack" {
		t.Errorf("got %q want slack", got)
	}
}

// A run that finds nothing to poll is a successful run, not a silent no-op:
// if it leaves last_success frozen, an error from a previous run outlives the
// subscription that caused it and the watcher shows ✗ error forever.
func TestRecordIdleRunClearsStaleError(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	if err := wdb.RecordPollerError(d.Conn(), "github", "Could not resolve to a Repository"); err != nil {
		t.Fatalf("record error: %v", err)
	}
	if !wdb.HasPollerError(d.Conn(), "github") {
		t.Fatalf("precondition: expected poller to be in error state")
	}

	if err := recordIdleRun(d.Conn(), "github"); err != nil {
		t.Fatalf("recordIdleRun: %v", err)
	}

	if wdb.HasPollerError(d.Conn(), "github") {
		t.Errorf("poller still reports an error after a clean run with no resources")
	}
}

func TestJiraAuthForReadsPrefsFromHandlerConfig(t *testing.T) {
	creds := wcfg.JiraCreds{Host: "https://example.atlassian.net", Email: "me@example.com", Token: "tok"}
	cfg := &config.Config{Jira: &wcfg.JiraBehavior{
		BotUsernames: []string{"ci-bot"},
		CustomFields: map[string]string{"epic_key": "customfield_10014"},
	}}
	auth := jiraAuthFor(creds, cfg)
	if auth.URL != creds.Host || auth.Email != creds.Email || auth.Token != creds.Token {
		t.Errorf("credentials not carried over: %+v", auth)
	}
	if len(auth.BotUsernames) != 1 || auth.BotUsernames[0] != "ci-bot" {
		t.Errorf("bot usernames = %v", auth.BotUsernames)
	}
	if auth.CustomFields["epic_key"] != "customfield_10014" {
		t.Errorf("custom fields = %v", auth.CustomFields)
	}
	// No jira block: still a usable auth with no prefs.
	if got := jiraAuthFor(creds, &config.Config{}); got.URL != creds.Host || got.CustomFields != nil {
		t.Errorf("empty config: %+v", got)
	}
}

func TestGitHubOptionsForReadsBotUsernames(t *testing.T) {
	cfg := &config.Config{GitHub: &wcfg.GitHubBehavior{BotUsernames: []string{"*ci*"}}}
	if got := githubOptionsFor(cfg).BotUsernames; len(got) != 1 || got[0] != "*ci*" {
		t.Errorf("bot usernames = %v", got)
	}
	if got := githubOptionsFor(&config.Config{}).BotUsernames; got != nil {
		t.Errorf("want none, got %v", got)
	}
}

func TestSeedDefaultJiraCustomFieldsWritesHandlerConfig(t *testing.T) {
	t.Setenv("HANDLER_HOME", t.TempDir())
	if err := seedDefaultJiraCustomFields(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Read(config.DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Jira == nil || cfg.Jira.CustomFields["epic_key"] != "customfield_10014" {
		t.Errorf("jira = %+v", cfg.Jira)
	}
}
