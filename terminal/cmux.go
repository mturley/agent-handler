package terminal

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// CmuxBackend implements Backend for cmux terminals.
type CmuxBackend struct{}

func (b *CmuxBackend) Capture(terminalID string, lines int) (string, error) {
	args := []string{"capture-pane", "--surface", terminalID, "--window", "window:1"}
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
	args := []string{"notify", "--surface", terminalID, "--window", "window:1", "--title", title}
	if body != "" {
		args = append(args, "--body", body)
	}
	if err := exec.Command("cmux", args...).Run(); err != nil {
		return fmt.Errorf("cmux notify failed: %w", err)
	}
	return nil
}

func (b *CmuxBackend) Flash(terminalID string) error {
	if err := exec.Command("cmux", "trigger-flash", "--surface", terminalID, "--window", "window:1").Run(); err != nil {
		return fmt.Errorf("cmux trigger-flash failed: %w", err)
	}
	return nil
}

func (b *CmuxBackend) Bell(terminalID string) error {
	return nil // cmux has better notification primitives
}

// CmuxWorkspaceName resolves the workspace name for a surface UUID.
// Returns empty string if resolution fails.
func CmuxWorkspaceName(surfaceID string) string {
	// Get workspace ref from identify
	idOut, err := exec.Command("cmux", "identify", "--surface", surfaceID).Output()
	if err != nil {
		return ""
	}
	var idData struct {
		Caller *struct {
			WorkspaceRef string `json:"workspace_ref"`
		} `json:"caller"`
	}
	if err := json.Unmarshal(idOut, &idData); err != nil || idData.Caller == nil || idData.Caller.WorkspaceRef == "" {
		return ""
	}

	// Get workspace name from list
	listOut, err := exec.Command("cmux", "workspace", "list", "--json").Output()
	if err != nil {
		return ""
	}
	var listData struct {
		Workspaces []struct {
			Ref         string `json:"ref"`
			Title       string `json:"title"`
			CustomTitle string `json:"custom_title"`
		} `json:"workspaces"`
	}
	if err := json.Unmarshal(listOut, &listData); err != nil {
		return ""
	}
	for _, w := range listData.Workspaces {
		if w.Ref == idData.Caller.WorkspaceRef {
			if w.CustomTitle != "" {
				return w.CustomTitle
			}
			return w.Title
		}
	}
	return ""
}
