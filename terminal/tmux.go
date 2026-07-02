package terminal

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// TmuxBackend implements Backend for tmux terminals.
type TmuxBackend struct{}

func (b *TmuxBackend) Capture(terminalID string, lines int) (string, error) {
	args := []string{"capture-pane", "-t", terminalID, "-p"}
	if lines > 0 {
		args = append(args, "-S", "-"+strconv.Itoa(lines))
	}
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane failed: %w", err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func (b *TmuxBackend) Notify(terminalID string, title, body string) error {
	return nil // tmux has no native notification mechanism
}

func (b *TmuxBackend) Flash(terminalID string) error {
	return nil // tmux has no flash equivalent
}

func (b *TmuxBackend) Bell(terminalID string) error {
	err := exec.Command("tmux", "send-keys", "-t", terminalID, "printf", "'\\a'", "Enter").Run()
	if err != nil {
		return fmt.Errorf("tmux bell failed: %w", err)
	}
	return nil
}
