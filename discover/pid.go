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
	cmd := exec.Command("kill", "-0", strconv.Itoa(pid))
	err := cmd.Run()
	return err == nil
}

// IsSessionProcess checks if the process at the given PID belongs to the
// specified session. Returns true if the process is alive and its command line
// contains the session ID (handles both --session-id and --resume flags).
// Returns true if the process is alive but we can't read its command line
// (benefit of the doubt). Returns false if the process is dead or belongs
// to a different session.
func IsSessionProcess(pid int, sessionID string) bool {
	if !IsProcessAlive(pid) {
		return false
	}
	out, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return true // can't inspect, assume it's ours
	}
	cmdline := string(out)
	if !strings.Contains(cmdline, "claude") {
		return false // not a claude process at all
	}
	if strings.Contains(cmdline, sessionID) {
		return true // session ID in command line
	}
	// Claude process but no session ID visible — could be a --continue session
	// where the session ID isn't on the command line. Check PID cache as tiebreaker.
	home := os.Getenv("HANDLER_HOME")
	if home == "" {
		homeDir, _ := os.UserHomeDir()
		home = filepath.Join(homeDir, ".agent-handler")
	}
	sessionsDir := filepath.Join(home, "data", "sessions")
	cachedID, err := ReadPIDCache(sessionsDir, pid)
	if err == nil && cachedID == sessionID {
		return true
	}
	return false
}
