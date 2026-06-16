package cmd

import (
	"fmt"

	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/discover"
	"github.com/spf13/cobra"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Print the current session ID",
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID, err := discover.ResolveSessionID(db.HandlerHome())
		if err != nil {
			return fmt.Errorf("no session registered for this process. Run 'handler register' or start a new prompt first")
		}

		fmt.Print(sessionID)
		return nil
	},
}

func init() {
	whoamiCmd.GroupID = "agent"
	rootCmd.AddCommand(whoamiCmd)
}
