package git

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mturley/agent-handler/internal/fscache"
)

// fakeClock installs a settable clock as now and returns a pointer to it.
func fakeClock(t *testing.T) *time.Time {
	t.Helper()
	cur := time.Now()
	orig := now
	now = func() time.Time { return cur }
	t.Cleanup(func() { now = orig })
	return &cur
}

// countingByCmd counts all runGit calls and merge-base calls.
func countingByCmd(t *testing.T) (total, mergeBase *int) {
	t.Helper()
	var mu sync.Mutex
	var tot, mb int
	orig := runGit
	runGit = func(cwd string, args ...string) (string, error) {
		mu.Lock()
		tot++
		if len(args) > 0 && args[0] == "merge-base" {
			mb++
		}
		mu.Unlock()
		return orig(cwd, args...)
	}
	t.Cleanup(func() { runGit = orig })
	return &tot, &mb
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func statusFile(t *testing.T, cacheDir string) string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(cacheDir, "status-*.json"))
	if len(m) != 1 {
		t.Fatalf("status files = %v, want 1", m)
	}
	return m[0]
}

func TestCachedStatusWarmHitSpawnsNothing(t *testing.T) {
	fakeClock(t)
	dir, cache := newRepo(t), t.TempDir()
	count := countingRunner(t)
	first := GetStatusCached(dir, cache)
	if *count == 0 {
		t.Fatal("cold call spawned nothing")
	}
	before := *count
	second := GetStatusCached(dir, cache)
	if *count != before {
		t.Errorf("warm call spawned %d", *count-before)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("first %+v != second %+v", first, second)
	}
}

func TestCachedStatusInvalidatesOnCommit(t *testing.T) {
	fakeClock(t)
	dir, cache := newRepo(t), t.TempDir()
	runIn(t, dir, "checkout", "-b", "feat")
	count := countingRunner(t)
	if got := GetStatusCached(dir, cache); got.Ahead != 0 {
		t.Fatalf("Ahead = %d", got.Ahead)
	}
	runIn(t, dir, "commit", "--allow-empty", "-m", "c1")
	before := *count
	got := GetStatusCached(dir, cache)
	if *count == before || got.Ahead != 1 || got.Branch != "feat" {
		t.Errorf("after commit: delta=%d %+v", *count-before, got)
	}
}

func TestCachedStatusInvalidatesOnStage(t *testing.T) {
	fakeClock(t)
	dir, cache := newRepo(t), t.TempDir()
	first := GetStatusCached(dir, cache)
	writeFile(t, filepath.Join(dir, "new.txt"), "x\n")
	runIn(t, dir, "add", "new.txt")
	got := GetStatusCached(dir, cache)
	if got.Modified <= first.Modified {
		t.Errorf("Modified %d -> %d", first.Modified, got.Modified)
	}
}

func TestCachedStatusInvalidatesOnCheckout(t *testing.T) {
	fakeClock(t)
	dir, cache := newRepo(t), t.TempDir()
	GetStatusCached(dir, cache)
	runIn(t, dir, "checkout", "-b", "other")
	if got := GetStatusCached(dir, cache); got.Branch != "other" {
		t.Errorf("Branch = %q", got.Branch)
	}
}

func TestCachedStatusRefreshesAfterTTL(t *testing.T) {
	clock := fakeClock(t)
	dir, cache := newRepo(t), t.TempDir()
	start := *clock
	first := GetStatusCached(dir, cache)
	writeFile(t, filepath.Join(dir, "README.md"), "changed\n")
	*clock = start.Add(29 * time.Second)
	if got := GetStatusCached(dir, cache); got.Modified != first.Modified {
		t.Errorf("+29s Modified = %d, want cached %d", got.Modified, first.Modified)
	}
	*clock = start.Add(31 * time.Second)
	if got := GetStatusCached(dir, cache); got.Modified != first.Modified+1 {
		t.Errorf("+31s Modified = %d, want %d", got.Modified, first.Modified+1)
	}
}

func TestBaseCacheReusedAcrossDirtyChanges(t *testing.T) {
	fakeClock(t)
	dir, cache := newRepo(t), t.TempDir()
	runIn(t, dir, "checkout", "-b", "feat")
	runIn(t, dir, "commit", "--allow-empty", "-m", "c1")
	total, mb := countingByCmd(t)
	GetStatusCached(dir, cache)
	writeFile(t, filepath.Join(dir, "new.txt"), "x\n")
	runIn(t, dir, "add", "new.txt")
	before := *total
	got := GetStatusCached(dir, cache)
	if *total == before {
		t.Error("status not recomputed after stage")
	}
	if *mb != 1 {
		t.Errorf("merge-base count = %d, want 1", *mb)
	}
	if got.Ahead != 1 {
		t.Errorf("Ahead = %d", got.Ahead)
	}
}

