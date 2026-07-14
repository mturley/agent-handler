package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mturley/agent-handler/discover"
	"github.com/mturley/agent-handler/terminal"
	"github.com/spf13/cobra"
)

var peekCmd = &cobra.Command{
	Use:   "peek",
	Short: "Capture terminal content for a session",
	RunE:  runPeek,
}

var (
	peekSessionID    string
	peekLines        int
	peekListNeedInput bool
)

func init() {
	peekCmd.GroupID = "agent"
	rootCmd.AddCommand(peekCmd)
	peekCmd.Flags().StringVar(&peekSessionID, "session", "", "session ID, name, or branch")
	peekCmd.Flags().IntVar(&peekLines, "lines", 0, "limit capture to last N lines (0 = full pane)")
	peekCmd.Flags().BoolVar(&peekListNeedInput, "list-need-input", false, "list all sessions waiting for user input")
}

func runPeek(cmd *cobra.Command, args []string) error {
	if peekListNeedInput {
		return runListNeedInput(cmd)
	}

	if peekSessionID == "" {
		return fmt.Errorf("required flag \"session\" not set")
	}

	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	session, err := resolveSessionByTarget(d, peekSessionID)
	if err != nil {
		return err
	}

	if session.TerminalType == "" || session.TerminalID == "" {
		return fmt.Errorf("session is not peekable (not started via handler claude or not in a supported terminal)")
	}

	if !discover.IsSessionProcess(session.PID, session.SessionID) {
		return fmt.Errorf("session process is not running (PID %d not found)", session.PID)
	}

	backend, err := terminal.NewBackend(session.TerminalType)
	if err != nil {
		return fmt.Errorf("failed to create terminal backend: %w", err)
	}

	content, err := backend.Capture(session.TerminalID, peekLines)
	if err != nil {
		return fmt.Errorf("failed to capture terminal: %w", err)
	}

	if jsonOutput {
		output := map[string]interface{}{
			"session_id":    session.SessionID,
			"session_name":  session.SessionName,
			"terminal_type": session.TerminalType,
			"captured_at":   time.Now().UTC().Format(time.RFC3339),
			"content":       content,
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Print(content)
		if len(content) > 0 && content[len(content)-1] != '\n' {
			fmt.Println()
		}
	}

	return nil
}

func runListNeedInput(cmd *cobra.Command) error {
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	type needInputResult struct {
		SessionID   string `json:"session_id"`
		SessionName string `json:"session_name"`
		Reason      string `json:"reason"`
	}

	awaiting := findSessionsAwaitingApproval(d)
	var results []needInputResult
	for _, s := range awaiting {
		results = append(results, needInputResult{
			SessionID:   s.SessionID,
			SessionName: s.SessionName,
			Reason:      "awaiting approval",
		})
	}

	if jsonOutput {
		data, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		if len(results) == 0 {
			fmt.Println("No sessions waiting for input.")
			return nil
		}
		for _, r := range results {
			name := r.SessionName
			if name == "" {
				name = r.SessionID[:8]
			}
			fmt.Printf("  %s — %s\n", name, r.Reason)
		}
	}

	return nil
}
