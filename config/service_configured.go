package config

import (
	wcfg "github.com/mturley/watcher/config"
)

// ServiceConfiguredForWatching reports whether credentials for the given
// watcher service ("github"/"jira") are present in the shared watcher auth
// file (~/.config/watcher/auth.yaml), which is the source of truth the
// pollers (`handler watcher run`/`install`) actually read. This keeps "is
// this service configured for watching?" checks consistent across the
// codebase, regardless of whether the (now legacy) handler
// ~/.agent-handler/config.yaml has anything configured.
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
	default:
		return false
	}
}
