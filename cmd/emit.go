package cmd

import (
	"encoding/json"
	"fmt"
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
)

func init() {
	rootCmd.AddCommand(emitCmd)
	emitCmd.Flags().StringVar(&emitType, "type", "", "event type (required)")
	emitCmd.Flags().StringVar(&emitTitle, "title", "", "event title (required)")
	emitCmd.Flags().StringVar(&emitBody, "body", "", "event body")
	emitCmd.Flags().StringVar(&emitSessionID, "session-id", "", "target session ID")
	emitCmd.Flags().StringVar(&emitSource, "source", "agent", "event source")
	emitCmd.Flags().BoolVar(&emitBroadcast, "broadcast", false, "broadcast to all sessions")
	emitCmd.Flags().StringVar(&emitTags, "tags", "", "comma-separated tags")
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

	if err := d.InsertEvent(evt, nil, nil); err != nil {
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
