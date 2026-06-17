package watcher

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mturley/agent-handler/config"
	"github.com/spf13/cobra"
)

func init() {
	authCmd.AddCommand(authStatusCmd)
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show authentication status for external services",
	RunE:  runAuthStatus,
}

type authStatus struct {
	GitHub bool `json:"github"`
	Jira   bool `json:"jira"`
}

func runAuthStatus(cmd *cobra.Command, args []string) error {
	configPath := config.DefaultPath()
	cfg, err := config.Read(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	status := authStatus{
		GitHub: cfg.IsServiceConfigured("github"),
		Jira:   cfg.IsServiceConfigured("jira"),
	}

	if *JSONOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}

	// Human-readable output
	if status.GitHub {
		fmt.Println("✓ GitHub: configured")
	} else {
		fmt.Println("✗ GitHub: not configured")
	}

	if status.Jira {
		fmt.Println("✓ Jira: configured")
	} else {
		fmt.Println("✗ Jira: not configured")
	}

	return nil
}
