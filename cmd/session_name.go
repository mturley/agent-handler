package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var sessionNameCmd = &cobra.Command{
	Use:   "session-name",
	Short: "Print the current session's name",
	RunE:  runSessionName,
}

func init() {
	sessionNameCmd.GroupID = "agent"
	rootCmd.AddCommand(sessionNameCmd)
	sessionNameCmd.Flags().String("session-id", "", "session ID (auto-detected if omitted)")
}

func runSessionName(cmd *cobra.Command, args []string) error {
	sessionID, err := resolveSessionID(cmd)
	if err != nil {
		return fmt.Errorf("could not determine session: %w", err)
	}

	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	session, err := d.GetSession(sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	if session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	fmt.Println(session.SessionName)
	return nil
}
