package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These tests run the real hook scripts under hooks/ with `bash`, against a
// fake `handler` binary placed on a temp PATH, so they exercise the exact
// bytes that get installed rather than a copy. Filenames must stay in sync
// with configureHooks()/removeHooks() in cmd/setup.go / cmd/uninstall.go.

// hooksScriptPath resolves a script under the repo's hooks/ directory from
// this test file's own location, independent of the test binary's cwd.
func hooksScriptPath(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine test file location")
	}
	// cmd/hook_scripts_test.go -> repo root -> hooks/<name>
	path := filepath.Join(filepath.Dir(thisFile), "..", "hooks", name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("hook script not found at %s: %v", path, err)
	}
	return path
}

// fakeHandlerPath writes a fake `handler` script to a temp dir and returns
// that dir (to prepend to PATH) plus the path to the file it will record its
// invocation (args + stdin) into. exitCode controls the fake handler's exit
// status, so tests can simulate a failing handler invocation.
func fakeHandlerPath(t *testing.T, exitCode int) (binDir, recordFile string) {
	t.Helper()
	binDir = t.TempDir()
	recordFile = filepath.Join(t.TempDir(), "record")

	script := "#!/bin/bash\n" +
		"{\n" +
		"  printf 'ARGS:%s\\n' \"$*\"\n" +
		"  printf 'ENV_CLAUDE_PID:%s\\n' \"$CLAUDE_PID\"\n" +
		"  printf 'STDIN_START\\n'\n" +
		"  cat\n" +
		"  printf '\\nSTDIN_END\\n'\n" +
		"} > " + shellQuote(recordFile) + "\n" +
		"exit " + itoa(exitCode) + "\n"

	handlerPath := filepath.Join(binDir, "handler")
	if err := os.WriteFile(handlerPath, []byte(script), 0755); err != nil {
		t.Fatalf("writing fake handler: %v", err)
	}
	return binDir, recordFile
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// runHook runs a hook script with bash, with the given PATH prefix and
// HANDLER_HOME, feeding stdin and returning combined output and exit code.
func runHook(t *testing.T, scriptPath, binDir, handlerHome, stdin string) (exitCode int) {
	t.Helper()
	cmd := exec.Command("bash", scriptPath)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"HANDLER_HOME=" + handlerHome,
		"HOME=" + os.Getenv("HOME"),
	}
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		t.Fatalf("running %s: %v (output: %s)", scriptPath, err, out.String())
	}
	return 0
}

func recordExists(recordFile string) bool {
	_, err := os.Stat(recordFile)
	return err == nil
}

// --- post_tool_use_wake.sh ---------------------------------------------------

func TestWakeHookNoMarkerDoesNotInvokeHandler(t *testing.T) {
	script := hooksScriptPath(t, "post_tool_use_wake.sh")

	for _, name := range []string{"empty-dir", "missing-dir"} {
		t.Run(name, func(t *testing.T) {
			handlerHome := t.TempDir()
			if name == "empty-dir" {
				if err := os.MkdirAll(filepath.Join(handlerHome, "state", "wake-armed"), 0755); err != nil {
					t.Fatalf("mkdir wake-armed: %v", err)
				}
			}
			binDir, recordFile := fakeHandlerPath(t, 0)

			payload := `{"session_id":"abc","tool_name":"Bash"}`
			code := runHook(t, script, binDir, handlerHome, payload)
			if code != 0 {
				t.Errorf("expected exit 0, got %d", code)
			}
			if recordExists(recordFile) {
				t.Error("fake handler should never have been invoked")
			}
		})
	}
}

