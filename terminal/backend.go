package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Backend defines the interface for interacting with a terminal environment.
type Backend interface {
	Capture(terminalID string, lines int) (string, error)
	Notify(terminalID string, title, body string) error
	Flash(terminalID string) error
	Bell(terminalID string) error
}

// Detect checks the current environment and returns the terminal backend type,
// terminal ID, and workspace ID. Checks cmux first, then tmux.
func Detect() (backendType, terminalID, workspaceID string) {
	if surfaceID := os.Getenv("CMUX_SURFACE_ID"); surfaceID != "" {
		return "cmux", surfaceID, os.Getenv("CMUX_WORKSPACE_ID")
	}

	if os.Getenv("TMUX") != "" {
		out, err := exec.Command("tmux", "display-message", "-p", "#{pane_id}").Output()
		if err == nil {
			paneID := strings.TrimSpace(string(out))
			if paneID != "" {
				return "tmux", paneID, ""
			}
		}
	}

	return "", "", ""
}

// NewBackend returns a Backend implementation for the given type.
func NewBackend(backendType string) (Backend, error) {
	switch backendType {
	case "cmux":
		return &CmuxBackend{}, nil
	case "tmux":
		return &TmuxBackend{}, nil
	default:
		return nil, fmt.Errorf("unsupported terminal backend: %q", backendType)
	}
}
