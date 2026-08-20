package config

import (
	wcfg "github.com/mturley/watcher/config"
)

// ServiceConfiguredForWatching reports whether credentials for the given
// watcher service ("github"/"jira"/"slack") are present in the shared
// watcher auth file (~/.config/watcher/auth.yaml), which is the source of
// truth the pollers (`handler watcher run`/`install`) actually read. This
// keeps "is this service configured for watching?" checks consistent across
// the codebase, regardless of whether the (now legacy) handler
// ~/.agent-handler/config.yaml has anything configured.
//
// Slack is included here even though handler has no Slack poller yet
// (that's a later phase) — this function answers "are Slack credentials
// configured?", which the `handler watcher auth`/status flows need
// regardless of poller support; it does not imply Slack can be watched.
func ServiceConfiguredForWatching(service string) bool {
	creds, err := wcfg.Load(wcfg.DefaultPath())
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
