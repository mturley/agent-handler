package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var emitCmd = &cobra.Command{
	Use:   "emit",
	Short: "Emit a new event",
	RunE:  runEmit,
}

var (
	emitType      string
	emitTitle     string
	emitBody      string
	emitSessionID string
	emitSource    string
	emitBroadcast bool
	emitTags      string
	emitTo        []string
)

func init() {
	emitCmd.GroupID = "agent"
	rootCmd.AddCommand(emitCmd)
	emitCmd.Flags().StringVar(&emitType, "type", "", "event type (required)")
	emitCmd.Flags().StringVar(&emitTitle, "title", "", "event title (required)")
	emitCmd.Flags().StringVar(&emitBody, "body", "", "event body")
	emitCmd.Flags().StringVar(&emitSessionID, "session-id", "", "target session ID")
	emitCmd.Flags().StringVar(&emitSource, "source", "agent", "event source")
	emitCmd.Flags().BoolVar(&emitBroadcast, "broadcast", false, "broadcast to all sessions")
	emitCmd.Flags().StringVar(&emitTags, "tags", "", "comma-separated tags")
	emitCmd.Flags().StringSliceVar(&emitTo, "to", nil, "recipient session IDs or branch names (can specify multiple)")
	emitCmd.MarkFlagRequired("type")
	emitCmd.MarkFlagRequired("title")
}

func runEmit(cmd *cobra.Command, args []string) error {
	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	eventID := uuid.New().String()
	ts := time.Now().UTC().Format(time.RFC3339)

	evt := db.Event{
		ID:        eventID,
		TS:        ts,
		Source:    emitSource,
		Type:      emitType,
		Title:     emitTitle,
		Broadcast: emitBroadcast,
	}

	if emitBody != "" {
		evt.Body = &emitBody
	}
	if emitSessionID != "" {
		evt.SessionID = &emitSessionID
	}
	if emitTags != "" {
		evt.Tags = &emitTags
	}

	var recipients []db.EventRecipient
	for _, to := range emitTo {
		recipientType, recipientValue, err := resolveRecipient(d, to)
		if err != nil {
			return err
		}
		recipients = append(recipients, db.EventRecipient{
			RecipientType:  recipientType,
			RecipientValue: recipientValue,
		})
	}

	if err := d.InsertEvent(evt, recipients, nil); err != nil {
		return fmt.Errorf("failed to insert event: %w", err)
	}

	if jsonOutput {
		output := map[string]interface{}{
			"event_id":  eventID,
			"timestamp": ts,
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("✓ Event emitted\n")
		fmt.Printf("  ID: %s\n", eventID)
		fmt.Printf("  Timestamp: %s\n", ts)
	}

	return nil
}

func resolveRecipient(d *db.DB, to string) (recipientType, recipientValue string, err error) {
	// UUID-shaped strings are session IDs
	if len(to) == 36 && strings.Count(to, "-") == 4 {
		return "session", to, nil
	}

	// repo:branch format
	if strings.Contains(to, ":") {
		return "branch", to, nil
	}

	// Try session name match
	nameRows, err := d.Conn().Query(
		`SELECT session_id FROM sessions WHERE session_name = ? AND status != 'archived'`, to)
	if err == nil {
		defer nameRows.Close()
		var sessionIDs []string
		for nameRows.Next() {
			var id string
			nameRows.Scan(&id)
			sessionIDs = append(sessionIDs, id)
		}
		if len(sessionIDs) == 1 {
			return "session", sessionIDs[0], nil
		}
		if len(sessionIDs) > 1 {
			return "", "", fmt.Errorf("multiple sessions named %q — use session ID instead", to)
		}
	}

	// Try branch name match
	branchRows, err := d.Conn().Query(
		`SELECT DISTINCT repo FROM sessions WHERE branch = ? AND status != 'archived'`, to)
	if err != nil {
		return "", "", fmt.Errorf("failed to look up %q: %w", to, err)
	}
	defer branchRows.Close()

	var repos []string
	for branchRows.Next() {
		var repo string
		branchRows.Scan(&repo)
		repos = append(repos, repo)
	}

	if len(repos) == 1 {
		return "branch", to, nil
	}
	if len(repos) > 1 {
		return "", "", fmt.Errorf("branch %q exists in multiple repos: %s. Use repo:branch format (e.g. %s:%s)",
			to, strings.Join(repos, ", "), repos[0], to)
	}

	return "", "", fmt.Errorf("no session or branch found matching %q", to)
}
