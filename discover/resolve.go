package discover

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ResolveSessionID finds the current session ID. It checks (in order):
// 1. HANDLER_SESSION_ID env var (set by hooks that know the session ID)
// 2. PID cache for CLAUDE_PID env var (the Claude process PID, set by hooks)
// 3. PID cache for PPID (direct parent, typically the Claude process)
// It does NOT walk up the full PID tree — that caused false matches when
// stale PID cache files from dead sessions collided with reused PIDs.
func ResolveSessionID(handlerHome string) (string, error) {
	// Check env var first (most reliable, set by hooks)
	if envID := os.Getenv("HANDLER_SESSION_ID"); envID != "" {
		return envID, nil
	}

	sessionsDir := filepath.Join(handlerHome, "data", "sessions")

	// Check CLAUDE_PID env var (set by user_prompt_submit hook)
	if claudePID := os.Getenv("CLAUDE_PID"); claudePID != "" {
		pid, err := strconv.Atoi(claudePID)
		if err == nil {
			if id, err := ReadPIDCache(sessionsDir, pid); err == nil {
				return id, nil
			}
		}
	}

	// Walk ancestors looking for a claude process with a PID cache entry.
	// Only trust cache files belonging to actual claude processes to avoid
	// false matches from stale cache files with reused PIDs.
	pid := os.Getppid()
	for i := 0; i < 5 && pid > 1; i++ {
		if isClaudeProcess(pid) {
			if id, err := ReadPIDCache(sessionsDir, pid); err == nil {
				return id, nil
			}
		}
		pid = getParentPID(pid)
	}

	return "", fmt.Errorf("no claude process found in ancestor tree with a registered session")
}

func isClaudeProcess(pid int) bool {
	out, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "claude")
}

func getParentPID(pid int) int {
	out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	ppid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0
	}
	return ppid
}
