package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
)

// wakeMarker identifies a wake job's prompt. The hooks use it to recognise an
// existing job, and the PreToolUse guard uses it to decide whether a CronCreate
// call is the wake job it expects.
const wakeMarker = "[handler-wake]"

// wakeStaleWindow bounds how old the statusline's rate limit observation may be
// before it is ignored. The statusline refreshes roughly every 10s even for
// idle, backgrounded sessions (verified), so anything this old means it stopped
// reporting and the value must not be trusted.
const wakeStaleWindow = 15 * time.Minute

// wakeDecision is the outcome of the hook-side check. Everything the session
// needs is computed here so the injected instruction is fully formed.
type wakeDecision struct {
	Inject bool
	Cron   string // pinned one-shot expression, local time
	Prompt string // prompt for the wake job itself
	FireAt time.Time
}

// isWakeCron reports whether a tracked cron job is a wake job.
func isWakeCron(c db.SessionCron) bool {
	return strings.Contains(c.Prompt, wakeMarker)
}

// decideWake performs the whole hook-side check: config, freshness, threshold,
// existing-job guard, and reset-time validity. Claude is never asked to verify
// any of this — if Inject is true the session should simply create the job.
//
// Every failure mode is silent: a missing or stale row means "unknown", never
// "over threshold". Failing closed keeps a broken statusline from spamming wake
// jobs.
func decideWake(cfg *config.Config, rl *db.RateLimitState, crons []db.SessionCron, now time.Time) wakeDecision {
	if cfg == nil || !cfg.AutoWakeOnRateLimit() {
		return wakeDecision{}
	}
	if rl == nil || rl.IsStale(now, wakeStaleWindow) {
		return wakeDecision{}
	}
	if rl.FiveHourPercent < float64(cfg.AutoWakeThresholdPercent()) {
		return wakeDecision{}
	}
	for _, c := range crons {
		if isWakeCron(c) {
			return wakeDecision{}
		}
	}

	resets, err := time.Parse(time.RFC3339, rl.FiveHourResetsAt)
	if err != nil {
		return wakeDecision{}
	}
	// A reset already behind us means the limit has recovered; nothing to wake for.
	if !resets.After(now) {
		return wakeDecision{}
	}

	// Cron expressions are interpreted in local time, so convert before
	// formatting. One minute after the reset, pinned to the exact date to keep
	// it a true one-shot.
	fireAt := resets.In(now.Location()).Add(time.Minute)
	return wakeDecision{
		Inject: true,
		Cron: fmt.Sprintf("%d %d %d %d *",
			fireAt.Minute(), fireAt.Hour(), fireAt.Day(), int(fireAt.Month())),
		Prompt: wakeMarker + " The rate limit that paused you has reset. If work was " +
			"in progress before the pause, resume it, picking up where you left off " +
			"without asking the user to repeat themselves. If you were waiting for the " +
			"user to answer a question, do not start anything — restate the question " +
			"and wait. If there was no work in progress and no pending question, do " +
			"nothing at all.",
		FireAt: fireAt,
	}
}

// wakeInstruction renders the text injected into the session. It is a complete
// directive with literal arguments: the hook has already confirmed the usage
// and that no wake job exists, so there is nothing for Claude to check.
func wakeInstruction(d wakeDecision) string {
	if !d.Inject {
		return ""
	}
	return fmt.Sprintf(
		"[agent-handler wake] Your 5h rate limit is close to its cap and resets at %s. "+
			"No wake job exists for this session yet. Before your next tool call, call "+
			"CronCreate with cron %q, recurring false, and prompt %q. Then carry on with "+
			"what you were doing.",
		d.FireAt.Format("15:04"), d.Cron, d.Prompt)
}

// loadWakeInputs fetches the two pieces of state every wake path needs.
func loadWakeInputs(d *db.DB, sessionID string) (*db.RateLimitState, []db.SessionCron) {
	rl, err := d.GetRateLimit(sessionID)
	if err != nil {
		return nil, nil
	}
	crons, err := d.ListSessionCrons(sessionID)
	if err != nil {
		return rl, nil
	}
	return rl, crons
}

// wakeCheckMessage runs the full hook-side check and returns the text to inject,
// or "" when nothing should be injected. Every guard the user would otherwise
// have to ask Claude to perform happens here.
func wakeCheckMessage(d *db.DB, cfg *config.Config, sessionID string, now time.Time) string {
	if d == nil || sessionID == "" {
		return ""
	}
	rl, crons := loadWakeInputs(d, sessionID)
	return wakeInstruction(decideWake(cfg, rl, crons, now))
}

// shouldAllowCronCreate decides whether a CronCreate call is the wake job
// handler is currently asking for, and may therefore skip the permission
// prompt. Anything else returns false and falls through to normal handling —
// this never denies, it only declines to vouch.
//
// The grant is deliberately narrow: it exists only while the feature is on, the
// rate limit data is fresh and over threshold, the prompt carries handler's own
// marker, and the job is a one-shot. It is not keyed to the exact schedule,
// because resets_at can tick between the injection and the call.
func shouldAllowCronCreate(d *db.DB, cfg *config.Config, sessionID, prompt string, recurring bool, now time.Time) bool {
	if d == nil || sessionID == "" || cfg == nil || !cfg.AutoWakeOnRateLimit() {
		return false
	}
	if recurring || !strings.Contains(prompt, wakeMarker) {
		return false
	}
	rl, err := d.GetRateLimit(sessionID)
	if err != nil || rl == nil || rl.IsStale(now, wakeStaleWindow) {
		return false
	}
	return rl.FiveHourPercent >= float64(cfg.AutoWakeThresholdPercent())
}

// NOTE: Stop deliberately has no wake-job behaviour at all.
//
// An earlier version cancelled a wake job when the session went idle, holding
// the session open for a turn (Stop exit 2) so it could call CronDelete. Two
// things killed it in live use:
//
//  1. Stop fires at the end of an assistant TURN, which is not the same as the
//     session's work being done — a turn can end with subagents still running.
//  2. Cancel-on-Stop and create-on-PostToolUse contradicted each other every
//     turn, so the two hooks issued opposing instructions and oscillated.
//
// A wake job now outlives the turn. If it fires with nothing to resume it does
// nothing; if the session was waiting on the user it restates the question
// rather than barging ahead. See the prompt in decideWake.

// The PostToolUse wake hook runs after every tool call. Spawning the handler
// binary each time would be wasteful when nothing is near the limit, so the
// statusline maintains a marker file per session and the shell wrapper tests
// for it with a single stat before invoking Go at all.
//
// The marker is a hint, never the authority: every real decision is still made
// against the database.

func wakeMarkerDir() string {
	return filepath.Join(db.HandlerHome(), "state", "wake-armed")
}

func wakeMarkerPath(sessionID string) string {
	return filepath.Join(wakeMarkerDir(), sessionID)
}

// wakeArmed reports whether the fast-path marker is set for a session.
func wakeArmed(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	_, err := os.Stat(wakeMarkerPath(sessionID))
	return err == nil
}

// setWakeArmed creates or removes the marker. Best-effort: a failure here costs
// a redundant hook invocation, never correctness.
func setWakeArmed(sessionID string, armed bool) {
	if sessionID == "" {
		return
	}
	path := wakeMarkerPath(sessionID)
	if !armed {
		os.Remove(path)
		return
	}
	if err := os.MkdirAll(wakeMarkerDir(), 0755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		f.Close()
	}
}

// clearWakeMarkers removes markers for sessions being archived.
func clearWakeMarkers(sessionIDs []string) {
	for _, id := range sessionIDs {
		setWakeArmed(id, false)
	}
}
