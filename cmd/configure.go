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
	configureCmd.Flags().String("role", "", "session role (handler, or empty to clear)")
	configureCmd.Flags().String("get", "", "get a specific setting value (inbox-mode, auto-poll-interval, role)")
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
		case "role":
			fmt.Println(session.Role)
		default:
			return fmt.Errorf("unknown setting: %s (valid: inbox-mode, auto-poll-interval, role)", getFlag)
		}
		return nil
	}

	inboxMode, _ := cmd.Flags().GetString("inbox-mode")
	autoPollInterval, _ := cmd.Flags().GetInt("auto-poll-interval")
	roleFlag, _ := cmd.Flags().GetString("role")

	if inboxMode == "" && autoPollInterval == 0 && !cmd.Flags().Changed("role") {
		return fmt.Errorf("at least one of --inbox-mode, --auto-poll-interval, or --role must be provided")
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

	var finalRole *string
	if cmd.Flags().Changed("role") {
		if roleFlag == "" {
			// Explicit empty string means clear the role
			finalRole = new(string)
		} else {
			finalRole = &roleFlag
		}
	}

	if err := d.ConfigureSession(sessionID, finalInboxMode, finalAutoPoll, finalRole); err != nil {
		return fmt.Errorf("failed to configure session: %w", err)
	}

	// Re-fetch session to get final state
	session, err = d.GetSession(sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	if jsonOutput {
		output := map[string]interface{}{
			"session_id":         sessionID,
			"inbox_mode":         session.InboxMode,
			"auto_poll_interval": session.AutoPollInterval,
			"role":               session.Role,
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("✓ Configured session %s\n", sessionID)
		fmt.Printf("  Inbox mode: %s\n", session.InboxMode)
		if session.AutoPollInterval != nil {
			fmt.Printf("  Auto-poll interval: %d seconds\n", *session.AutoPollInterval)
		}
		if session.Role != "" {
			fmt.Printf("  Role: %s\n", session.Role)
		}
	}

	return nil
}
