package worktreeinterop

import (
	"encoding/json"
	"errors"
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

// SetSeamsForTest overrides the exec/lookPath seams for testing and returns a
// restore func. Pass nil to leave a seam unchanged.
func SetSeamsForTest(execFn func(string, ...string) *exec.Cmd, lookFn func(string) (string, error)) (restore func()) {
	origExec, origLook := execCommand, lookPath
	if execFn != nil {
		execCommand = execFn
	}
	if lookFn != nil {
		lookPath = lookFn
	}
	return func() { execCommand = origExec; lookPath = origLook }
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
		// cmd.Output captures stderr into ExitError.Stderr on a non-zero
		// exit; surface it to aid debugging a broken worktree CLI.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return nil, fmt.Errorf("worktree resources list: %w (%s)", err, string(exitErr.Stderr))
		}
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

// AddResource runs `worktree resources add` for dir. Best-effort: the caller
// treats any error as a soft degradation.
func AddResource(dir string, r Resource, related bool) error {
	args := []string{"resources", "add", r.Type, r.ID}
	if r.URL != "" {
		args = append(args, "--url", r.URL)
	}
	if related {
		args = append(args, "--related")
	}
	args = append(args, "--worktree", dir)
	cmd := execCommand("worktree", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("worktree resources add: %w (%s)", err, string(out))
	}
	return nil
}
