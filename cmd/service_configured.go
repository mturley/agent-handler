package cmd

import (
	wcfg "github.com/mturley/watcher/config"
)

// serviceConfiguredForWatching reports whether credentials for the given
// watcher service ("github"/"jira") are present in the shared watcher auth
// file (~/.config/watcher/auth.yaml), which is the source of truth the
// pollers use. This keeps "is this service configured for watching?"
// checks consistent with what `handler watcher run`/`install` actually
// read, regardless of whether the (now legacy) ~/.agent-handler/config.yaml
// has anything configured.
func serviceConfiguredForWatching(service string) bool {
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
