package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Configure session settings",
	RunE:  runConfigure,
}

func init() {
	configureCmd.GroupID = "agent"
	rootCmd.AddCommand(configureCmd)
	configureCmd.Flags().String("session-id", "", "session ID (auto-detected if omitted)")
	configureCmd.Flags().String("inbox-mode", "", "inbox mode (manual, on-submit, auto)")
	configureCmd.Flags().Int("auto-poll-interval", 0, "auto-poll interval in seconds (for auto mode)")
	configureCmd.Flags().String("get", "", "get a specific setting value (inbox-mode, auto-poll-interval)")
}

func runConfigure(cmd *cobra.Command, args []string) error {
	sessionID, err := resolveSessionID(cmd)
	if err != nil {
		return fmt.Errorf("could not determine session: %w", err)
	}

	getFlag, _ := cmd.Flags().GetString("get")

	if getFlag != "" {
		d, err := openReadOnlyDB()
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer d.Close()

		session, err := d.GetSession(sessionID)
		if err != nil {
			return fmt.Errorf("failed to get session: %w", err)
		}

		switch getFlag {
		case "inbox-mode":
			fmt.Println(session.InboxMode)
		case "auto-poll-interval", "auto_poll_interval":
			if session.AutoPollInterval != nil {
				fmt.Println(*session.AutoPollInterval)
			} else {
				fmt.Println("null")
			}
		default:
			return fmt.Errorf("unknown setting: %s (valid: inbox-mode, auto-poll-interval)", getFlag)
		}
		return nil
	}

	inboxMode, _ := cmd.Flags().GetString("inbox-mode")
	autoPollInterval, _ := cmd.Flags().GetInt("auto-poll-interval")

	if inboxMode == "" && autoPollInterval == 0 {
		return fmt.Errorf("at least one of --inbox-mode or --auto-poll-interval must be provided")
	}

	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	session, err := d.GetSession(sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	finalInboxMode := session.InboxMode
	if inboxMode != "" {
		finalInboxMode = inboxMode
	}

	var finalAutoPoll *int
	if autoPollInterval > 0 {
		finalAutoPoll = &autoPollInterval
	} else {
		finalAutoPoll = session.AutoPollInterval
	}

	if err := d.ConfigureSession(sessionID, finalInboxMode, finalAutoPoll); err != nil {
		return fmt.Errorf("failed to configure session: %w", err)
	}

	if jsonOutput {
		output := map[string]interface{}{
			"session_id":         sessionID,
			"inbox_mode":         finalInboxMode,
			"auto_poll_interval": finalAutoPoll,
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("✓ Configured session %s\n", sessionID)
		fmt.Printf("  Inbox mode: %s\n", finalInboxMode)
		if finalAutoPoll != nil {
			fmt.Printf("  Auto-poll interval: %d seconds\n", *finalAutoPoll)
		}
	}

	return nil
}
