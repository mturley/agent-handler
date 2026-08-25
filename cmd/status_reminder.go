package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
)

// statusReminderDecision is the outcome of the threshold check. Unit is
// "turns" or "hours" and Count is the corresponding elapsed amount.
type statusReminderDecision struct {
	Fire  bool
	Unit  string
	Count int
}

// decideStatusReminder reports whether a session is overdue for a status event.
// The turn check wins when both thresholds trip, since a turn count is more
// actionable than wall-clock time. A threshold of 0 disables that check, and an
// unknown (zero) or future baseline skips the elapsed-time check entirely.
func decideStatusReminder(s config.StatusReminderSettings, promptsSinceStatus int, baseline, now time.Time) statusReminderDecision {
	if !s.Enabled {
		return statusReminderDecision{}
	}

	if s.Turns > 0 && promptsSinceStatus >= s.Turns {
		return statusReminderDecision{Fire: true, Unit: "turns", Count: promptsSinceStatus}
	}

	if s.Hours > 0 && !baseline.IsZero() {
		elapsed := now.Sub(baseline)
		if elapsed >= time.Duration(s.Hours)*time.Hour {
			return statusReminderDecision{Fire: true, Unit: "hours", Count: int(elapsed.Hours())}
		}
	}

	return statusReminderDecision{}
}

// statusReminderMessage renders the nudge injected into the session's context.
func statusReminderMessage(d statusReminderDecision) string {
	unit := d.Unit
	if d.Count == 1 {
		unit = strings.TrimSuffix(unit, "s")
	}
	return fmt.Sprintf("Reminder: it has been %d %s since your last emitted status. "+
		"Use `handler emit --type status` to update the ledger with what you have been doing "+
		"since the last status and what you are doing now. Be concise.", d.Count, unit)
}

// autoInboxPrompt is the automated poll the inbox issues; it is not real work,
// so it must not count toward the turn threshold.
const autoInboxPrompt = "/inbox --auto"

// checkStatusReminder records this prompt as a turn and returns the reminder
// text if the session is overdue for a status event, or "" if not. Firing a
// reminder resets both the turn counter and the time baseline, so the nudge
// can't repeat until another full interval has passed.
func checkStatusReminder(d *db.DB, session *db.Session, prompt string, now time.Time) string {
	if session == nil || prompt == autoInboxPrompt {
		return ""
	}
	// The handler command-center session polls continuously; reminding it would
	// trip the turn threshold constantly and flood the ledger.
	if session.Role == "handler" {
		return ""
	}

	cfg, err := config.Read(config.DefaultPath())
	if err != nil {
		return ""
	}
	settings := cfg.StatusReminder()
	if !settings.Enabled {
		return ""
	}

	prompts, err := d.BumpPromptsSinceStatus(session.SessionID)
	if err != nil {
		return ""
	}
	_, baselineStr, err := d.StatusReminderState(session.SessionID)
	if err != nil {
		return ""
	}
	baseline, err := time.Parse(time.RFC3339, baselineStr)
	if err != nil {
		baseline = time.Time{} // unknown baseline skips the elapsed-time check
	}

	decision := decideStatusReminder(settings, prompts, baseline, now)
	if !decision.Fire {
		return ""
	}

	d.ResetStatusReminderBaseline(session.SessionID, now.Format(time.RFC3339))
	return statusReminderMessage(decision)
}
