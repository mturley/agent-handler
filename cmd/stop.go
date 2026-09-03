package cmd

import (
	"encoding/json"
	"io"
	"os"

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
	SessionID string `json:"session_id"`
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

	// Stop deliberately does NOT touch wake jobs. The end of a turn is not the
	// end of the session's work — subagents may still be running — and an
	// earlier cancel-on-Stop fought with the PostToolUse create path, with the
	// two hooks issuing opposing instructions every turn. See wakeStopForces.
	return nil
}
