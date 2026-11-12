package config

import (
	wcfg "github.com/mturley/watcher/config"
)

// ServiceConfiguredForWatching reports whether credentials for the given
// watcher service ("github"/"jira"/"slack") are present in handler's
// watcher-auth.yaml (see WatcherAuthPath), which is what the pollers
// (`handler watcher run`/`install`) read.
//
// Slack is a first-class watcher (alongside github/jira): this answers "are
// Slack credentials configured?" for the `handler watcher auth`/status/install
// flows and the slack poller.
func ServiceConfiguredForWatching(service string) bool {
	creds, err := wcfg.Load(WatcherAuthPath())
	if err != nil {
		return false
	}
	switch service {
	case "github":
		_, err := creds.GitHub()
		return err == nil
	case "jira":
		_, err := creds.Jira()
		return err == nil
	case "slack":
		_, err := creds.Slack()
		return err == nil
	default:
		return false
	}
}
