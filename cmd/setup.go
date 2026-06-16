package cmd

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var (
	embeddedSkills embed.FS
	embeddedHooks  embed.FS
)

func SetEmbedded(skills, hooks embed.FS) {
	embeddedSkills = skills
	embeddedHooks = hooks
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Set up or update agent-handler skills, hooks, and database",
	RunE:  runInstall,
}

func init() {
	rootCmd.AddCommand(setupCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	handlerDir := filepath.Join(home, ".agent-handler")
	hooksDir := filepath.Join(handlerDir, "hooks")
	skillsDir := filepath.Join(handlerDir, "skills")
	claudeSkillsDir := filepath.Join(home, ".claude", "skills")
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	fmt.Println("agent-handler setup will:")
	fmt.Println("")
	fmt.Printf("  Create directory structure at %s\n", handlerDir)
	fmt.Printf("  Initialize SQLite database at %s\n", db.DefaultPath())
	fmt.Printf("  Extract hooks to %s\n", hooksDir)
	fmt.Printf("  Extract skills to %s\n", skillsDir)
	fmt.Printf("  Symlink %d skills into %s\n", len(skillNames), claudeSkillsDir)
	fmt.Printf("  Configure 3 Claude Code hooks in %s\n", settingsPath)
	fmt.Println("")

	if !confirm("Proceed?") {
		fmt.Println("Aborted.")
		return nil
	}
	fmt.Println("")

	// 1. Create directory structure
	dataDir := filepath.Join(handlerDir, "data")
	for _, dir := range []string{handlerDir, dataDir, filepath.Join(dataDir, "sessions"), filepath.Join(dataDir, "logs"), hooksDir, skillsDir} {
		os.MkdirAll(dir, 0755)
	}
	fmt.Printf("  ✓ Created directory structure at %s\n", handlerDir)

	// 2. Initialize database
	database, err := db.Open(db.DefaultPath())
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	database.Close()
	fmt.Printf("  ✓ Initialized database at %s\n\n", db.DefaultPath())

	// 3. Extract hooks
	hookFiles, err := fs.Glob(embeddedHooks, "hooks/*.sh")
	if err != nil || len(hookFiles) == 0 {
		return fmt.Errorf("no hooks found in embedded data")
	}
	for _, hookPath := range hookFiles {
		data, err := fs.ReadFile(embeddedHooks, hookPath)
		if err != nil {
			return fmt.Errorf("reading embedded hook %s: %w", hookPath, err)
		}
		dst := filepath.Join(hooksDir, filepath.Base(hookPath))
		if err := os.WriteFile(dst, data, 0755); err != nil {
			return fmt.Errorf("writing hook %s: %w", dst, err)
		}
		fmt.Printf("  ✓ Extracted %s\n", filepath.Base(hookPath))
	}

	// 4. Clean stale skills from previous installs
	fmt.Println("")
	currentSkills := make(map[string]bool)
	for _, name := range skillNames {
		currentSkills[name] = true
	}
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() && !currentSkills[entry.Name()] {
				os.RemoveAll(filepath.Join(skillsDir, entry.Name()))
				staleSymlink := filepath.Join(claudeSkillsDir, entry.Name())
				if info, err := os.Lstat(staleSymlink); err == nil && info.Mode()&os.ModeSymlink != 0 {
					os.Remove(staleSymlink)
				}
				fmt.Printf("  ✓ Removed stale skill %s\n", entry.Name())
			}
		}
	}

	// 5. Extract skills and create symlinks
	os.MkdirAll(claudeSkillsDir, 0755)
	for _, skillName := range skillNames {
		skillSrcPath := filepath.Join("skills", skillName, "SKILL.md")
		data, err := fs.ReadFile(embeddedSkills, skillSrcPath)
		if err != nil {
			return fmt.Errorf("reading embedded skill %s: %w", skillName, err)
		}

		dstDir := filepath.Join(skillsDir, skillName)
		os.MkdirAll(dstDir, 0755)
		if err := os.WriteFile(filepath.Join(dstDir, "SKILL.md"), data, 0644); err != nil {
			return fmt.Errorf("writing skill %s: %w", skillName, err)
		}

		symlinkDst := filepath.Join(claudeSkillsDir, skillName)
		if _, err := os.Lstat(symlinkDst); err == nil {
			os.Remove(symlinkDst)
		}
		if err := os.Symlink(dstDir, symlinkDst); err != nil {
			return fmt.Errorf("symlinking skill %s: %w", skillName, err)
		}
		fmt.Printf("  ✓ %s -> %s\n", skillName, dstDir)
	}

	// 6. Configure Claude Code hooks
	fmt.Println("")
	if err := configureHooks(home, hooksDir); err != nil {
		return fmt.Errorf("configuring hooks: %w", err)
	}

	fmt.Println("\n✓ Installation complete!")
	fmt.Printf("\n  All files installed to %s\n", handlerDir)
	fmt.Println("  To update: 'go install github.com/mturley/agent-handler@latest && handler setup'")
	fmt.Println("  To uninstall, run 'handler uninstall'.")
	fmt.Println("\nTest with: handler status")
	return nil
}

func configureHooks(home, hooksDir string) error {
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	settings := make(map[string]interface{})
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("failed to parse %s: %w", settingsPath, err)
		}
	}

	hookEntries := map[string]string{
		"SessionStart":     "session_start.sh",
		"UserPromptSubmit": "user_prompt_submit.sh",
		"PreCompact":       "pre_compact.sh",
	}
	timeouts := map[string]int{
		"SessionStart":     10,
		"UserPromptSubmit": 5,
		"PreCompact":       10,
	}

	existingHooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		existingHooks = make(map[string]interface{})
	}

	for event, script := range hookEntries {
		scriptPath := filepath.Join(hooksDir, script)
		newMatcherGroup := map[string]interface{}{
			"matcher": "",
			"hooks": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": scriptPath,
					"timeout": timeouts[event],
				},
			},
		}

		// Preserve existing matcher groups from other tools, remove any existing agent-handler ones
		var kept []interface{}
		if existing, ok := existingHooks[event].([]interface{}); ok {
			for _, group := range existing {
				if !isAgentHandlerHook(group) {
					kept = append(kept, group)
				}
			}
		}
		existingHooks[event] = append(kept, newMatcherGroup)
		fmt.Printf("  ✓ %s -> %s\n", event, scriptPath)
	}

	settings["hooks"] = existingHooks

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(settingsPath, data, 0644)
}
