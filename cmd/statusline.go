package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var statuslineCmd = &cobra.Command{
	Use:   "statusline",
	Short: "Output statusline info for a session",
	RunE:  runStatusline,
}

var slSessionID string

func init() {
	rootCmd.AddCommand(statuslineCmd)
	statuslineCmd.Flags().StringVar(&slSessionID, "session", "", "session ID")
	statuslineCmd.MarkFlagRequired("session")
}

func runStatusline(cmd *cobra.Command, args []string) error {
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	// Get session
	session, err := d.GetSession(slSessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	if session == nil {
		return fmt.Errorf("session not found: %s", slSessionID)
	}

	// Query unread count
	unreadCount, breakdown, err := d.UnreadCountForSession(slSessionID)
	if err != nil {
		return fmt.Errorf("failed to query unread count: %w", err)
	}

	// Output line 1: inbox status
	if unreadCount == 0 {
		fmt.Println("/inbox: No new messages")
	} else {
		// Build breakdown string
		var breakdownParts []string
		for eventType, count := range breakdown {
			breakdownParts = append(breakdownParts, fmt.Sprintf("%d %s", count, eventType))
		}
		breakdownStr := ""
		if len(breakdownParts) > 0 {
			breakdownStr = fmt.Sprintf(" (%s)", breakdownParts[0])
			if len(breakdownParts) > 1 {
				for i := 1; i < len(breakdownParts); i++ {
					breakdownStr += fmt.Sprintf(", %s", breakdownParts[i])
				}
			}
		}
		fmt.Printf("/inbox: %d unread%s\n", unreadCount, breakdownStr)
	}

	// Output line 2: inbox mode
	// TODO: Detect if polling is stopped for auto mode
	// For now, just show the mode
	switch session.InboxMode {
	case "manual":
		fmt.Println("/inbox_mode: \033[1mmanual\033[0m | on-submit | auto")
	case "on-submit":
		fmt.Println("/inbox_mode: manual | \033[1mon-submit\033[0m | auto")
	case "auto":
		fmt.Println("/inbox_mode: manual | on-submit | \033[1mauto\033[0m")
	default:
		fmt.Printf("/inbox_mode: %s\n", session.InboxMode)
	}

	return nil
}
