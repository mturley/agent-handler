package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var cmuxSwitchCmd = &cobra.Command{
	Use:   "cmux-switch",
	Short: "Switch to a session's cmux workspace and surface",
	Long:  "Navigate to a session's cmux workspace and focus its surface tab.",
	RunE:  runCmuxSwitch,
}

var (
	cmuxSwitchSession       string
	cmuxSwitchFirstAwaiting bool
)

func init() {
	cmuxSwitchCmd.GroupID = "human"
	rootCmd.AddCommand(cmuxSwitchCmd)
	cmuxSwitchCmd.Flags().StringVar(&cmuxSwitchSession, "session", "", "session name, ID, or branch to switch to")
	cmuxSwitchCmd.Flags().BoolVarP(&cmuxSwitchFirstAwaiting, "first-awaiting", "a", false, "switch to the first session awaiting approval")
}

func runCmuxSwitch(cmd *cobra.Command, args []string) error {
	if _, err := exec.LookPath("cmux"); err != nil {
		return fmt.Errorf("cmux is not installed or not on PATH")
	}

	// Accept session as positional arg
	if cmuxSwitchSession == "" && len(args) > 0 {
		cmuxSwitchSession = strings.Join(args, " ")
	}

	if cmuxSwitchSession == "" && !cmuxSwitchFirstAwaiting {
		return fmt.Errorf("either --session or --first-awaiting/-a is required")
	}

	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	var session *db.Session

	if cmuxSwitchFirstAwaiting {
		session, err = findFirstAwaiting(d)
		if err != nil {
			return err
		}
	} else {
		session, err = resolveSessionByTarget(d, cmuxSwitchSession)
		if err != nil {
			return err
		}
	}

	if session.TerminalType != "cmux" {
		return fmt.Errorf("session %q is not a cmux session (terminal type: %q)", session.SessionName, session.TerminalType)
	}
	if session.TerminalID == "" || session.CmuxWorkspaceID == "" {
		return fmt.Errorf("session %q is missing cmux surface or workspace ID", session.SessionName)
	}

	out, err := exec.Command("cmux", "focus-panel",
		"--panel", session.TerminalID,
		"--workspace", session.CmuxWorkspaceID,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cmux focus-panel failed: %s", string(out))
	}

	name := session.SessionName
	if name == "" {
		name = session.SessionID[:8]
	}
	fmt.Printf("Switched to %s\n", name)
	return nil
}

func findFirstAwaiting(d *db.DB) (*db.Session, error) {
	awaiting := findSessionsAwaitingApproval(d)
	for _, s := range awaiting {
		if s.TerminalType == "cmux" && s.TerminalID != "" && s.CmuxWorkspaceID != "" {
			return &s, nil
		}
	}
	return nil, fmt.Errorf("no cmux sessions awaiting approval")
}
