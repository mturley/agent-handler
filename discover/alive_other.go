//go:build !unix

package discover

import (
	"os/exec"
	"strconv"
)

// IsProcessAlive checks if a process with the given PID is running
func IsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	cmd := exec.Command("kill", "-0", strconv.Itoa(pid))
	return cmd.Run() == nil
}
