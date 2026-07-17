package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/chzyer/readline"
	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
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
	switchCloseCaller   bool
)

func init() {
	switchCmd.GroupID = "human"
	rootCmd.AddCommand(switchCmd)
	switchCmd.Flags().StringVar(&switchSession, "session", "", "session name, ID, or branch to switch to")
	switchCmd.Flags().BoolVarP(&switchFirstAwaiting, "first-awaiting", "a", false, "switch to the first session awaiting approval")
	switchCmd.Flags().BoolVar(&switchCloseCaller, "close-caller", false, "close the calling cmux surface after switching (for keyboard shortcut actions)")
}

func runSwitch(cmd *cobra.Command, args []string) error {
	if os.Getenv("CMUX_SURFACE_ID") == "" {
		return fmt.Errorf("not running inside cmux")
	}

	// Accept session as positional arg
	if switchSession == "" && len(args) > 0 {
		switchSession = strings.Join(args, " ")
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
	} else if switchSession != "" {
		session, err = resolveSessionByTarget(d, switchSession)
		if err != nil {
			return err
		}
	} else {
		session, err = interactiveSwitch(d)
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

	selfSurface := os.Getenv("CMUX_SURFACE_ID")
	selfWorkspace := os.Getenv("CMUX_WORKSPACE_ID")

	// If --close-caller and we're in the same workspace as the target,
	// move our tab to just after the target so closing it falls back to the target.
	if switchCloseCaller && selfSurface != "" && selfSurface != session.TerminalID &&
		selfWorkspace == session.CmuxWorkspaceID {
		exec.Command("cmux", "reorder-surface",
			"--surface", selfSurface,
			"--after", session.TerminalID,
		).Run()
	}

	if out, err := exec.Command("cmux", "workspace", "select",
		"--workspace", session.CmuxWorkspaceID,
	).CombinedOutput(); err != nil {
		return fmt.Errorf("cmux workspace select failed: %s", string(out))
	}
	if out, err := exec.Command("cmux", "focus-panel",
		"--panel", session.TerminalID,
		"--workspace", session.CmuxWorkspaceID,
	).CombinedOutput(); err != nil {
		return fmt.Errorf("cmux focus-panel failed: %s", string(out))
	}

	// Close the caller surface after switching
	if switchCloseCaller && selfSurface != "" && selfSurface != session.TerminalID {
		exec.Command("cmux", "close-surface", "--surface", selfSurface).Run()
	}

	name := session.SessionName
	if name == "" {
		name = session.SessionID[:8]
	}
	fmt.Printf("Switched to %s\n", name)
	return nil
}

func interactiveSwitch(d *db.DB) (*db.Session, error) {
	selfSurface := os.Getenv("CMUX_SURFACE_ID")

	sessions, err := d.ListSessions(false, 100, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	// Filter to switchable cmux sessions (exclude self, dead, non-cmux)
	var candidates []db.Session
	var names []string
	for _, s := range sessions {
		if s.TerminalType != "cmux" || s.TerminalID == "" || s.CmuxWorkspaceID == "" {
			continue
		}
		if s.TerminalID == selfSurface {
			continue
		}
		if s.PID > 0 && !discover.IsSessionProcess(s.PID, s.SessionID) {
			continue
		}
		candidates = append(candidates, s)
		name := s.SessionName
		if name == "" {
			name = s.SessionID[:8]
		}
		names = append(names, name)
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no other cmux sessions to switch to")
	}

	// Build minimal statuses for renderSessionList
	var statuses []sessionStatus
	for _, s := range candidates {
		statuses = append(statuses, sessionStatus{
			SessionID:    s.SessionID,
			SessionName:  s.SessionName,
			Branch:       s.Branch,
			DisplayState: "active",
			Peekable:     s.TerminalType != "",
			TerminalType: s.TerminalType,
			LastActive:   s.LastActive,
		})
	}
	renderSessionList(candidates, statuses)
	fmt.Println()

	// Readline with tab completion
	completer := readline.NewPrefixCompleter()
	for _, name := range names {
		completer.Children = append(completer.Children, readline.PcItem(name))
	}

	rl, err := readline.NewEx(&readline.Config{
		Prompt:       "Switch to session \033[2m(tab-complete supported)\033[0m: ",
		AutoComplete: completer,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize prompt: %w", err)
	}
	defer rl.Close()

	input, err := rl.Readline()
	if err != nil {
		return nil, fmt.Errorf("cancelled")
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("no selection")
	}

	for i, s := range candidates {
		if names[i] == input || s.SessionName == input || s.SessionID == input {
			return &s, nil
		}
	}

	return nil, fmt.Errorf("session %q not found", input)
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
