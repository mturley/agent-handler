package watcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var logsLines int

func init() {
	WatcherCmd.AddCommand(logsCmd)
	logsCmd.Flags().IntVar(&logsLines, "lines", 50, "Number of lines to show from the end of the log")
}

var logsCmd = &cobra.Command{
	Use:   "logs <name>",
	Short: "View watcher logs",
	Long: `View the most recent log entries for a watcher.

Valid watchers: github, jira

Logs are stored at ~/.agent-handler/data/logs/watcher-<name>.log`,
	Args: cobra.ExactArgs(1),
	RunE: showLogs,
}

func showLogs(cmd *cobra.Command, args []string) error {
	name := strings.ToLower(args[0])

	// Validate watcher name
	if name != "github" && name != "jira" {
		return fmt.Errorf("unknown watcher: %s (must be 'github' or 'jira')", name)
	}

	// Get log file path
	logPath := filepath.Join(db.HandlerHome(), "data", "logs", fmt.Sprintf("watcher-%s.log", name))

	// Check if log file exists
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		fmt.Printf("No logs found for watcher %q\n", name)
		fmt.Printf("Log file does not exist: %s\n", logPath)
		return nil
	}

	// Read the entire file
	data, err := os.ReadFile(logPath)
	if err != nil {
		return fmt.Errorf("failed to read log file: %w", err)
	}

	// Split into lines
	lines := strings.Split(string(data), "\n")

	// Get last N lines (excluding empty trailing line)
	startIdx := len(lines) - logsLines - 1
	if startIdx < 0 {
		startIdx = 0
	}

	// Remove empty trailing line if present
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// Print lines
	tailLines := lines[startIdx:]
	if len(tailLines) == 0 {
		fmt.Printf("No log entries found for watcher %q\n", name)
		return nil
	}

	for _, line := range tailLines {
		fmt.Println(line)
	}

	return nil
}
