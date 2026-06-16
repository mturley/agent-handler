package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mturley/agent-handler/discover"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "List sessions and their status",
	RunE:  runStatus,
}

var (
	statusAll   bool
	statusLimit int
)

func init() {
	statusCmd.GroupID = "human"
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().BoolVar(&statusAll, "all", false, "include archived sessions")
	statusCmd.Flags().IntVar(&statusLimit, "limit", 20, "maximum number of sessions to show")
}

func runStatus(cmd *cobra.Command, args []string) error {
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	sessions, err := d.ListSessions(statusAll, statusLimit, 0)
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	type sessionStatus struct {
		SessionID    string         `json:"session_id"`
		SessionName  string         `json:"session_name"`
		Branch       string         `json:"branch"`
		PID          int            `json:"pid"`
		Status       string         `json:"status"`
		DisplayState string         `json:"display_state"`
		InboxMode    string         `json:"inbox_mode"`
		UnreadCount  int            `json:"unread_count"`
		LastActive   string         `json:"last_active"`
		Breakdown    map[string]int `json:"unread_breakdown,omitempty"`
	}

	statuses := []sessionStatus{}

	for _, s := range sessions {
		// Determine display state
		displayState := "archived"
		if s.Status != "archived" {
			processAlive := discover.IsProcessAlive(s.PID)
			if !processAlive {
				displayState = "dead"
			} else {
				// Check if heartbeat is recent (within 5 minutes)
				lastActive, err := time.Parse(time.RFC3339, s.LastActive)
				if err == nil {
					age := time.Since(lastActive)
					if age < 5*time.Minute {
						displayState = "active"
					} else {
						displayState = "idle"
					}
				} else {
					displayState = "idle"
				}
			}
		}

		// Query unread count
		unreadCount, breakdown, err := d.UnreadCountForSession(s.SessionID)
		if err != nil {
			unreadCount = 0
			breakdown = nil
		}

		statuses = append(statuses, sessionStatus{
			SessionID:    s.SessionID,
			SessionName:  s.SessionName,
			Branch:       s.Branch,
			PID:          s.PID,
			Status:       s.Status,
			DisplayState: displayState,
			InboxMode:    s.InboxMode,
			UnreadCount:  unreadCount,
			LastActive:   s.LastActive,
			Breakdown:    breakdown,
		})
	}

	if jsonOutput {
		data, err := json.MarshalIndent(statuses, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		if len(statuses) == 0 {
			fmt.Println("No sessions found")
			return nil
		}

		fmt.Printf("%-20s %-15s %-10s %-8s %-10s %s\n", "SESSION", "NAME", "STATE", "UNREAD", "INBOX", "BRANCH")
		fmt.Println("────────────────────────────────────────────────────────────────────────────")
		for _, st := range statuses {
			name := st.SessionName
			if name == "" {
				name = "-"
			}
			if len(name) > 15 {
				name = name[:12] + "..."
			}

			unreadStr := fmt.Sprintf("%d", st.UnreadCount)
			if st.UnreadCount == 0 {
				unreadStr = "-"
			}

			sessionDisplay := st.SessionID
			if len(sessionDisplay) > 20 {
				sessionDisplay = sessionDisplay[:20]
			}
			fmt.Printf("%-20s %-15s %-10s %-8s %-10s %s\n",
				sessionDisplay, name, st.DisplayState, unreadStr, st.InboxMode, st.Branch)
		}
	}

	return nil
}
