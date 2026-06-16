package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Set up agent-handler for the current user",
	RunE:  runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	// 1. Create directory structure
	agentHandlerDir := filepath.Join(home, ".agent-handler")
	sessionsDir := filepath.Join(agentHandlerDir, "sessions")
	logsDir := filepath.Join(agentHandlerDir, "logs")

	for _, dir := range []string{agentHandlerDir, sessionsDir, logsDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	fmt.Printf("✓ Created directory structure at %s\n", agentHandlerDir)

	// 2. Initialize database
	dbPath := db.DefaultPath()
	database, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	if err := database.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}
	fmt.Printf("✓ Initialized database at %s\n", dbPath)

	// 3. Symlink skills
	claudeSkillsDir := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(claudeSkillsDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude/skills directory: %w", err)
	}

	// Find skills directory relative to binary location.
	// The binary lives at <repo>/bin/handler (possibly symlinked from /usr/local/bin).
	// Resolve the real path to find the repo root.
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not determine binary location: %w", err)
	}
	realPath, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("could not resolve binary symlink: %w", err)
	}

	skillNames := []string{
		"inbox",
		"inbox_mode",
		"handler_register",
		"handler_emit",
		"handler_subscribe",
		"handler_snapshot",
		"handler_unregister",
	}

	// Try: <real-binary-dir>/../skills, <real-binary-dir>/skills, <real-binary-dir>/../../skills
	realDir := filepath.Dir(realPath)
	candidates := []string{
		filepath.Join(realDir, "..", "skills"),
		filepath.Join(realDir, "skills"),
		filepath.Join(realDir, "..", "..", "skills"),
	}

	var foundSkillsDir string
	for _, candidate := range candidates {
		resolved, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if info, err := os.Stat(resolved); err == nil && info.IsDir() {
			foundSkillsDir = resolved
			break
		}
	}

	if foundSkillsDir == "" {
		return fmt.Errorf("could not locate skills directory relative to binary at %s (resolved: %s)", execPath, realPath)
	}

	// Create symlinks for each skill
	symlinkCount := 0
	for _, skillName := range skillNames {
		src := filepath.Join(foundSkillsDir, skillName)
		dst := filepath.Join(claudeSkillsDir, skillName)

		// Check if skill source exists
		if _, err := os.Stat(src); os.IsNotExist(err) {
			fmt.Printf("⚠ Skill not found: %s\n", src)
			continue
		}

		// Remove existing symlink or file if present
		if _, err := os.Lstat(dst); err == nil {
			if err := os.Remove(dst); err != nil {
				fmt.Printf("⚠ Could not remove existing file at %s: %v\n", dst, err)
				continue
			}
		}

		// Create symlink
		if err := os.Symlink(src, dst); err != nil {
			fmt.Printf("⚠ Could not create symlink for %s: %v\n", skillName, err)
			continue
		}
		symlinkCount++
	}

	if symlinkCount > 0 {
		fmt.Printf("✓ Created %d skill symlinks in %s\n", symlinkCount, claudeSkillsDir)
	}

	// 4. Print summary and next steps
	fmt.Println("\n✓ Installation complete!")
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Register Claude Code hooks manually by adding to ~/.claude/settings.json:")
	fmt.Println("     - AgentStart: handler register")
	fmt.Println("     - AgentStop: handler unregister")
	fmt.Println("  2. Test with: handler status")
	fmt.Println("\nFor more information, see: handler --help")

	return nil
}
