package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var autoDeliveredCmd = &cobra.Command{
	Use:   "auto-delivered",
	Short: "Print the number of events auto-delivered since the last human prompt",
	RunE:  runAutoDelivered,
}

func init() {
	autoDeliveredCmd.GroupID = "agent"
	rootCmd.AddCommand(autoDeliveredCmd)
	autoDeliveredCmd.Flags().String("session-id", "", "session ID (auto-detected if omitted)")
}

func runAutoDelivered(cmd *cobra.Command, args []string) error {
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	sessionID, err := resolveSessionID(cmd)
	if err != nil {
		return fmt.Errorf("could not determine session: %w", err)
	}

	session, err := d.GetSession(sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	if session == nil {
		fmt.Println("0")
		return nil
	}

	var count int
	if session.Role == "handler" {
		count, err = d.AutoDeliveredCountAll(sessionID)
	} else {
		count, err = d.AutoDeliveredCount(sessionID)
	}
	if err != nil {
		return fmt.Errorf("failed to count auto-delivered events: %w", err)
	}

	fmt.Println(count)
	return nil
}
