package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/terminal"
	"github.com/spf13/cobra"
)

var notifyCmd = &cobra.Command{
	Use:    "notify",
	Short:  "Send notification if unread count increased (used by statusline hook)",
	Hidden: true,
	RunE:   runNotify,
}

var (
	notifySessionID string
	notifyCount     int
	notifyMessage   string
)

func init() {
	rootCmd.AddCommand(notifyCmd)
	notifyCmd.Flags().StringVar(&notifySessionID, "session", "", "session ID")
	notifyCmd.Flags().IntVar(&notifyCount, "count", 0, "current unread count")
	notifyCmd.Flags().StringVar(&notifyMessage, "message", "", "notification body")
	notifyCmd.MarkFlagRequired("session")
	notifyCmd.MarkFlagRequired("count")
}

func runNotify(cmd *cobra.Command, args []string) error {
	if notifyCount == 0 {
		// Clean up temp file if count dropped to 0
		countFile := notifiedCountPath(notifySessionID)
		os.Remove(countFile)
		return nil
	}

	d, err := openReadOnlyDB()
	if err != nil {
		return nil // silently fail — don't break the statusline
	}
	defer d.Close()

	session, err := d.GetSession(notifySessionID)
	if err != nil || session == nil {
		return nil
	}

	if session.TerminalType == "" || session.TerminalID == "" {
		return nil
	}

	// Check cached count
	countFile := notifiedCountPath(notifySessionID)
	cachedCount := 0
	if data, err := os.ReadFile(countFile); err == nil {
		cachedCount, _ = strconv.Atoi(string(data))
	}

	if notifyCount <= cachedCount {
		return nil
	}

	// Send notification
	backend, err := terminal.NewBackend(session.TerminalType)
	if err != nil {
		return nil // silently fail — don't break the statusline
	}

	title := "handler"
	body := notifyMessage
	if body == "" {
		body = fmt.Sprintf("%d unread event(s)", notifyCount)
	}

	backend.Notify(session.TerminalID, title, body)
	backend.Flash(session.TerminalID)

	// Update cached count
	os.MkdirAll(filepath.Dir(countFile), 0755)
	os.WriteFile(countFile, []byte(strconv.Itoa(notifyCount)), 0644)

	return nil
}

func notifiedCountPath(sessionID string) string {
	return filepath.Join(db.HandlerHome(), "sessions", sessionID+".notified_count")
}
