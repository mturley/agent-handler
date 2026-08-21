package watcher

// KnownWatchers is the canonical list of external services agent-handler can
// poll/install watchers for. It is the single source of truth: the watcher
// CLI (cmd/watcher) and the status/statusline/triage/watching/uninstall paths
// in the parent cmd package all iterate this list rather than hardcoding
// {"github","jira"}, so a new watcher type appears everywhere at once.
//
// Slack is first-class alongside github and jira: it has credential
// auth/test+repair support and a poller (github.com/mturley/watcher/slack).
var KnownWatchers = []string{"github", "jira", "slack"}
