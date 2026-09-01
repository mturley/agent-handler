package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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

// readCmuxConfig reads and parses ~/.config/cmux/cmux.json directly. Handler
// owns this end-to-end — it never shells out to any external "cmux-settings"
// helper (that was a skill-provided script that is not guaranteed to be
// installed, and must not be depended on here).
func readCmuxConfig() map[string]interface{} {
	data, err := os.ReadFile(cmuxConfigFilePath())
	if err != nil {
		return nil
	}
	var cfg map[string]interface{}
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	return cfg
}

// writeCmuxConfig backs up the existing cmux.json to a timestamped .bak
// alongside it (per cmux's own docs: "back up any existing cmux.json file to
// a timestamped .bak copy before editing"), then writes the new config and
// asks cmux to reload it.
func writeCmuxConfig(cfg map[string]interface{}) error {
	path := cmuxConfigFilePath()
	if data, err := os.ReadFile(path); err == nil {
		backupPath := fmt.Sprintf("%s.%s.bak", path, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(backupPath, data, 0644); err != nil {
			return fmt.Errorf("backing up cmux.json: %w", err)
		}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling cmux.json: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating cmux config dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing cmux.json: %w", err)
	}
	exec.Command("cmux", "reload-config").Run()
	return nil
}

func configureCmuxActions() {
	cfg := readCmuxConfig()
	if cfg == nil {
		cfg = map[string]interface{}{}
	}
	actions, ok := cfg["actions"].(map[string]interface{})
	if !ok {
		actions = map[string]interface{}{}
	}

	// Set each action (always overwrite to pick up updates)
	for _, id := range handlerCmuxActionIDs {
		actions[id] = handlerCmuxActions[id]
	}
	cfg["actions"] = actions

	if err := writeCmuxConfig(cfg); err != nil {
		fmt.Printf("  ⚠ Failed to configure cmux actions: %v\n", err)
		return
	}

	var actionSummary []string
	for _, id := range handlerCmuxActionIDs {
		if s, ok := handlerCmuxActions[id]["shortcut"].(string); ok {
			actionSummary = append(actionSummary, fmt.Sprintf("%s (%s)", id, s))
		}
	}
	fmt.Printf("  ✓ Configured cmux actions: %s\n", strings.Join(actionSummary, ", "))
}

func cmuxConfigActions() map[string]interface{} {
	cfg := readCmuxConfig()
	if cfg == nil {
		return nil
	}
	actions, ok := cfg["actions"].(map[string]interface{})
	if !ok {
		return nil
	}
	return actions
}

func hasCmuxActions() bool {
	actions := cmuxConfigActions()
	if actions == nil {
		return false
	}
	for _, id := range handlerCmuxActionIDs {
		if _, ok := actions[id]; ok {
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

// GetCmuxShortcuts reads the configured shortcuts directly from
// ~/.config/cmux/cmux.json. Returns nil if the config is missing or the
// handler actions aren't configured in it.
func GetCmuxShortcuts() *CmuxShortcuts {
	cfg := readCmuxConfig()
	if cfg == nil {
		return nil
	}
	actions, _ := cfg["actions"].(map[string]interface{})

	shortcutFor := func(id string) string {
		a, ok := actions[id].(map[string]interface{})
		if !ok {
			return ""
		}
		s, _ := a["shortcut"].(string)
		return s
	}

	shortcuts := &CmuxShortcuts{
		SwitchToAwaiting: shortcutFor("handler-switch-to-awaiting"),
		SwitchToSession:  shortcutFor("handler-switch-to-session"),
		SwitchToUnread:   shortcutFor("handler-switch-to-unread"),
	}

	// Read browser back/forward from cmux shortcuts bindings.
	// These have defaults even without explicit config.
	if shortcutsCfg, ok := cfg["shortcuts"].(map[string]interface{}); ok {
		if bindings, ok := shortcutsCfg["bindings"].(map[string]interface{}); ok {
			if s, ok := bindings["browserBack"].(string); ok && s != "" {
				shortcuts.FocusBack = s
			}
			if s, ok := bindings["browserForward"].(string); ok && s != "" {
				shortcuts.FocusForward = s
			}
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
	cfg := readCmuxConfig()
	if cfg == nil {
		return
	}
	actions, ok := cfg["actions"].(map[string]interface{})
	if !ok {
		return
	}

	found := false
	for _, id := range handlerCmuxActionIDs {
		if _, ok := actions[id]; ok {
			found = true
			delete(actions, id)
		}
	}
	if !found {
		return
	}
	cfg["actions"] = actions

	if err := writeCmuxConfig(cfg); err != nil {
		fmt.Printf("  ⚠ Failed to remove cmux actions: %v\n", err)
		return
	}
	fmt.Println("  ✓ Removed cmux actions (handler-switch-to-awaiting, handler-switch-to-session)")
}
