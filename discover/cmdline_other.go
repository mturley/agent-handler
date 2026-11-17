//go:build !darwin && !linux

package discover

import (
	"os/exec"
	"strconv"
)

// processCmdline returns the command line for the given PID by shelling out
// to ps, for platforms without a direct syscall-based implementation.
func processCmdline(pid int) (string, error) {
	out, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
