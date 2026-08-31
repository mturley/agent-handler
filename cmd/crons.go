package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var cronsAllSessions bool

var cronsCmd = &cobra.Command{
	Use:   "crons",
	Short: "List Claude Code cron jobs tracked for a session",
	Long: `List the Claude Code cron jobs recorded for a session.

Jobs are recorded by the PostToolUse hook when CronCreate/CronDelete run, and
reconciled every turn against the Stop hook's authoritative session_crons
snapshot — which is what catches one-shot jobs that auto-delete when they fire.

Claude cron jobs are in-memory and session-scoped: they do not survive the
session that created them.`,
	RunE: runCrons,
}

func init() {
	cronsCmd.GroupID = "human"
	rootCmd.AddCommand(cronsCmd)
	cronsCmd.Flags().String("session-id", "", "session ID (auto-detected if omitted)")
	cronsCmd.Flags().BoolVar(&cronsAllSessions, "all", false, "list cron jobs across all sessions")
}

func runCrons(cmd *cobra.Command, args []string) error {
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	var crons []db.SessionCron
	if cronsAllSessions {
		crons, err = d.ListAllSessionCrons()
	} else {
		var sessionID string
		sessionID, err = resolveSessionID(cmd)
		if err != nil {
			return fmt.Errorf("could not determine session: %w", err)
		}
		crons, err = d.ListSessionCrons(sessionID)
	}
	if err != nil {
		return fmt.Errorf("failed to list cron jobs: %w", err)
	}

	fmt.Print(renderCrons(crons, jsonOutput))
	return nil
}

func truncatePrompt(s string, max int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func renderCrons(crons []db.SessionCron, asJSON bool) string {
	if asJSON {
		if crons == nil {
			crons = []db.SessionCron{}
		}
		data, err := json.MarshalIndent(crons, "", "  ")
		if err != nil {
			return "[]\n"
		}
		return string(data) + "\n"
	}

	if len(crons) == 0 {
		return "No cron jobs tracked\n"
	}

	var b strings.Builder
	for _, c := range crons {
		recurrence := "one-shot"
		if c.Recurring {
			recurrence = "recurring"
		}
		fmt.Fprintf(&b, "%-10s  %-16s  %-9s  %s\n",
			c.JobID, c.Schedule, recurrence, truncatePrompt(c.Prompt, 60))
	}
	return b.String()
}
