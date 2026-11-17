package git

import (
	"sync"
	"testing"
)

// countingRunner wraps execGit with a call counter and installs itself as
// runGit, restoring the original on t.Cleanup. Returns a pointer to the count.
func countingRunner(t *testing.T) *int {
	t.Helper()
	var mu sync.Mutex
	count := 0
	orig := runGit
	runGit = func(cwd string, args ...string) (string, error) {
		mu.Lock()
		count++
		mu.Unlock()
		return orig(cwd, args...)
	}
	t.Cleanup(func() { runGit = orig })
	return &count
}

func TestGetStatusNonGitSpawnsNothing(t *testing.T) {
	count := countingRunner(t)
	dir := t.TempDir()
	got := GetStatus(dir)
	if got.InGit {
		t.Errorf("InGit = true, want false")
	}
	if got.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q, want %q", got.DefaultBranch, "main")
	}
	if *count != 0 {
		t.Errorf("spawn count = %d, want 0", *count)
	}
}
