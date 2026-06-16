package discover

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ResolveSessionID finds the current session ID by checking the PID cache
// for the current process and its ancestors (up to 5 levels).
func ResolveSessionID(handlerHome string) (string, error) {
	sessionsDir := filepath.Join(handlerHome, "data", "sessions")

	pid := os.Getpid()
	for i := 0; i < 5; i++ {
		if id, err := ReadPIDCache(sessionsDir, pid); err == nil {
			return id, nil
		}
		pid = getParentPID(pid)
		if pid <= 1 {
			break
		}
	}

	return "", fmt.Errorf("no session found in PID cache for this process or its ancestors")
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
