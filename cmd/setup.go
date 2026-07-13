package cmd

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var (
	embeddedSkills embed.FS
	embeddedHooks  embed.FS
	embeddedRules  embed.FS
)

func SetEmbedded(skills, hooks, rules embed.FS) {
	embeddedSkills = skills
	embeddedHooks = hooks
	embeddedRules = rules
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Set up or update agent-handler skills, hooks, and database",
	RunE:  runInstall,
}

func init() {
	setupCmd.GroupID = "admin"
	rootCmd.AddCommand(setupCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	handlerDir := db.HandlerHome()
	hooksDir := filepath.Join(handlerDir, "hooks")
	skillsDir := filepath.Join(handlerDir, "skills")
	rulesDir := filepath.Join(handlerDir, "rules")
	claudeSkillsDir := filepath.Join(home, ".claude", "skills")
	claudeRulesDir := filepath.Join(home, ".claude", "rules")
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	fmt.Println("agent-handler setup will:")
	fmt.Println("")
	fmt.Printf("  Create directory structure at %s\n", handlerDir)
	fmt.Printf("  Initialize SQLite database at %s\n", db.DefaultPath())
	fmt.Printf("  Extract hooks to %s\n", hooksDir)
	fmt.Printf("  Extract skills to %s\n", skillsDir)
	fmt.Printf("  Symlink %d skills into %s:\n", len(skillNames), claudeSkillsDir)
	for _, name := range skillNames {
		fmt.Printf("    - /%s\n", name)
	}
	fmt.Printf("  Install global rules to %s\n", claudeRulesDir)
	fmt.Printf("  Configure Claude Code hooks in %s:\n", settingsPath)
	for _, hook := range []string{"SessionEnd", "UserPromptSubmit", "PreCompact"} {
		fmt.Printf("    - %s\n", hook)
	}
	fmt.Printf("  Configure status line widget in %s\n", settingsPath)
	fmt.Println("  Offer to auto-allow handler CLI commands (Bash permission)")
	fmt.Println("  Offer to configure external service API tokens (GitHub, Jira)")
	fmt.Println("  Offer to install watchers for configured services")
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

	// 6. Extract rules and install to ~/.claude/rules/
	fmt.Println("")
	os.MkdirAll(rulesDir, 0755)
	os.MkdirAll(claudeRulesDir, 0755)
	ruleFiles, _ := fs.Glob(embeddedRules, "rules/*.md")
	for _, rulePath := range ruleFiles {
		data, err := fs.ReadFile(embeddedRules, rulePath)
		if err != nil {
			return fmt.Errorf("reading embedded rule %s: %w", rulePath, err)
		}
		baseName := filepath.Base(rulePath)
		// Extract to handler dir
		if err := os.WriteFile(filepath.Join(rulesDir, baseName), data, 0644); err != nil {
			return fmt.Errorf("writing rule %s: %w", baseName, err)
		}
		// Copy to ~/.claude/rules/
		if err := os.WriteFile(filepath.Join(claudeRulesDir, baseName), data, 0644); err != nil {
			return fmt.Errorf("installing rule %s: %w", baseName, err)
		}
		fmt.Printf("  ✓ Installed rule %s\n", baseName)
	}

	// 8. Configure Claude Code hooks and status line
	fmt.Println("")
	if err := configureHooks(home, hooksDir); err != nil {
		return fmt.Errorf("configuring hooks: %w", err)
	}
	if err := configureStatusLine(home); err != nil {
		return fmt.Errorf("configuring status line: %w", err)
	}

	// 9. Offer to auto-allow handler commands
	fmt.Println("")
	configurePermissions(home)

	// 10. Set up external service watchers (auth + install)
	fmt.Println("\nSetting up external service watchers...")
	watcherInstallCmd := exec.Command("handler", "watcher", "install")
	watcherInstallCmd.Stdin = os.Stdin
	watcherInstallCmd.Stdout = os.Stdout
	watcherInstallCmd.Stderr = os.Stderr
	watcherInstallCmd.Run()

	fmt.Println("\n✓ Installation complete!")
	fmt.Printf("\n  All files installed to %s\n", handlerDir)
	fmt.Println("  To update, run 'handler update'.")
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
		"SessionEnd":       "session_end.sh",
		"UserPromptSubmit": "user_prompt_submit.sh",
		"PreCompact":       "pre_compact.sh",
	}
	timeouts := map[string]int{
		"SessionEnd":       10,
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

func configureStatusLine(home string) error {
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	settings := make(map[string]interface{})
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("failed to parse %s: %w", settingsPath, err)
		}
	}

	handlerDir := db.HandlerHome()
	statuslineScript := filepath.Join(handlerDir, "hooks", "statusline.sh")

	settings["statusLine"] = map[string]interface{}{
		"type":            "command",
		"command":         statuslineScript,
		"refreshInterval": 10,
	}

	fmt.Printf("  ✓ Status line -> %s (refresh every 10s)\n", statuslineScript)

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(settingsPath, data, 0644)
}

func configurePermissions(home string) {
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	permission := "Bash(handler *)"

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return
	}
	settings := make(map[string]interface{})
	if err := json.Unmarshal(data, &settings); err != nil {
		return
	}

	perms, _ := settings["permissions"].(map[string]interface{})
	if perms != nil {
		if allow, ok := perms["allow"].([]interface{}); ok {
			for _, p := range allow {
				if s, ok := p.(string); ok && s == permission {
					fmt.Printf("  ✓ Permission already configured: %s\n", permission)
					return
				}
			}
		}
	}

	fmt.Printf("  Auto-allow all handler CLI commands in Claude Code sessions?\n")
	fmt.Printf("  This adds \"%s\" to your Claude Code permissions so agents\n", permission)
	fmt.Printf("  can run handler commands without prompting for approval.\n\n")
	if !confirm("  Add permission?") {
		fmt.Println("  Skipped. You can add it manually later in ~/.claude/settings.json")
		return
	}

	if perms == nil {
		perms = make(map[string]interface{})
	}
	allow, _ := perms["allow"].([]interface{})
	allow = append(allow, permission)
	perms["allow"] = allow
	settings["permissions"] = perms

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return
	}
	out = append(out, '\n')
	os.WriteFile(settingsPath, out, 0644)
	fmt.Printf("  ✓ Added permission: %s\n", permission)
}
