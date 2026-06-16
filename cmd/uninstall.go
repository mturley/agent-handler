package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove agent-handler configuration",
	RunE:  runUninstall,
}

func init() {
	uninstallCmd.GroupID = "admin"
	rootCmd.AddCommand(uninstallCmd)
}

var skillNames = []string{
	"inbox",
	"inbox_mode",
	"handler_emit",
	"handler_subscribe",
	"handler_snapshot",
}

func runUninstall(cmd *cobra.Command, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	agentHandlerDir := db.HandlerHome()
	claudeSkillsDir := filepath.Join(home, ".claude", "skills")
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	// Summarize what will be done
	fmt.Println("agent-handler uninstall will:")
	fmt.Println("")

	// Check skill symlinks
	symlinkTargets := findAgentHandlerSkills(claudeSkillsDir)
	if len(symlinkTargets) > 0 {
		fmt.Printf("  Remove %d skill symlinks from %s:\n", len(symlinkTargets), claudeSkillsDir)
		for _, name := range symlinkTargets {
			fmt.Printf("    - %s\n", name)
		}
	}

	// Check hooks
	hookNames := findAgentHandlerHooks(settingsPath)
	if len(hookNames) > 0 {
		fmt.Printf("  Remove %d hooks from %s:\n", len(hookNames), settingsPath)
		for _, name := range hookNames {
			fmt.Printf("    - %s\n", name)
		}
	}

	// Detect the binary location
	binaryPath, _ := os.Executable()
	realBinaryPath, _ := filepath.EvalSymlinks(binaryPath)
	if realBinaryPath != "" {
		if binaryPath != realBinaryPath {
			fmt.Printf("  Remove %s -> %s\n", binaryPath, realBinaryPath)
		} else {
			fmt.Printf("  Remove %s\n", realBinaryPath)
		}
	}

	hooksPath := filepath.Join(agentHandlerDir, "hooks")
	skillsPath := filepath.Join(agentHandlerDir, "skills")
	if _, err := os.Stat(hooksPath); err == nil {
		fmt.Printf("  Remove extracted hooks from %s\n", hooksPath)
	}
	if _, err := os.Stat(skillsPath); err == nil {
		fmt.Printf("  Remove extracted skills from %s\n", skillsPath)
	}

	if hasHandlerPermission(settingsPath) {
		fmt.Printf("  Remove Bash(handler *) permission from %s\n", settingsPath)
	}

	fmt.Println("")

	if !confirm("Proceed?") {
		fmt.Println("Aborted.")
		return nil
	}
	fmt.Println("")

	for _, name := range symlinkTargets {
		dst := filepath.Join(claudeSkillsDir, name)
		os.Remove(dst)
		fmt.Printf("  ✓ Removed skill symlink %s\n", name)
	}

	if len(hookNames) > 0 {
		removeHooks(home)
	}

	// Remove binary last (since we're running from it)
	if realBinaryPath != "" {
		if binaryPath != realBinaryPath {
			os.Remove(binaryPath)
			fmt.Printf("  ✓ Removed %s\n", binaryPath)
		}
		os.Remove(realBinaryPath)
		fmt.Printf("  ✓ Removed %s\n", realBinaryPath)
	}

	// Remove extracted hooks and skills from ~/.agent-handler
	for _, dir := range []string{"hooks", "skills"} {
		dirPath := filepath.Join(agentHandlerDir, dir)
		if _, err := os.Stat(dirPath); err == nil {
			os.RemoveAll(dirPath)
			fmt.Printf("  ✓ Removed %s\n", dirPath)
		}
	}

	fmt.Println("\n✓ Uninstallation complete!")
	dataDir := filepath.Join(agentHandlerDir, "data")
	if _, err := os.Stat(dataDir); err == nil {
		fmt.Printf("\n  Your event history, session data, and database are still at %s\n", dataDir)
		fmt.Println("  To fully remove all data: rm -rf ~/.agent-handler")
	}
	return nil
}

func findAgentHandlerSkills(claudeSkillsDir string) []string {
	var found []string
	for _, name := range skillNames {
		dst := filepath.Join(claudeSkillsDir, name)
		info, err := os.Lstat(dst)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(dst)
			if err != nil {
				continue
			}
			if strings.Contains(target, "agent-handler") {
				found = append(found, name)
			}
		}
	}
	return found
}

func findAgentHandlerHooks(settingsPath string) []string {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil
	}
	settings := make(map[string]interface{})
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil
	}
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		return nil
	}

	var found []string
	for _, event := range []string{"SessionStart", "SessionEnd", "UserPromptSubmit", "PreCompact"} {
		existing, ok := hooks[event].([]interface{})
		if !ok {
			continue
		}
		for _, group := range existing {
			if isAgentHandlerHook(group) {
				found = append(found, event)
				break
			}
		}
	}
	return found
}

func removeHooks(home string) error {
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return err
	}

	settings := make(map[string]interface{})
	if err := json.Unmarshal(data, &settings); err != nil {
		return err
	}

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		return nil
	}

	for _, event := range []string{"SessionStart", "SessionEnd", "UserPromptSubmit", "PreCompact"} {
		existing, ok := hooks[event].([]interface{})
		if !ok {
			continue
		}
		var kept []interface{}
		removed := false
		for _, group := range existing {
			if isAgentHandlerHook(group) {
				removed = true
			} else {
				kept = append(kept, group)
			}
		}
		if removed {
			if len(kept) == 0 {
				delete(hooks, event)
			} else {
				hooks[event] = kept
			}
			fmt.Printf("  ✓ Removed %s hook\n", event)
		}
	}

	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}

	// Remove status line if it's ours
	if sl, ok := settings["statusLine"]; ok {
		if isAgentHandlerHook(sl) {
			delete(settings, "statusLine")
			fmt.Println("  ✓ Removed status line configuration")
		}
	}

	// Remove handler permission
	if perms, ok := settings["permissions"].(map[string]interface{}); ok {
		if allow, ok := perms["allow"].([]interface{}); ok {
			var kept []interface{}
			for _, p := range allow {
				if s, ok := p.(string); ok && s == "Bash(handler *)" {
					continue
				}
				kept = append(kept, p)
			}
			if len(kept) != len(allow) {
				perms["allow"] = kept
				fmt.Println("  ✓ Removed Bash(handler *) permission")
			}
		}
	}

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(settingsPath, out, 0644)
}

func hasHandlerPermission(settingsPath string) bool {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return false
	}
	settings := make(map[string]interface{})
	if err := json.Unmarshal(data, &settings); err != nil {
		return false
	}
	perms, _ := settings["permissions"].(map[string]interface{})
	if perms == nil {
		return false
	}
	allow, _ := perms["allow"].([]interface{})
	for _, p := range allow {
		if s, ok := p.(string); ok && s == "Bash(handler *)" {
			return true
		}
	}
	return false
}

func isAgentHandlerHook(hookConfig interface{}) bool {
	data, err := json.Marshal(hookConfig)
	if err != nil {
		return false
	}
	s := string(data)
	return strings.Contains(s, "agent-handler")
}

func confirm(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}