func TestWakeHookWithMarkerPassesPayload(t *testing.T) {
	script := hooksScriptPath(t, "post_tool_use_wake.sh")
	handlerHome := t.TempDir()
	armedDir := filepath.Join(handlerHome, "state", "wake-armed")
	if err := os.MkdirAll(armedDir, 0755); err != nil {
		t.Fatalf("mkdir wake-armed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(armedDir, "abc"), nil, 0644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	binDir, recordFile := fakeHandlerPath(t, 0)

	payload := `{"session_id":"abc","tool_name":"Bash"}`
	code := runHook(t, script, binDir, handlerHome, payload)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}

	data, err := os.ReadFile(recordFile)
	if err != nil {
		t.Fatalf("fake handler was not invoked: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "ARGS:wake-check") {
		t.Errorf("expected handler invoked with args 'wake-check', got:\n%s", got)
	}
	if !strings.Contains(got, "STDIN_START\n"+payload+"\nSTDIN_END") {
		t.Errorf("expected exact payload on stdin, got:\n%s", got)
	}
}

func TestWakeHookNestedSessionIdKey(t *testing.T) {
	script := hooksScriptPath(t, "post_tool_use_wake.sh")
	handlerHome := t.TempDir()
	armedDir := filepath.Join(handlerHome, "state", "wake-armed")
	if err := os.MkdirAll(armedDir, 0755); err != nil {
		t.Fatalf("mkdir wake-armed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(armedDir, "abc"), nil, 0644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	binDir, recordFile := fakeHandlerPath(t, 0)

	// This is the case a bash regex for "session_id" gets wrong: the leftmost
	// match is the nested key inside tool_input ("zzz"), not the real session
	// id ("abc"). The hook must not try to pick a session out of this at all —
	// it just hands the whole payload to Go.
	payload := `{"tool_input":{"session_id":"zzz"},"session_id":"abc"}`
	code := runHook(t, script, binDir, handlerHome, payload)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}

	data, err := os.ReadFile(recordFile)
	if err != nil {
		t.Fatalf("fake handler was not invoked: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "ARGS:wake-check") {
		t.Errorf("expected handler invoked with args 'wake-check', got:\n%s", got)
	}
	if !strings.Contains(got, "STDIN_START\n"+payload+"\nSTDIN_END") {
		t.Errorf("expected the full payload on stdin, got:\n%s", got)
	}
}

func TestWakeHookHandlerFailureIsInvisible(t *testing.T) {
	script := hooksScriptPath(t, "post_tool_use_wake.sh")
	handlerHome := t.TempDir()
	armedDir := filepath.Join(handlerHome, "state", "wake-armed")
	if err := os.MkdirAll(armedDir, 0755); err != nil {
		t.Fatalf("mkdir wake-armed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(armedDir, "abc"), nil, 0644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	binDir, _ := fakeHandlerPath(t, 1)

	payload := `{"session_id":"abc","tool_name":"Bash"}`
	code := runHook(t, script, binDir, handlerHome, payload)
	if code != 0 {
		t.Errorf("a failing handler invocation must stay invisible; expected exit 0, got %d", code)
	}
}

// Regression guard for the old implementation, which parsed JSON in bash with
// jq (or a sed/cat fallback) to pull session_id out before deciding whether to
// invoke handler at all.
func TestWakeHookDoesNotUseJq(t *testing.T) {
	script := hooksScriptPath(t, "post_tool_use_wake.sh")
	data, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("reading script: %v", err)
	}
	// Strip comments so prose (e.g. "catches") doesn't trip a command-name
	// check; this only cares about commands actually invoked.
	var codeLines []string
	for _, line := range strings.Split(string(data), "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		codeLines = append(codeLines, line)
	}
	code := strings.Join(codeLines, "\n")

	wordBoundary := func(s, word string) bool {
		idx := 0
		for {
			i := strings.Index(s[idx:], word)
			if i < 0 {
				return false
			}
			pos := idx + i
			before := byte(' ')
			if pos > 0 {
				before = s[pos-1]
			}
			after := byte(' ')
			if pos+len(word) < len(s) {
				after = s[pos+len(word)]
			}
			isWordChar := func(b byte) bool {
				return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
			}
			if !isWordChar(before) && !isWordChar(after) {
				return true
			}
			idx = pos + 1
		}
	}

	for _, banned := range []string{"jq", "cat", "sed"} {
		if wordBoundary(code, banned) {
			t.Errorf("script must not invoke %q; found it in:\n%s", banned, code)
		}
	}
}

// --- statusline.sh -----------------------------------------------------------

func TestStatuslineHookPassesStdin(t *testing.T) {
	script := hooksScriptPath(t, "statusline.sh")
	binDir, recordFile := fakeHandlerPath(t, 0)

	payload := `{"session_id":"abc"}`
	cmd := exec.Command("bash", script)
	cmd.Stdin = strings.NewReader(payload)
	cmd.Env = []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
	}
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("running %s: %v (output: %s)", script, err, out.String())
	}

	data, err := os.ReadFile(recordFile)
	if err != nil {
		t.Fatalf("fake handler was not invoked: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "ARGS:statusline --from-hook") {
		t.Errorf("expected handler invoked with args 'statusline --from-hook', got:\n%s", got)
	}
	if !strings.Contains(got, "STDIN_START\n"+payload+"\nSTDIN_END") {
		t.Errorf("expected exact stdin passed through, got:\n%s", got)
	}
	if !strings.Contains(got, "ENV_CLAUDE_PID:") || strings.Contains(got, "ENV_CLAUDE_PID:\n") {
		t.Errorf("expected a non-empty CLAUDE_PID env var, got:\n%s", got)
	}
}
