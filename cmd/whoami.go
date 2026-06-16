package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
	"github.com/spf13/cobra"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Print the current session ID",
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionsDir := filepath.Join(db.HandlerHome(), "data", "sessions")
		pid := os.Getppid()

		sessionID, err := discover.ReadPIDCache(sessionsDir, pid)
		if err != nil {
			return fmt.Errorf("no session registered for this process. Run 'handler register' or start a new prompt first")
		}

		fmt.Print(sessionID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(whoamiCmd)
}
