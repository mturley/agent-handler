package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mturley/agent-handler/config"
	"github.com/spf13/cobra"
)

// wakeMarkerMaxAge bounds how long a wake-armed marker may outlive the
// session it was written for. The statusline clears a session's own marker
// as soon as usage drops back below threshold, but a session that crashes or
// is killed while armed never runs that code again, so its marker would
// otherwise sit in wakeMarkerDir() forever. Since the shell wrapper
// (hooks/post_tool_use_wake.sh) spawns the handler binary on every tool call
// in EVERY session as long as the directory is non-empty, one leaked marker
// defeats the fast-path for every other session too. runWakeCheck only runs
// when the shell wrapper already found at least one marker, so it is a
// convenient place to sweep stale ones without any extra process spawn.
//
// This does NOT use fscache.Prune: that helper writes its own ".pruned"
// throttle marker into the directory it scans, which would itself make
// wakeMarkerDir() permanently non-empty and defeat the shell wrapper's
// glob check forever. pruneStaleWakeMarkers is a plain, unthrottled sweep
// instead — cheap (a directory listing plus a stat per entry, no process
// spawn) and only reached when the directory was already non-empty.
const wakeMarkerMaxAge = 6 * time.Hour

// pruneStaleWakeMarkers removes wake-armed markers older than
// wakeMarkerMaxAge, so a marker left behind by a crashed or killed session
// cannot keep the PostToolUse wake hook's fast-path disarmed indefinitely
// for every session. Best-effort: errors are ignored.
func pruneStaleWakeMarkers(now time.Time) {
	dir := wakeMarkerDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > wakeMarkerMaxAge {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// Hook entry points for automatic rate-limit wake jobs. All three are silent
// no-ops when the feature is disabled, when the payload is malformed, or when
// the rate limit state is missing or stale — a hook must never disrupt a
// session, so every failure mode here is quiet.

var wakeCheckCmd = &cobra.Command{
	Use:    "wake-check",
	Short:  "Handle PostToolUse hook for rate-limit wake jobs",
	Hidden: true,
	RunE:   runWakeCheck,
}

var cronGuardCmd = &cobra.Command{
	Use:    "cron-guard",
	Short:  "Handle PreToolUse hook for CronCreate (auto-approves the wake job only)",
	Hidden: true,
	RunE:   runCronGuard,
}

var stopFailureCmd = &cobra.Command{
	Use:    "stop-failure-hook",
	Short:  "Handle StopFailure hook (records rate-limit terminations)",
	Hidden: true,
	RunE:   runStopFailure,
}

func init() {
	rootCmd.AddCommand(wakeCheckCmd, cronGuardCmd, stopFailureCmd)
}

type wakeHookInput struct {
	SessionID string `json:"session_id"`
	ToolInput struct {
		Cron      string `json:"cron"`
		Prompt    string `json:"prompt"`
		Recurring bool   `json:"recurring"`
	} `json:"tool_input"`
	Error string `json:"error"`
}

func readWakeInput() (*wakeHookInput, *config.Config, bool) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, nil, false
	}
	var in wakeHookInput
	if err := json.Unmarshal(data, &in); err != nil || in.SessionID == "" {
		return nil, nil, false
	}
	cfg, _ := config.Read(config.DefaultPath())
	if cfg == nil || !cfg.AutoWakeOnRateLimit() {
		return nil, nil, false
	}
	return &in, cfg, true
}

// runWakeCheck injects the wake instruction mid-task. PostToolUse stdout goes
// to the debug log, so the message must travel as hookSpecificOutput.
// additionalContext, which is delivered inline with the tool result and is
// therefore acted on before the session's next tool call.
func runWakeCheck(cmd *cobra.Command, args []string) error {
	in, cfg, ok := readWakeInput()
	if !ok {
		return nil
	}
	pruneStaleWakeMarkers(time.Now())
	d, err := openDB()
	if err != nil {
		return nil
	}
	defer d.Close()

	msg := wakeCheckMessage(d, cfg, in.SessionID, time.Now())
	if msg == "" {
		return nil
	}
	out, err := json.Marshal(map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName":     "PostToolUse",
			"additionalContext": msg,
		},
	})
	if err != nil {
		return nil
	}
	fmt.Println(string(out))
	return nil
}

// runCronGuard auto-approves the wake job, and only the wake job. The one
// thing it denies is a wake job pinned to a moment that has already passed
// (see isStaleWakeJob); anything else it does not recognise gets no decision
// and falls through to normal permission handling.
func runCronGuard(cmd *cobra.Command, args []string) error {
	in, cfg, ok := readWakeInput()
	if !ok {
		return nil
	}
	if isStaleWakeJob(in.ToolInput.Cron, in.ToolInput.Prompt, in.ToolInput.Recurring, time.Now()) {
		out, err := json.Marshal(map[string]interface{}{
			"hookSpecificOutput": map[string]interface{}{
				"hookEventName":      "PreToolUse",
				"permissionDecision": "deny",
				"permissionDecisionReason": "agent-handler: this wake job's time has already passed — " +
					"the rate limit has reset, so it is no longer needed. Do not retry it; carry on " +
					"with what you were doing.",
			},
		})
		if err != nil {
			return nil
		}
		fmt.Println(string(out))
		return nil
	}
	d, err := openDB()
	if err != nil {
		return nil
	}
	defer d.Close()

	if !shouldAllowCronCreate(d, cfg, in.SessionID, in.ToolInput.Prompt, in.ToolInput.Recurring, time.Now()) {
		return nil
	}
	out, err := json.Marshal(map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "allow",
			"permissionDecisionReason": "agent-handler: recognised rate-limit wake job",
		},
	})
	if err != nil {
		return nil
	}
	fmt.Println(string(out))
	return nil
}

// runStopFailure records a turn that died on a rate limit, so the Stop hook
// knows not to cancel the wake job that failure just made necessary.
func runStopFailure(cmd *cobra.Command, args []string) error {
	in, _, ok := readWakeInput()
	if !ok {
		return nil
	}
	if in.Error != "rate_limit" {
		return nil
	}
	d, err := openDB()
	if err != nil {
		return nil
	}
	defer d.Close()

	d.RecordRateLimitError(in.SessionID, time.Now().UTC().Format(time.RFC3339))
	return nil
}
