package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// readHookMatchers returns the matcher strings agent-handler installed for an
// event, plus the commands, from a settings.json written by configureHooks.
func readHookMatchers(t *testing.T, settingsPath, event string) (matchers, commands []string) {
	t.Helper()
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("failed to read settings: %v", err)
	}
	var settings struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("failed to parse settings: %v", err)
	}
	for _, group := range settings.Hooks[event] {
		matchers = append(matchers, group.Matcher)
		for _, h := range group.Hooks {
			commands = append(commands, h.Command)
		}
	}
	return matchers, commands
}

func TestConfigureHooksInstallsCronPostToolUseHook(t *testing.T) {
	claudeDir := t.TempDir()
	hooksDir := filepath.Join(t.TempDir(), "hooks")

	if err := configureHooks(claudeDir, hooksDir); err != nil {
		t.Fatalf("configureHooks failed: %v", err)
	}

	matchers, commands := readHookMatchers(t, filepath.Join(claudeDir, "settings.json"), "PostToolUse")
	if len(matchers) != 1 {
		t.Fatalf("expected 1 PostToolUse matcher group, got %d", len(matchers))
	}
	if matchers[0] != "CronCreate|CronDelete" {
		t.Errorf("expected matcher %q, got %q", "CronCreate|CronDelete", matchers[0])
	}
	want := filepath.Join(hooksDir, "post_tool_use.sh")
	if len(commands) != 1 || commands[0] != want {
		t.Errorf("expected command %q, got %v", want, commands)
	}
}

// Lifecycle hooks stay on the empty matcher (every invocation).
func TestConfigureHooksUsesEmptyMatcherForLifecycleHooks(t *testing.T) {
	claudeDir := t.TempDir()
	hooksDir := filepath.Join(t.TempDir(), "hooks")

	if err := configureHooks(claudeDir, hooksDir); err != nil {
		t.Fatalf("configureHooks failed: %v", err)
	}

	for _, event := range []string{"Stop", "SessionEnd", "UserPromptSubmit", "PreCompact"} {
		matchers, _ := readHookMatchers(t, filepath.Join(claudeDir, "settings.json"), event)
		if len(matchers) != 1 {
			t.Errorf("%s: expected 1 matcher group, got %d", event, len(matchers))
			continue
		}
		if matchers[0] != "" {
			t.Errorf("%s: expected empty matcher, got %q", event, matchers[0])
		}
	}
}

// Re-running setup must not stack duplicate agent-handler groups.
func TestConfigureHooksIsIdempotent(t *testing.T) {
	claudeDir := t.TempDir()
	hooksDir := filepath.Join(t.TempDir(), "agent-handler", "hooks")

	for i := 0; i < 3; i++ {
		if err := configureHooks(claudeDir, hooksDir); err != nil {
			t.Fatalf("configureHooks run %d failed: %v", i, err)
		}
	}

	for _, event := range []string{"PostToolUse", "Stop", "SessionEnd", "UserPromptSubmit", "PreCompact"} {
		matchers, _ := readHookMatchers(t, filepath.Join(claudeDir, "settings.json"), event)
		if len(matchers) != 1 {
			t.Errorf("%s: expected 1 group after repeated setup, got %d", event, len(matchers))
		}
	}
}

// A third party's PostToolUse hook must survive setup.
func TestConfigureHooksPreservesForeignPostToolUseHooks(t *testing.T) {
	claudeDir := t.TempDir()
	hooksDir := filepath.Join(t.TempDir(), "agent-handler", "hooks")
	settingsPath := filepath.Join(claudeDir, "settings.json")

	foreign := `{"hooks":{"PostToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"/opt/other/lint.sh"}]}]}}`
	if err := os.WriteFile(settingsPath, []byte(foreign), 0644); err != nil {
		t.Fatalf("failed to seed settings: %v", err)
	}

	if err := configureHooks(claudeDir, hooksDir); err != nil {
		t.Fatalf("configureHooks failed: %v", err)
	}

	matchers, commands := readHookMatchers(t, settingsPath, "PostToolUse")
	if len(matchers) != 2 {
		t.Fatalf("expected foreign hook preserved alongside ours, got %d groups", len(matchers))
	}
	var foundForeign bool
	for _, c := range commands {
		if c == "/opt/other/lint.sh" {
			foundForeign = true
		}
	}
	if !foundForeign {
		t.Errorf("foreign PostToolUse hook was dropped; commands=%v", commands)
	}
}

func TestRemoveHooksRemovesCronAndStopHooks(t *testing.T) {
	claudeDir := t.TempDir()
	hooksDir := filepath.Join(t.TempDir(), "agent-handler", "hooks")
	settingsPath := filepath.Join(claudeDir, "settings.json")

	if err := configureHooks(claudeDir, hooksDir); err != nil {
		t.Fatalf("configureHooks failed: %v", err)
	}
	if err := removeHooks(claudeDir); err != nil {
		t.Fatalf("removeHooks failed: %v", err)
	}

	for _, event := range []string{"PostToolUse", "Stop", "SessionEnd", "UserPromptSubmit", "PreCompact"} {
		matchers, commands := readHookMatchers(t, settingsPath, event)
		if len(matchers) != 0 {
			t.Errorf("%s: expected hook removed, got %d groups (%v)", event, len(matchers), commands)
		}
	}
}

func TestRemoveHooksPreservesForeignHooks(t *testing.T) {
	claudeDir := t.TempDir()
	hooksDir := filepath.Join(t.TempDir(), "agent-handler", "hooks")
	settingsPath := filepath.Join(claudeDir, "settings.json")

	foreign := `{"hooks":{"PostToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"/opt/other/lint.sh"}]}]}}`
	if err := os.WriteFile(settingsPath, []byte(foreign), 0644); err != nil {
		t.Fatalf("failed to seed settings: %v", err)
	}
	if err := configureHooks(claudeDir, hooksDir); err != nil {
		t.Fatalf("configureHooks failed: %v", err)
	}
	if err := removeHooks(claudeDir); err != nil {
		t.Fatalf("removeHooks failed: %v", err)
	}

	_, commands := readHookMatchers(t, settingsPath, "PostToolUse")
	if len(commands) != 1 || commands[0] != "/opt/other/lint.sh" {
		t.Errorf("expected only the foreign hook to remain, got %v", commands)
	}
}
