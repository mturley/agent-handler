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

var (
	cfgSessionID       string
	cfgInboxMode       string
	cfgAutoPollInterval int
	cfgGet             string
)

func init() {
	rootCmd.AddCommand(configureCmd)
	configureCmd.Flags().StringVar(&cfgSessionID, "session-id", "", "session ID")
	configureCmd.Flags().StringVar(&cfgInboxMode, "inbox-mode", "", "inbox mode (manual, on-submit, auto)")
	configureCmd.Flags().IntVar(&cfgAutoPollInterval, "auto-poll-interval", 0, "auto-poll interval in seconds (for auto mode)")
	configureCmd.Flags().StringVar(&cfgGet, "get", "", "get a specific setting value (inbox_mode or auto_poll_interval)")
}

func runConfigure(cmd *cobra.Command, args []string) error {
	// --get mode: read and output a single value
	if cfgGet != "" {
		if cfgSessionID == "" {
			return fmt.Errorf("--session-id is required when using --get")
		}

		d, err := openReadOnlyDB()
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer d.Close()

		session, err := d.GetSession(cfgSessionID)
		if err != nil {
			return fmt.Errorf("failed to get session: %w", err)
		}
		if session == nil {
			return fmt.Errorf("session not found: %s", cfgSessionID)
		}

		switch cfgGet {
		case "inbox_mode":
			fmt.Println(session.InboxMode)
		case "auto_poll_interval":
			if session.AutoPollInterval != nil {
				fmt.Println(*session.AutoPollInterval)
			} else {
				fmt.Println("null")
			}
		default:
			return fmt.Errorf("unknown setting: %s (valid: inbox_mode, auto_poll_interval)", cfgGet)
		}
		return nil
	}

	// Set mode
	if cfgSessionID == "" {
		return fmt.Errorf("--session-id is required")
	}
	if cfgInboxMode == "" && cfgAutoPollInterval == 0 {
		return fmt.Errorf("at least one of --inbox-mode or --auto-poll-interval must be provided")
	}

	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	// Get current session to preserve unset values
	session, err := d.GetSession(cfgSessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	if session == nil {
		return fmt.Errorf("session not found: %s", cfgSessionID)
	}

	// Apply changes
	inboxMode := session.InboxMode
	if cfgInboxMode != "" {
		inboxMode = cfgInboxMode
	}

	var autoPollInterval *int
	if cfgAutoPollInterval > 0 {
		autoPollInterval = &cfgAutoPollInterval
	} else {
		autoPollInterval = session.AutoPollInterval
	}

	err = d.ConfigureSession(cfgSessionID, inboxMode, autoPollInterval)
	if err != nil {
		return fmt.Errorf("failed to configure session: %w", err)
	}

	if jsonOutput {
		output := map[string]interface{}{
			"session_id":         cfgSessionID,
			"inbox_mode":         inboxMode,
			"auto_poll_interval": autoPollInterval,
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("✓ Configured session %s\n", cfgSessionID)
		fmt.Printf("  Inbox mode: %s\n", inboxMode)
		if autoPollInterval != nil {
			fmt.Printf("  Auto-poll interval: %d seconds\n", *autoPollInterval)
		}
	}

	return nil
}
