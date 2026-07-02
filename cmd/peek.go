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
	peekSessionID string
	peekLines     int
)

func init() {
	peekCmd.GroupID = "agent"
	rootCmd.AddCommand(peekCmd)
	peekCmd.Flags().StringVar(&peekSessionID, "session", "", "session ID, name, or branch")
	peekCmd.Flags().IntVar(&peekLines, "lines", 0, "limit capture to last N lines (0 = full pane)")
	peekCmd.MarkFlagRequired("session")
}

func runPeek(cmd *cobra.Command, args []string) error {
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	session, err := d.GetSession(peekSessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	if session == nil {
		return fmt.Errorf("session %q not found", peekSessionID)
	}

	if session.TerminalType == "" || session.TerminalID == "" {
		return fmt.Errorf("session is not peekable (not started via handler claude or not in a supported terminal)")
	}

	if !discover.IsProcessAlive(session.PID) {
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
