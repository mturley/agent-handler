//go:build unix

package discover

import (
	"errors"
	"syscall"
)

// IsProcessAlive checks if a process with the given PID is running
func IsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
