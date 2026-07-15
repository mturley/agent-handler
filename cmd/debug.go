package cmd

import (
	"fmt"

	"github.com/mturley/agent-handler/config"
	"github.com/spf13/cobra"
)

var debugCmd = &cobra.Command{
	Use:   "debug [enable|disable]",
	Short: "Toggle debug info in the statusline",
	Args:  cobra.ExactArgs(1),
	RunE:  runDebug,
}

func init() {
	debugCmd.GroupID = "admin"
	rootCmd.AddCommand(debugCmd)
}

func runDebug(cmd *cobra.Command, args []string) error {
	action := args[0]
	if action != "enable" && action != "disable" {
		return fmt.Errorf("usage: handler debug [enable|disable]")
	}

	cfgPath := config.DefaultPath()
	cfg, err := config.Read(cfgPath)
	if err != nil {
		cfg = &config.Config{}
	}

	cfg.Debug = action == "enable"

	if err := config.Write(cfgPath, cfg); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	if cfg.Debug {
		fmt.Println("✓ Debug mode enabled — statusline will show debug info for all sessions")
	} else {
		fmt.Println("✓ Debug mode disabled")
	}
	return nil
}
