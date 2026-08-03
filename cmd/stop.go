package cmd

import (
	"encoding/json"
	"io"
	"os"

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

	var input stopHookInput
	if err := json.Unmarshal(data, &input); err != nil || input.SessionID == "" {
		return nil
	}

	d, err := openDB()
	if err != nil {
		return nil
	}
	defer d.Close()

	d.SetWorking(input.SessionID, false)
	return nil
}
