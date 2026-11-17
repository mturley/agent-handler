//go:build linux

package discover

import (
	"fmt"
	"os"
	"strings"
)

// processCmdline returns the command line for the given PID by reading
// /proc/<pid>/cmdline and replacing NUL separators with spaces.
func processCmdline(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return "", err
	}
	data = []byte(strings.TrimRight(string(data), "\x00"))
	if len(data) == 0 {
		return "", fmt.Errorf("cmdline: empty for pid %d", pid)
	}
	return strings.ReplaceAll(string(data), "\x00", " "), nil
}
