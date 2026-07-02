package terminal

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// CmuxBackend implements Backend for cmux terminals.
type CmuxBackend struct{}

func (b *CmuxBackend) Capture(terminalID string, lines int) (string, error) {
	args := []string{"capture-pane", "--surface", terminalID}
	if lines > 0 {
		args = append(args, "--lines", strconv.Itoa(lines))
	}
	cmd := exec.Command("cmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return "", fmt.Errorf("cmux capture-pane: %s", msg)
		}
		return "", fmt.Errorf("cmux capture-pane failed: %w", err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func (b *CmuxBackend) Notify(terminalID string, title, body string) error {
	args := []string{"notify", "--surface", terminalID, "--title", title}
	if body != "" {
		args = append(args, "--body", body)
	}
	if err := exec.Command("cmux", args...).Run(); err != nil {
		return fmt.Errorf("cmux notify failed: %w", err)
	}
	return nil
}

func (b *CmuxBackend) Flash(terminalID string) error {
	if err := exec.Command("cmux", "trigger-flash", "--surface", terminalID).Run(); err != nil {
		return fmt.Errorf("cmux trigger-flash failed: %w", err)
	}
	return nil
}

func (b *CmuxBackend) Bell(terminalID string) error {
	return nil // cmux has better notification primitives
}
