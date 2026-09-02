package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:    "stop-hook",
	Short:  "Handle Claude Code Stop hook (marks session not working)",
	Hidden: true,
	RunE:   runStopHook,
}

func init() {
	rootCmd.AddCommand(stopCmd)
}

type stopHookInput struct {
	SessionID      string `json:"session_id"`
	StopHookActive bool   `json:"stop_hook_active"`
}

func runStopHook(cmd *cobra.Command, args []string) error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil
	}

	d, err := openDB()
	if err != nil {
		return nil
	}
	defer d.Close()

	return applyStopHook(d, data)
}

// applyStopHook handles both jobs the Stop hook is responsible for: marking the
// session idle, and reconciling its cron rows against the authoritative
// session_crons snapshot. Cron reconciliation is best-effort — a failure there
// must not stop the session being marked idle, since the working flag drives
// the statusline.
func applyStopHook(d *db.DB, data []byte) error {
	var input stopHookInput
	if err := json.Unmarshal(data, &input); err != nil || input.SessionID == "" {
		return nil
	}

	d.SetWorking(input.SessionID, false)
	applyStopHookCrons(d, data)

	// A wake job is pointless once the session is idle, and firing one would
	// interrupt a session waiting on the user. Only Claude can call CronDelete,
	// so hold the session open for one turn to let it cancel. stop_hook_active
	// is the loop guard.
	cfg, _ := config.Read(config.DefaultPath())
	if cfg != nil && cfg.AutoWakeOnRateLimit() {
		if ids, force := wakeStopDecision(d, input.SessionID, time.Now(), input.StopHookActive); force {
			fmt.Fprintln(os.Stderr, wakeDeleteInstruction(ids))
			d.Close()
			os.Exit(2)
		}
	}
	return nil
}
