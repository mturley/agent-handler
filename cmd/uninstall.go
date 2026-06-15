package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	purgeFlagUninstall bool
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove agent-handler configuration",
	RunE:  runUninstall,
}

func init() {
	uninstallCmd.Flags().BoolVar(&purgeFlagUninstall, "purge", false, "remove all data including database")
	rootCmd.AddCommand(uninstallCmd)
}

func runUninstall(cmd *cobra.Command, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	agentHandlerDir := filepath.Join(home, ".agent-handler")
	claudeSkillsDir := filepath.Join(home, ".claude", "skills")

	// 1. Remove skill symlinks from ~/.claude/skills/
	skillNames := []string{
		"inbox",
		"inbox_mode",
		"handler_register",
		"handler_emit",
		"handler_subscribe",
		"handler_snapshot",
		"handler_unregister",
	}

	removedSymlinks := 0
	for _, skillName := range skillNames {
		dst := filepath.Join(claudeSkillsDir, skillName)

		// Check if it exists and is a symlink
		info, err := os.Lstat(dst)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			fmt.Printf("⚠ Could not check %s: %v\n", dst, err)
			continue
		}

		// Only remove if it's a symlink pointing to an agent-handler directory
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(dst)
			if err != nil {
				fmt.Printf("⚠ Could not read symlink %s: %v\n", dst, err)
				continue
			}

			// Check if target contains "agent-handler" to be safe
			if strings.Contains(target, "agent-handler") || strings.Contains(target, "agent-ledger") {
				if err := os.Remove(dst); err != nil {
					fmt.Printf("⚠ Could not remove symlink %s: %v\n", dst, err)
					continue
				}
				removedSymlinks++
			}
		}
	}

	if removedSymlinks > 0 {
		fmt.Printf("✓ Removed %d skill symlinks from %s\n", removedSymlinks, claudeSkillsDir)
	}

	// 2. Handle data directory based on --purge flag
	if purgeFlagUninstall {
		if _, err := os.Stat(agentHandlerDir); err == nil {
			if err := os.RemoveAll(agentHandlerDir); err != nil {
				return fmt.Errorf("failed to remove %s: %w", agentHandlerDir, err)
			}
			fmt.Printf("✓ Removed all data from %s\n", agentHandlerDir)
		}
	} else {
		if _, err := os.Stat(agentHandlerDir); err == nil {
			fmt.Printf("✓ Data preserved at %s (use --purge to remove)\n", agentHandlerDir)
		}
	}

	// 3. Print reminder about hooks
	fmt.Println("\n✓ Uninstallation complete!")
	fmt.Println("\nRemember to remove Claude Code hook registrations from ~/.claude/settings.json:")
	fmt.Println("  - AgentStart: handler register")
	fmt.Println("  - AgentStop: handler unregister")

	return nil
}