func TestBaseCacheExpiresAfter10m(t *testing.T) {
	clock := fakeClock(t)
	dir, cache := newRepo(t), t.TempDir()
	runIn(t, dir, "checkout", "-b", "feat")
	_, mb := countingByCmd(t)
	GetStatusCached(dir, cache)
	*clock = clock.Add(11 * time.Minute)
	GetStatusCached(dir, cache)
	if *mb != 2 {
		t.Errorf("merge-base count = %d, want 2", *mb)
	}
}

func TestBaseCacheInvalidatesWhenBaseRefMoves(t *testing.T) {
	clock := fakeClock(t)
	dir, cache := newRepo(t), t.TempDir()
	runIn(t, dir, "update-ref", "refs/remotes/origin/main", "main")
	runIn(t, dir, "checkout", "-b", "feat")
	_, mb := countingByCmd(t)
	first := GetStatusCached(dir, cache)
	tree := strings.TrimSpace(runIn(t, dir, "rev-parse", "origin/main^{tree}"))
	next := strings.TrimSpace(runIn(t, dir, "commit-tree", tree, "-p", "origin/main", "-m", "upstream"))
	runIn(t, dir, "update-ref", "refs/remotes/origin/main", next)
	*clock = clock.Add(31 * time.Second)
	got := GetStatusCached(dir, cache)
	if got.Behind != first.Behind+1 {
		t.Errorf("Behind %d -> %d", first.Behind, got.Behind)
	}
	if *mb != 2 {
		t.Errorf("merge-base count = %d, want 2", *mb)
	}
}

func TestBaseCacheOneFilePerBranch(t *testing.T) {
	fakeClock(t)
	dir, cache := newRepo(t), t.TempDir()
	runIn(t, dir, "checkout", "-b", "feat")
	for i := 0; i < 3; i++ {
		runIn(t, dir, "commit", "--allow-empty", "-m", "c")
		GetStatusCached(dir, cache)
	}
	m, _ := filepath.Glob(filepath.Join(cache, "base-*.json"))
	if len(m) != 1 {
		t.Errorf("base files = %v, want 1", m)
	}
}

func TestFingerprintTakenBeforeCompute(t *testing.T) {
	fakeClock(t)
	dir, cache := newRepo(t), t.TempDir()
	runIn(t, dir, "checkout", "-b", "feat")
	committed := false
	var mu sync.Mutex
	count := 0
	orig := runGit
	runGit = func(cwd string, args ...string) (string, error) {
		mu.Lock()
		count++
		doCommit := !committed && containsArg(args, "status")
		if doCommit {
			committed = true
		}
		mu.Unlock()
		if doCommit {
			if _, err := execGit(cwd, "commit", "--allow-empty", "-m", "mid"); err != nil {
				t.Errorf("mid-compute commit: %v", err)
			}
		}
		return orig(cwd, args...)
	}
	t.Cleanup(func() { runGit = orig })

	GetStatusCached(dir, cache)
	before := count
	got := GetStatusCached(dir, cache)
	if count == before {
		t.Error("second call was a hit; pre-compute fingerprint not used")
	}
	if got.Ahead != 1 {
		t.Errorf("Ahead = %d, want 1 (post-commit)", got.Ahead)
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestCachedStatusLinkedWorktree(t *testing.T) {
	fakeClock(t)
	dir, cache := newRepo(t), t.TempDir()
	wt := filepath.Join(t.TempDir(), "wt")
	runIn(t, dir, "worktree", "add", "-b", "feat", wt)
	count := countingRunner(t)
	first := GetStatusCached(wt, cache)
	if first.Branch != "feat" {
		t.Fatalf("Branch = %q", first.Branch)
	}
	before := *count
	GetStatusCached(wt, cache)
	if *count != before {
		t.Errorf("warm hit spawned %d", *count-before)
	}
	runIn(t, wt, "commit", "--allow-empty", "-m", "wt")
	before = *count
	got := GetStatusCached(wt, cache)
	if *count == before || got.Ahead != 1 {
		t.Errorf("after worktree commit: delta=%d %+v", *count-before, got)
	}
}

func TestCacheConcurrentWritersNeverCorrupt(t *testing.T) {
	dir, cache := newRepo(t), t.TempDir()
	want := GetStatus(dir)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var bad []*Status
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				got := GetStatusCached(dir, cache)
				if *got != *want {
					mu.Lock()
					bad = append(bad, got)
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	if len(bad) > 0 {
		t.Errorf("%d mismatched results, e.g. %+v want %+v", len(bad), bad[0], want)
	}
	var e statusEntry
	if !fscache.ReadJSON(statusFile(t, cache), &e) {
		t.Error("status file unreadable after concurrent writes")
	}
}

func TestCorruptCacheFileIsMiss(t *testing.T) {
	fakeClock(t)
	dir, cache := newRepo(t), t.TempDir()
	GetStatusCached(dir, cache)
	writeFile(t, statusFile(t, cache), "{")
	got := GetStatusCached(dir, cache)
	if want := GetStatus(dir); *got != *want {
		t.Errorf("got %+v want %+v", got, want)
	}
}
