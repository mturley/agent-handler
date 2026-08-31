package cmd

import (
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

// cronToolHookInput is the PostToolUse payload for CronCreate / CronDelete.
// See docs/claude-hook-stdin.md ("PostToolUse" and "Cron tool payloads").
type cronToolHookInput struct {
	SessionID string `json:"session_id"`
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		ID        string `json:"id"`   // CronDelete
		Cron      string `json:"cron"` // CronCreate
		Prompt    string `json:"prompt"`
		Recurring bool   `json:"recurring"`
	} `json:"tool_input"`
	ToolResponse struct {
		ID            string `json:"id"`
		HumanSchedule string `json:"humanSchedule"`
		Recurring     bool   `json:"recurring"`
	} `json:"tool_response"`
}

// stopHookCronsInput carries the Stop hook's authoritative cron snapshot.
// SessionCrons is a POINTER so an absent key ("unknown") is distinguishable
// from an explicit empty array ("no jobs"). Only the latter may clear rows.
type stopHookCronsInput struct {
	SessionID    string `json:"session_id"`
	SessionCrons *[]struct {
		ID        string `json:"id"`
		Schedule  string `json:"schedule"`
		Recurring bool   `json:"recurring"`
		Prompt    string `json:"prompt"`
	} `json:"session_crons"`
}

var cronHookCmd = &cobra.Command{
	Use:    "cron-hook",
	Short:  "Handle Claude Code PostToolUse hook for CronCreate/CronDelete",
	Hidden: true,
	RunE:   runCronHook,
}

func init() {
	rootCmd.AddCommand(cronHookCmd)
}

func runCronHook(cmd *cobra.Command, args []string) error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil
	}
	d, err := openDB()
	if err != nil {
		return nil
	}
	defer d.Close()

	applyCronToolHook(d, data)
	return nil
}

// applyCronToolHook records or removes a cron job from a PostToolUse payload.
// Hooks must never fail a user's tool call, so malformed or irrelevant input is
// silently ignored rather than returned as an error.
func applyCronToolHook(d *db.DB, data []byte) error {
	var in cronToolHookInput
	if err := json.Unmarshal(data, &in); err != nil || in.SessionID == "" {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339)

	switch in.ToolName {
	case "CronCreate":
		jobID := in.ToolResponse.ID
		if jobID == "" {
			return nil
		}
		schedule := in.ToolInput.Cron
		if schedule == "" {
			schedule = in.ToolResponse.HumanSchedule
		}
		recurring := in.ToolInput.Recurring || in.ToolResponse.Recurring
		return d.UpsertSessionCron(in.SessionID, db.SessionCron{
			JobID:     jobID,
			Schedule:  schedule,
			Recurring: recurring,
			Prompt:    in.ToolInput.Prompt,
		}, now)

	case "CronDelete":
		jobID := in.ToolInput.ID
		if jobID == "" {
			jobID = in.ToolResponse.ID
		}
		if jobID == "" {
			return nil
		}
		return d.DeleteSessionCron(in.SessionID, jobID)
	}

	return nil
}

// applyStopHookCrons reconciles a session's cron rows against the Stop hook's
// session_crons snapshot. This is what catches one-shot jobs that fired and
// auto-deleted without emitting a CronDelete, and re-adds any job a missed
// PostToolUse hook failed to record.
//
// An absent session_crons key means "unknown" and is left alone; only an
// explicit (possibly empty) array is treated as authoritative.
func applyStopHookCrons(d *db.DB, data []byte) error {
	var in stopHookCronsInput
	if err := json.Unmarshal(data, &in); err != nil || in.SessionID == "" {
		return nil
	}
	if in.SessionCrons == nil {
		return nil
	}

	snapshot := make([]db.SessionCron, 0, len(*in.SessionCrons))
	for _, c := range *in.SessionCrons {
		if c.ID == "" {
			continue
		}
		snapshot = append(snapshot, db.SessionCron{
			JobID:     c.ID,
			Schedule:  c.Schedule,
			Recurring: c.Recurring,
			Prompt:    c.Prompt,
		})
	}

	now := time.Now().UTC().Format(time.RFC3339)
	return d.SyncSessionCrons(in.SessionID, snapshot, now)
}
