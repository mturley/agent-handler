package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const cmuxConfigPath = ".config/cmux/cmux.json"

var handlerCmuxActions = map[string]map[string]interface{}{
	"handler-switch-to-awaiting": {
		"type":     "command",
		"title":    "agent-handler: Switch to Awaiting Session",
		"subtitle": "Jump to the first session awaiting approval",
		"command":  "handler switch -a --close-caller",
		"shortcut": "cmd+shift+a",
		"palette":  true,
	},
	"handler-switch-to-session": {
		"type":     "command",
		"title":    "agent-handler: Switch to Session",
		"subtitle": "Interactive session switcher with tab completion",
		"command":  "handler switch --close-caller",
		"shortcut": "cmd+shift+s",
		"palette":  true,
	},
	"handler-switch-to-unread": {
		"type":     "command",
		"title":    "agent-handler: Switch to Unread Session",
		"subtitle": "Jump to the first session with unread messages",
		"command":  "handler switch -u --close-caller",
		"shortcut": "cmd+shift+i",
		"palette":  true,
	},
}

var handlerCmuxActionIDs = []string{"handler-switch-to-awaiting", "handler-switch-to-session", "handler-switch-to-unread"}

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

	// Set each action (always overwrite to pick up updates)
	for _, id := range handlerCmuxActionIDs {
		actionJSON, _ := json.Marshal(handlerCmuxActions[id])
		key := "actions." + id
		if err := exec.Command(cmuxSettings, "set", key, string(actionJSON)).Run(); err != nil {
			fmt.Printf("  ⚠ Failed to set cmux action %s: %v\n", id, err)
			return
		}
	}

	exec.Command("cmux", "reload-config").Run()
	var actionSummary []string
	for _, id := range handlerCmuxActionIDs {
		if s, ok := handlerCmuxActions[id]["shortcut"].(string); ok {
			actionSummary = append(actionSummary, fmt.Sprintf("%s (%s)", id, s))
		}
	}
	fmt.Printf("  ✓ Configured cmux actions: %s\n", strings.Join(actionSummary, ", "))
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

// CmuxShortcuts holds the configured keyboard shortcuts for handler cmux actions.
type CmuxShortcuts struct {
	SwitchToAwaiting string
	SwitchToSession  string
	SwitchToUnread   string
	FocusBack        string
	FocusForward     string
}

// GetCmuxShortcuts reads the configured shortcuts from the cmux config.
// Returns nil if cmux-settings is not available or actions aren't configured.
func GetCmuxShortcuts() *CmuxShortcuts {
	cmuxSettings := findCmuxSettings()
	if cmuxSettings == "" {
		return nil
	}
	out, err := exec.Command(cmuxSettings, "get", "actions").Output()
	if err != nil || len(out) == 0 {
		return nil
	}
	var actions map[string]map[string]interface{}
	if json.Unmarshal(out, &actions) != nil {
		return nil
	}
	shortcuts := &CmuxShortcuts{}
	if a, ok := actions["handler-switch-to-awaiting"]; ok {
		if s, ok := a["shortcut"].(string); ok {
			shortcuts.SwitchToAwaiting = s
		}
	}
	if a, ok := actions["handler-switch-to-session"]; ok {
		if s, ok := a["shortcut"].(string); ok {
			shortcuts.SwitchToSession = s
		}
	}
	if a, ok := actions["handler-switch-to-unread"]; ok {
		if s, ok := a["shortcut"].(string); ok {
			shortcuts.SwitchToUnread = s
		}
	}
	// Read browser back/forward from cmux shortcuts bindings
	// These have defaults even without explicit config, so try to read them
	if sOut, err := exec.Command(cmuxSettings, "get", "shortcuts.bindings.browserBack").Output(); err == nil {
		s := strings.TrimSpace(strings.Trim(string(sOut), "\""))
		if s != "" {
			shortcuts.FocusBack = s
		}
	}
	if sOut, err := exec.Command(cmuxSettings, "get", "shortcuts.bindings.browserForward").Output(); err == nil {
		s := strings.TrimSpace(strings.Trim(string(sOut), "\""))
		if s != "" {
			shortcuts.FocusForward = s
		}
	}
	// Default cmux shortcuts if not explicitly configured
	if shortcuts.FocusBack == "" {
		shortcuts.FocusBack = "cmd+["
	}
	if shortcuts.FocusForward == "" {
		shortcuts.FocusForward = "cmd+]"
	}

	if shortcuts.SwitchToAwaiting == "" && shortcuts.SwitchToSession == "" {
		return nil
	}
	return shortcuts
}

func removeCmuxActions() {
	cmuxSettings := findCmuxSettings()
	if cmuxSettings == "" {
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
