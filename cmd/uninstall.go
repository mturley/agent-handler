package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove agent-handler configuration",
	RunE:  runUninstall,
}

func init() {
	uninstallCmd.Flags().Bool("purge", false, "Also remove ~/.agent-handler/ and all data")
	rootCmd.AddCommand(uninstallCmd)
}

var skillNames = []string{
	"inbox",
	"inbox_mode",
	"handler_register",
	"handler_emit",
	"handler_subscribe",
	"handler_snapshot",
	"handler_unregister",
}

func runUninstall(cmd *cobra.Command, args []string) error {
	purge, _ := cmd.Flags().GetBool("purge")
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	agentHandlerDir := filepath.Join(home, ".agent-handler")
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

	// Check /usr/local/bin symlink
	usrLocalBin := "/usr/local/bin/handler"
	removeUsrLocalBin := false
	if target, err := os.Readlink(usrLocalBin); err == nil {
		if strings.Contains(target, ".agent-handler") || strings.Contains(target, "agent-handler") || strings.Contains(target, "agent-ledger") {
			fmt.Printf("  Remove symlink %s -> %s\n", usrLocalBin, target)
			removeUsrLocalBin = true
		}
	}

	if purge {
		if _, err := os.Stat(agentHandlerDir); err == nil {
			fmt.Printf("  Delete %s (database, sessions, hooks, skills, binary)\n", agentHandlerDir)
		}
	} else {
		fmt.Printf("  Preserve data at %s (use --purge to remove)\n", agentHandlerDir)
	}

	fmt.Println("")

	// Ask for confirmation
	if !confirm("Proceed?") {
		fmt.Println("Aborted.")
		return nil
	}
	fmt.Println("")

	// Execute
	for _, name := range symlinkTargets {
		dst := filepath.Join(claudeSkillsDir, name)
		os.Remove(dst)
		fmt.Printf("  ✓ Removed skill symlink %s\n", name)
	}

	if len(hookNames) > 0 {
		removeHooks(home)
	}

	if removeUsrLocalBin {
		os.Remove(usrLocalBin)
		fmt.Printf("  ✓ Removed symlink %s\n", usrLocalBin)
	}

	if purge {
		if _, err := os.Stat(agentHandlerDir); err == nil {
			os.RemoveAll(agentHandlerDir)
			fmt.Printf("  ✓ Removed %s\n", agentHandlerDir)
		}
	}

	fmt.Println("\n✓ Uninstallation complete!")
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
			if strings.Contains(target, "agent-handler") || strings.Contains(target, "agent-ledger") {
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
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "PreCompact"} {
		if hookConfig, exists := hooks[event]; exists && isAgentHandlerHook(hookConfig) {
			found = append(found, event)
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

	for _, event := range []string{"SessionStart", "UserPromptSubmit", "PreCompact"} {
		if hookConfig, exists := hooks[event]; exists && isAgentHandlerHook(hookConfig) {
			delete(hooks, event)
			fmt.Printf("  ✓ Removed %s hook\n", event)
		}
	}

	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(settingsPath, out, 0644)
}

func isAgentHandlerHook(hookConfig interface{}) bool {
	data, err := json.Marshal(hookConfig)
	if err != nil {
		return false
	}
	s := string(data)
	return strings.Contains(s, "agent-handler") || strings.Contains(s, "agent-ledger") ||
		strings.Contains(s, "session_start.sh") || strings.Contains(s, "user_prompt_submit.sh") ||
		strings.Contains(s, "pre_compact.sh")
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
