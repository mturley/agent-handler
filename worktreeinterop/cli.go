package worktreeinterop

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

// Seams for testing.
var (
	execCommand = exec.Command
	lookPath    = exec.LookPath
)

// Available reports whether the `worktree` binary is on PATH.
func Available() bool {
	_, err := lookPath("worktree")
	return err == nil
}

type listItem struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	URL     string `json:"url"`
	Primary bool   `json:"primary"`
}

// ListPrimaryResources runs `worktree resources list --json` for dir and
// returns only the primary resources. Any error means "no worktree
// integration" — callers should proceed with their own state.
func ListPrimaryResources(dir string) ([]Resource, error) {
	cmd := execCommand("worktree", "resources", "list", "--json", "--worktree", dir)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("worktree resources list: %w", err)
	}
	var items []listItem
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("parse worktree resources json: %w", err)
	}
	var primaries []Resource
	for _, it := range items {
		if it.Primary {
			primaries = append(primaries, Resource{Type: it.Type, ID: it.ID, URL: it.URL})
		}
	}
	return primaries, nil
}
