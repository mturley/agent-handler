package discover

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// WritePIDCache writes the session ID to a PID cache file
func WritePIDCache(sessionsDir string, pid int, sessionID string) error {
	pidFile := filepath.Join(sessionsDir, strconv.Itoa(pid))
	return os.WriteFile(pidFile, []byte(sessionID), 0644)
}

// ReadPIDCache reads the session ID from a PID cache file
func ReadPIDCache(sessionsDir string, pid int) (string, error) {
	pidFile := filepath.Join(sessionsDir, strconv.Itoa(pid))
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// CleanStalePIDCaches removes PID cache files for processes that are no longer running
func CleanStalePIDCaches(sessionsDir string) (int, error) {
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return 0, err
	}

	cleaned := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Try to parse the filename as a PID
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			// Not a PID file, skip
			continue
		}

		// Check if the process is still alive
		if !IsProcessAlive(pid) {
			pidFile := filepath.Join(sessionsDir, entry.Name())
			if err := os.Remove(pidFile); err != nil {
				return cleaned, err
			}
			cleaned++
		}
	}

	return cleaned, nil
}

// IsProcessAlive checks if a process with the given PID is running
func IsProcessAlive(pid int) bool {
	// Use kill -0 to check if the process exists
	cmd := exec.Command("kill", "-0", strconv.Itoa(pid))
	err := cmd.Run()
	return err == nil
}
