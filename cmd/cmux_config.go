package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const cmuxConfigPath = ".config/cmux/cmux.json"

var handlerCmuxActions = map[string]map[string]interface{}{
	"handler-switch-to-awaiting": {
		"type":     "command",
		"title":    "agent-handler: Switch to Awaiting Session",
		"subtitle": "Jump to the first session awaiting approval",
		"command":  "handler switch -a; exit",
		"shortcut": "cmd+shift+a",
		"palette":  true,
	},
	"handler-switch-to-session": {
		"type":     "command",
		"title":    "agent-handler: Switch to Session",
		"subtitle": "Interactive session switcher with tab completion",
		"command":  "handler switch; exit",
		"shortcut": "cmd+shift+s",
		"palette":  true,
	},
}

var handlerCmuxActionIDs = []string{"handler-switch-to-awaiting", "handler-switch-to-session"}

func cmuxConfigFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, cmuxConfigPath)
}

func findCmuxSettings() string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".agents/skills/cmux-settings/scripts/cmux-settings"),
		filepath.Join(home, ".codex/skills/cmux-settings/scripts/cmux-settings"),
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.Mode()&0111 != 0 {
			return c
		}
	}
	return ""
}

func configureCmuxActions() {
	cmuxSettings := findCmuxSettings()
	if cmuxSettings == "" {
		fmt.Println("  \033[2mcmux-settings helper not found, skipping cmux action configuration\033[0m")
		return
	}

	if os.Getenv("CMUX_SURFACE_ID") == "" {
		fmt.Println("\n  \033[33m⚠ Not running inside cmux.\033[0m cmux actions cannot be configured from outside cmux.")
		if !setupYes && !confirm("  Skip cmux action setup and continue?") {
			fmt.Println("  Aborted. Re-run handler setup from inside cmux to configure actions.")
			os.Exit(1)
		}
		fmt.Println("  Skipping cmux action setup. Run 'handler setup' from inside cmux later.")
		return
	}

	// Check if actions already exist
	out, _ := exec.Command(cmuxSettings, "get", "actions").Output()
	if len(out) > 0 {
		var existing map[string]interface{}
		if json.Unmarshal(out, &existing) == nil {
			allPresent := true
			for _, id := range handlerCmuxActionIDs {
				if _, ok := existing[id]; !ok {
					allPresent = false
					break
				}
			}
			if allPresent {
				fmt.Println("  ✓ cmux actions already configured (handler-switch-to-awaiting, handler-switch-to-session)")
				return
			}
		}
	}

	// Set each action
	for _, id := range handlerCmuxActionIDs {
		actionJSON, _ := json.Marshal(handlerCmuxActions[id])
		key := "actions." + id
		if err := exec.Command(cmuxSettings, "set", key, string(actionJSON)).Run(); err != nil {
			fmt.Printf("  ⚠ Failed to set cmux action %s: %v\n", id, err)
			return
		}
	}

	exec.Command("cmux", "reload-config").Run()
	fmt.Println("  ✓ Added cmux actions: handler-switch-to-awaiting (cmd+shift+a), handler-switch-to-session (cmd+shift+s)")
}

func hasCmuxActions() bool {
	cmuxSettings := findCmuxSettings()
	if cmuxSettings == "" {
		return false
	}
	out, _ := exec.Command(cmuxSettings, "get", "actions").Output()
	if len(out) == 0 {
		return false
	}
	var existing map[string]interface{}
	if json.Unmarshal(out, &existing) != nil {
		return false
	}
	for _, id := range handlerCmuxActionIDs {
		if _, ok := existing[id]; ok {
			return true
		}
	}
	return false
}

func removeCmuxActions() {
	cmuxSettings := findCmuxSettings()
	if cmuxSettings == "" {
		return
	}

	if os.Getenv("CMUX_SURFACE_ID") == "" {
		fmt.Println("\n  \033[33m⚠ Not running inside cmux.\033[0m cmux actions cannot be removed from outside cmux.")
		if !confirm("  Skip cmux action removal and continue?") {
			fmt.Println("  Aborted. Re-run handler uninstall from inside cmux to remove actions.")
			os.Exit(1)
		}
		fmt.Println("  Skipping cmux action removal. Run 'handler uninstall' from inside cmux later.")
		return
	}

	// Check if any of our actions exist
	out, _ := exec.Command(cmuxSettings, "get", "actions").Output()
	if len(out) == 0 {
		return
	}
	var existing map[string]interface{}
	if json.Unmarshal(out, &existing) != nil {
		return
	}

	found := false
	for _, id := range handlerCmuxActionIDs {
		if _, ok := existing[id]; ok {
			found = true
			break
		}
	}
	if !found {
		return
	}

	for _, id := range handlerCmuxActionIDs {
		exec.Command(cmuxSettings, "unset", "actions."+id).Run()
	}

	exec.Command("cmux", "reload-config").Run()
	fmt.Println("  ✓ Removed cmux actions (handler-switch-to-awaiting, handler-switch-to-session)")
}
