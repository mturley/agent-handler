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
	want := filepath.Join(hooksDir, "post_tool_use.sh")
	var found bool
	for i, m := range matchers {
		if m == "CronCreate|CronDelete" && commands[i] == want {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a %q group pointing at %q; got %v / %v",
			"CronCreate|CronDelete", want, matchers, commands)
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

	// PostToolUse legitimately carries two groups (cron recorder + wake check);
	// the rest carry one. Repeated setup must not grow either.
	expected := map[string]int{
		"PostToolUse": 2, "Stop": 1, "SessionEnd": 1, "UserPromptSubmit": 1, "PreCompact": 1,
	}
	for event, want := range expected {
		matchers, _ := readHookMatchers(t, filepath.Join(claudeDir, "settings.json"), event)
		if len(matchers) != want {
			t.Errorf("%s: expected %d group(s) after repeated setup, got %d", event, want, len(matchers))
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
	if len(matchers) != 3 {
		t.Fatalf("expected the foreign hook preserved alongside our two, got %d groups", len(matchers))
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

// --- wake hooks -------------------------------------------------------------

func TestConfigureHooksInstallsWakeHooks(t *testing.T) {
	claudeDir := t.TempDir()
	hooksDir := filepath.Join(t.TempDir(), "agent-handler", "hooks")

	if err := configureHooks(claudeDir, hooksDir); err != nil {
		t.Fatalf("configureHooks failed: %v", err)
	}
	settingsPath := filepath.Join(claudeDir, "settings.json")

	// PostToolUse now carries two groups: the cron recorder and the wake check.
	matchers, commands := readHookMatchers(t, settingsPath, "PostToolUse")
	if len(matchers) != 2 {
		t.Fatalf("expected 2 PostToolUse groups, got %d (%v)", len(matchers), commands)
	}
	var sawCron, sawWake bool
	for i, m := range matchers {
		switch m {
		case "CronCreate|CronDelete":
			sawCron = commands[i] == filepath.Join(hooksDir, "post_tool_use.sh")
		case "":
			sawWake = commands[i] == filepath.Join(hooksDir, "post_tool_use_wake.sh")
		}
	}
	if !sawCron {
		t.Errorf("missing the CronCreate|CronDelete recorder group; got %v / %v", matchers, commands)
	}
	if !sawWake {
		t.Errorf("missing the all-tools wake group; got %v / %v", matchers, commands)
	}

	preMatchers, preCommands := readHookMatchers(t, settingsPath, "PreToolUse")
	if len(preMatchers) != 1 || preMatchers[0] != "CronCreate" {
		t.Errorf("expected one PreToolUse group matching CronCreate, got %v", preMatchers)
	}
	if len(preCommands) != 1 || preCommands[0] != filepath.Join(hooksDir, "pre_tool_use.sh") {
		t.Errorf("unexpected PreToolUse command: %v", preCommands)
	}

	sfMatchers, sfCommands := readHookMatchers(t, settingsPath, "StopFailure")
	if len(sfMatchers) != 1 {
		t.Errorf("expected one StopFailure group, got %v", sfMatchers)
	}
	if len(sfCommands) != 1 || sfCommands[0] != filepath.Join(hooksDir, "stop_failure.sh") {
		t.Errorf("unexpected StopFailure command: %v", sfCommands)
	}
}

func TestConfigureHooksIsIdempotentWithWakeHooks(t *testing.T) {
	claudeDir := t.TempDir()
	hooksDir := filepath.Join(t.TempDir(), "agent-handler", "hooks")
	for i := 0; i < 3; i++ {
		if err := configureHooks(claudeDir, hooksDir); err != nil {
			t.Fatalf("run %d failed: %v", i, err)
		}
	}
	settingsPath := filepath.Join(claudeDir, "settings.json")

	if m, c := readHookMatchers(t, settingsPath, "PostToolUse"); len(m) != 2 {
		t.Errorf("expected PostToolUse to stay at 2 groups, got %d (%v)", len(m), c)
	}
	for _, event := range []string{"PreToolUse", "StopFailure"} {
		if m, _ := readHookMatchers(t, settingsPath, event); len(m) != 1 {
			t.Errorf("%s: expected 1 group after repeated setup, got %d", event, len(m))
		}
	}
}

func TestRemoveHooksRemovesWakeHooks(t *testing.T) {
	claudeDir := t.TempDir()
	hooksDir := filepath.Join(t.TempDir(), "agent-handler", "hooks")
	if err := configureHooks(claudeDir, hooksDir); err != nil {
		t.Fatalf("configureHooks failed: %v", err)
	}
	if err := removeHooks(claudeDir); err != nil {
		t.Fatalf("removeHooks failed: %v", err)
	}
	settingsPath := filepath.Join(claudeDir, "settings.json")

	for _, event := range []string{"PostToolUse", "PreToolUse", "StopFailure"} {
		if m, c := readHookMatchers(t, settingsPath, event); len(m) != 0 {
			t.Errorf("%s: expected removal, got %d groups (%v)", event, len(m), c)
		}
	}
}
