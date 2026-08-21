package cmd

import (
	"os"
	"os/exec"
	"reflect"
	"testing"

	"github.com/mturley/agent-handler/worktreeinterop"
)

// TestSubscribe_PropagatesToWorktree verifies that a successful `subscribe`
// invocation propagates the resource to the worktree CLI (best-effort) via
// worktreeinterop.AddResource, with the expected argv.
func TestSubscribe_PropagatesToWorktree(t *testing.T) {
	isolateHomes(t)

	var gotArgs []string
	restore := worktreeinterop.SetSeamsForTest(
		func(name string, args ...string) *exec.Cmd {
			gotArgs = append([]string{name}, args...)
			cs := []string{"-test.run=TestHelperProcess", "--", name}
			cs = append(cs, args...)
			cmd := exec.Command(os.Args[0], cs...)
			cmd.Env = append(os.Environ(),
				"GO_WANT_HELPER_PROCESS=1",
				"HELPER_STDOUT=",
				"HELPER_EXIT=0",
			)
			return cmd
		},
		lookPathFound,
	)
	defer restore()

	// Use a resource type ("custom") that ResourceTypeToService does not map
	// to any watcher service, so the "is this service configured?" guard in
	// runSubscribe is skipped and the test stays hermetic (no watcher creds
	// needed).
	subResource = "custom:o/r#1"
	subURL = "u1"
	t.Cleanup(func() { subResource = ""; subURL = "" })

	if err := subscribeCmd.Flags().Set("session-id", "test-session"); err != nil {
		t.Fatalf("set session-id flag: %v", err)
	}
	if err := subscribeCmd.Flags().Set("related", "false"); err != nil {
		t.Fatalf("set related flag: %v", err)
	}
	t.Cleanup(func() {
		subscribeCmd.Flags().Set("session-id", "")
		subscribeCmd.Flags().Set("related", "false")
	})

	if err := runSubscribe(subscribeCmd, nil); err != nil {
		t.Fatalf("runSubscribe: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	want := []string{"worktree", "resources", "add", "custom", "o/r#1", "--url", "u1", "--worktree", cwd}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Errorf("argv got %v want %v", gotArgs, want)
	}
}
