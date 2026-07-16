package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var switchCmd = &cobra.Command{
	Use:                "switch [session-name]",
	Short:              "Switch to another session's cmux workspace and surface",
	Long:               "Navigate to another session's cmux workspace and focus its surface tab. Must be run from within cmux.",
	DisableFlagParsing: false,
	RunE:               runSwitch,
}

var (
	switchSession       string
	switchFirstAwaiting bool
)

func init() {
	switchCmd.GroupID = "human"
	rootCmd.AddCommand(switchCmd)
	switchCmd.Flags().StringVar(&switchSession, "session", "", "session name, ID, or branch to switch to")
	switchCmd.Flags().BoolVarP(&switchFirstAwaiting, "first-awaiting", "a", false, "switch to the first session awaiting approval")
}

func runSwitch(cmd *cobra.Command, args []string) error {
	if os.Getenv("CMUX_SURFACE_ID") == "" {
		return fmt.Errorf("not running inside cmux")
	}

	// Accept session as positional arg
	if switchSession == "" && len(args) > 0 {
		switchSession = strings.Join(args, " ")
	}

	if switchSession == "" && !switchFirstAwaiting {
		return fmt.Errorf("either a session name or --first-awaiting/-a is required")
	}

	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	var session *db.Session

	if switchFirstAwaiting {
		session, err = findFirstAwaiting(d)
		if err != nil {
			return err
		}
	} else {
		session, err = resolveSessionByTarget(d, switchSession)
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
