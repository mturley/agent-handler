package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mturley/agent-handler/config"
	"github.com/spf13/cobra"
)

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

// runCronGuard auto-approves the wake job, and only the wake job. It never
// denies: anything it does not recognise gets no decision and falls through to
// normal permission handling.
func runCronGuard(cmd *cobra.Command, args []string) error {
	in, cfg, ok := readWakeInput()
	if !ok {
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
