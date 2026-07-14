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
	d, err := openReadOnlyDB()
	if err != nil {
		return nil
	}
	defer d.Close()

	session, err := d.GetSession(notifySessionID)
	if err != nil || session == nil {
		return nil
	}

	dispatchNotification(session, notifyCount, notifyMessage)
	return nil
}

// dispatchNotification sends a terminal notification if the unread count increased.
func dispatchNotification(session *db.Session, unreadCount int, message string) {
	if unreadCount == 0 {
		countFile := notifiedCountPath(session.SessionID)
		os.Remove(countFile)
		return
	}

	if session.TerminalType == "" || session.TerminalID == "" {
		return
	}

	countFile := notifiedCountPath(session.SessionID)
	cachedCount := 0
	if data, err := os.ReadFile(countFile); err == nil {
		cachedCount, _ = strconv.Atoi(string(data))
	}

	if unreadCount <= cachedCount {
		return
	}

	backend, err := terminal.NewBackend(session.TerminalType)
	if err != nil {
		return
	}

	title := "handler"
	body := message
	if body == "" {
		body = fmt.Sprintf("%d unread event(s)", unreadCount)
	}

	backend.Notify(session.TerminalID, title, body)
	backend.Flash(session.TerminalID)

	os.MkdirAll(filepath.Dir(countFile), 0755)
	os.WriteFile(countFile, []byte(strconv.Itoa(unreadCount)), 0644)
}

func notifiedCountPath(sessionID string) string {
	return filepath.Join(db.HandlerHome(), "sessions", sessionID+".notified_count")
}
