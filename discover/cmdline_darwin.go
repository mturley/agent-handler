//go:build darwin

package discover

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/unix"
)

// processCmdline returns the command line (exec path + args, space-joined)
// for the given PID using the kern.procargs2 sysctl.
func processCmdline(pid int) (string, error) {
	data, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", err
	}
	if len(data) < 4 {
		return "", fmt.Errorf("procargs2: short buffer for pid %d", pid)
	}

	argc := int(binary.LittleEndian.Uint32(data[:4]))
	rest := data[4:]

	// Skip the NUL-terminated exec path.
	idx := bytes.IndexByte(rest, 0)
	if idx < 0 {
		return "", fmt.Errorf("procargs2: no exec path terminator for pid %d", pid)
	}
	rest = rest[idx:]

	// Skip NUL padding until the first argv string starts.
	for len(rest) > 0 && rest[0] == 0 {
		rest = rest[1:]
	}

	args := make([]string, 0, argc)
	for i := 0; i < argc && len(rest) > 0; i++ {
		idx := bytes.IndexByte(rest, 0)
		if idx < 0 {
			args = append(args, string(rest))
			rest = nil
			break
		}
		args = append(args, string(rest[:idx]))
		rest = rest[idx+1:]
	}

	if len(args) == 0 {
		return "", fmt.Errorf("procargs2: empty argv for pid %d", pid)
	}

	result := args[0]
	for _, a := range args[1:] {
		result += " " + a
	}
	return result, nil
}
