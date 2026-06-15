package resource

import (
	"encoding/json"
	"fmt"

	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var relatedCmd = &cobra.Command{
	Use:   "related",
	Short: "Find sessions related to a session via shared resources",
	RunE:  runRelated,
}

var relatedSessionID string

func init() {
	ResourceCmd.AddCommand(relatedCmd)
	relatedCmd.Flags().StringVar(&relatedSessionID, "session", "", "session ID")
	relatedCmd.MarkFlagRequired("session")
}

func runRelated(cmd *cobra.Command, args []string) error {
	d, err := db.OpenReadOnly(db.DefaultPath())
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	// Find related sessions
	sessions, err := d.FindRelatedSessions(relatedSessionID)
	if err != nil {
		return fmt.Errorf("failed to find related sessions: %w", err)
	}

	// Output
	if JSONOutput != nil && *JSONOutput {
		data, err := json.MarshalIndent(sessions, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		if len(sessions) == 0 {
			fmt.Println("No related sessions found")
			return nil
		}

		fmt.Printf("Related sessions for %s:\n\n", relatedSessionID)
		for _, s := range sessions {
			fmt.Printf("  %s [%s]\n", s.SessionID, s.Status)
			if s.SessionName != "" {
				fmt.Printf("    Name: %s\n", s.SessionName)
			}
			fmt.Printf("    Repo: %s\n", s.Repo)
			fmt.Printf("    Branch: %s\n", s.Branch)
			fmt.Printf("    Last active: %s\n", s.LastActive)
			fmt.Println()
		}
	}

	return nil
}
