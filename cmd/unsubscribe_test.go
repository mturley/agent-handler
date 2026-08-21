package cmd

import (
	"os"
	"os/exec"
	"testing"

	"github.com/mturley/agent-handler/worktreeinterop"
)

// TestUnsubscribe_NoWorktreeCall verifies that `unsubscribe` never shells out
// to the worktree CLI, even when the worktree binary is available on PATH.
func TestUnsubscribe_NoWorktreeCall(t *testing.T) {
	isolateHomes(t)

	callCount := 0
	restore := worktreeinterop.SetSeamsForTest(
		func(name string, args ...string) *exec.Cmd {
			callCount++
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

	// Subscribe first so there's something to unsubscribe from.
	subResource = "custom:o/r#1"
	subURL = "u1"
	t.Cleanup(func() { subResource = ""; subURL = "" })
	if err := subscribeCmd.Flags().Set("session-id", "test-session"); err != nil {
		t.Fatalf("set session-id flag: %v", err)
	}
	t.Cleanup(func() { subscribeCmd.Flags().Set("session-id", "") })
	if err := runSubscribe(subscribeCmd, nil); err != nil {
		t.Fatalf("runSubscribe: %v", err)
	}
	callCount = 0 // reset: only care about calls made during unsubscribe

	unsubResource = "custom:o/r#1"
	t.Cleanup(func() { unsubResource = "" })
	if err := unsubscribeCmd.Flags().Set("session-id", "test-session"); err != nil {
		t.Fatalf("set session-id flag: %v", err)
	}
	t.Cleanup(func() { unsubscribeCmd.Flags().Set("session-id", "") })

	if err := runUnsubscribe(unsubscribeCmd, nil); err != nil {
		t.Fatalf("runUnsubscribe: %v", err)
	}

	if callCount != 0 {
		t.Errorf("expected 0 worktree calls during unsubscribe, got %d", callCount)
	}
}
