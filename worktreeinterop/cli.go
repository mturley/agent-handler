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
	Type              string `json:"type"`
	ID                string `json:"id"`
	URL               string `json:"url"`
	Primary           bool   `json:"primary"`
	CustomName        string `json:"custom_name"`
	CustomDescription string `json:"custom_description"`
	UpdatedAt         string `json:"updated_at"`
}

func (it listItem) toResource() Resource {
	return Resource{
		Type: it.Type, ID: it.ID, URL: it.URL,
		CustomName: it.CustomName, CustomDescription: it.CustomDescription,
		UpdatedAt: it.UpdatedAt,
	}
}

// listAll runs `worktree resources list --json` for dir and returns the raw
// parsed items. Any error means "no worktree integration" — callers should
// proceed with their own state.
func listAll(dir string) ([]listItem, error) {
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
	return items, nil
}

// ListPrimaryResources returns only the primary resources tracked by dir.
func ListPrimaryResources(dir string) ([]Resource, error) {
	items, err := listAll(dir)
	if err != nil {
		return nil, err
	}
	var primaries []Resource
	for _, it := range items {
		if it.Primary {
			primaries = append(primaries, it.toResource())
		}
	}
	return primaries, nil
}

// ListResources returns ALL resources tracked by dir (primary and related),
// carrying custom name/description and updated_at. The name-sync heartbeat
// uses this because a Slack thread may be tracked as "related", not primary.
func ListResources(dir string) ([]Resource, error) {
	items, err := listAll(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Resource, 0, len(items))
	for _, it := range items {
		out = append(out, it.toResource())
	}
	return out, nil
}

// SetName runs `worktree resources set-name` for dir, always passing
// --updated-at so the origin timestamp is preserved on the worktree side
// (newest-wins convergence). Best-effort: the caller treats any error as a
// soft degradation.
func SetName(dir string, r Resource, name, description, updatedAt string) error {
	args := []string{"resources", "set-name", r.Type, r.ID, "--name", name, "--updated-at", updatedAt}
	if description != "" {
		args = append(args, "--description", description)
	}
	args = append(args, "--worktree", dir)
	cmd := execCommand("worktree", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("worktree resources set-name: %w (%s)", err, string(out))
	}
	return nil
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
