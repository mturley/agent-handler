package cmd

import (
	watcherlib "github.com/mturley/watcher"
)

// handlerEventDisplayNames maps agent-handler's OWN event types (those not
// owned by the watcher library) to human-readable labels for the statusline
// inbox breakdown. Watcher event types (pr_*, jira_*, ci_*, watch_started,
// watcher_error) are handled by the library's EventType.DisplayName instead.
//
// These entries were recovered from handler's former watcher/event_types.go
// eventTypeDisplayNames map, which was removed when the poller subsystem
// migrated to the watcher library.
var handlerEventDisplayNames = map[string]string{
	"status":    "status",
	"message":   "message",
	"reminder":  "reminder",
	"blocked":   "blocked",
	"unblocked": "unblocked",
	"milestone": "milestone",
	"decision":  "decision",
	"followup":  "followup",
	"handoff":   "handoff",
}

// eventDisplayName returns a human-readable label for an event type. Handler's
// own event types are looked up in handlerEventDisplayNames; anything else
// (watcher types, plus a raw-string fallback for truly unknown types) is
// delegated to the watcher library's EventType.DisplayName.
func eventDisplayName(t string) string {
	if name, ok := handlerEventDisplayNames[t]; ok {
		return name
	}
	return watcherlib.EventType(t).DisplayName()
}
