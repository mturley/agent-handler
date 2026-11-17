package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// repoScenario builds a repo in a particular shape and returns its root dir.
// Mirrors the scenarios exercised by TestGetStatusParity.
type repoScenario struct {
	name  string
	build func(t *testing.T) string
}

func scenarios() []repoScenario {
	return []repoScenario{
		{"clean on main", func(t *testing.T) string {
			return newRepo(t)
		}},
		{"dirty", func(t *testing.T) string {
			dir := newRepo(t)
			if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\nworld\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0644); err != nil {
				t.Fatal(err)
			}
			return dir
		}},
		{"feature branch ahead", func(t *testing.T) string {
			dir := newRepo(t)
			runIn(t, dir, "checkout", "-b", "feat")
			for i := 0; i < 2; i++ {
				if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(strRepeat("x", i+1)), 0644); err != nil {
					t.Fatal(err)
				}
				runIn(t, dir, "add", "f.txt")
				runIn(t, dir, "commit", "-m", "commit")
			}
			return dir
		}},
		{"detached HEAD", func(t *testing.T) string {
			dir := newRepo(t)
			sha := runIn(t, dir, "rev-parse", "HEAD")
			runIn(t, dir, "checkout", trim(sha))
			return dir
		}},
		{"no upstream no origin", func(t *testing.T) string {
			dir := newRepo(t)
			runIn(t, dir, "checkout", "-b", "solo")
			if err := os.WriteFile(filepath.Join(dir, "s.txt"), []byte("s\n"), 0644); err != nil {
				t.Fatal(err)
			}
			runIn(t, dir, "add", "s.txt")
			runIn(t, dir, "commit", "-m", "solo commit")
			return dir
		}},
		{"linked worktree on feature branch", func(t *testing.T) string {
			dir := newRepo(t)
			wtDir := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-wt")
			runIn(t, dir, "worktree", "add", wtDir, "-b", "feat")
			return wtDir
		}},
	}
}

// TestWarmStatuslineSpawnsZeroGit warms the status+base cache once per
// scenario, then confirms 5 repeated GetStatusCached calls spawn zero git
// processes and return results identical to an uncached GetStatus.
func TestWarmStatuslineSpawnsZeroGit(t *testing.T) {
	for _, sc := range scenarios() {
		t.Run(sc.name, func(t *testing.T) {
			dir := sc.build(t)
			cacheDir := t.TempDir()

			// Warm the cache (status + base entries) with an uncounted call.
			GetStatusCached(dir, cacheDir)

			want := GetStatus(dir)

			count := countingRunner(t)
			var got *Status
			for i := 0; i < 5; i++ {
				got = GetStatusCached(dir, cacheDir)
			}
			if *count != 0 {
				t.Errorf("spawn count after 5 warm calls = %d, want 0", *count)
			}
			if *got != *want {
				t.Errorf("warm GetStatusCached = %+v, want (uncached GetStatus) %+v", got, want)
			}
		})
	}
}

// coldSpawnBudget is the measured number of git spawns for a fully cold
// GetStatusCached call on the "feature branch ahead" scenario (local-only
// repo, no `origin` remote, branch 2 commits ahead of `main`). Measured
// directly against an instrumented runGit; stable across repeated runs.
// Breakdown (9 total):
//  1. --no-optional-locks status --porcelain=v2 --branch      (status pair)
//  2. diff HEAD --shortstat                                   (status pair, uncommitted stat)
//  3. symbolic-ref refs/remotes/origin/HEAD                   (default-branch fallback: no origin remote,
//     so refs/remotes/origin/HEAD doesn't exist on disk and computeBase must spawn)
//  4. rev-parse --verify upstream/main                        (no such remote, fails)
//  5. rev-parse --verify origin/main                           (no such remote, fails)
//  6. merge-base main HEAD
//  7. rev-list --count <mergeBase>..HEAD                       (ahead, base trio)
//  8. rev-list --count HEAD..main                              (behind, base trio)
//  9. diff --shortstat <mergeBase>..HEAD                       (committed adds/dels, base trio)
//
// The plan brief estimated <=7 but omitted #3 and #9; this is an arithmetic
// slip in the plan, confirmed by the plan owner, not a product constraint -
// the cold path is not being further optimized as part of this task.
const coldSpawnBudget = 9

// TestColdStatuslineSpawnBudget asserts the cold path doesn't regress beyond
// coldSpawnBudget, and that a second miss (triggered by staging a file, with
// the base cache still warm from the first cold call) only re-runs the
// status+uncommitted-diff pair, hitting the warm base cache.
func TestColdStatuslineSpawnBudget(t *testing.T) {
	dir := newRepo(t)
	runIn(t, dir, "checkout", "-b", "feat")
	for i := 0; i < 2; i++ {
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(strRepeat("x", i+1)), 0644); err != nil {
			t.Fatal(err)
		}
		runIn(t, dir, "add", "f.txt")
		runIn(t, dir, "commit", "-m", "commit")
	}

	cacheDir := t.TempDir()

	count := countingRunner(t)
	GetStatusCached(dir, cacheDir)
	if *count > coldSpawnBudget {
		t.Errorf("cold spawn count = %d, want <= %d", *count, coldSpawnBudget)
	}

	// Stage a file: the index mtime/size changes, so the status cache entry
	// (but not the base cache entry, keyed on HEAD sha which hasn't moved)
	// misses on the next call.
	if err := os.WriteFile(filepath.Join(dir, "g.txt"), []byte("g\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runIn(t, dir, "add", "g.txt")

	*count = 0
	GetStatusCached(dir, cacheDir)
	if *count != 2 {
		t.Errorf("second-miss (warm base cache) spawn count = %d, want 2", *count)
	}
}

// BenchmarkGetStatusCachedWarm measures GetStatusCached once the cache is
// warm: it should spawn no git processes per call.
func BenchmarkGetStatusCachedWarm(b *testing.B) {
	dir := newRepoB(b)
	cacheDir := b.TempDir()
	GetStatusCached(dir, cacheDir)

	spawns := 0
	orig := runGit
	runGit = func(cwd string, args ...string) (string, error) {
		spawns++
		return orig(cwd, args...)
	}
	defer func() { runGit = orig }()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetStatusCached(dir, cacheDir)
	}
	b.ReportMetric(float64(spawns)/float64(b.N), "spawns/op")
}

// BenchmarkGetStatusUncached measures the always-cold GetStatus path for
// comparison.
func BenchmarkGetStatusUncached(b *testing.B) {
	dir := newRepoB(b)

	spawns := 0
	orig := runGit
	runGit = func(cwd string, args ...string) (string, error) {
		spawns++
		return orig(cwd, args...)
	}
	defer func() { runGit = orig }()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetStatus(dir)
	}
	b.ReportMetric(float64(spawns)/float64(b.N), "spawns/op")
}

// newRepoB is newRepo for benchmarks, which take a *testing.B rather than a
// *testing.T. Duplicated rather than generified over TempDir/Fatalf, since
// *testing.B and *testing.T don't share an interface for those.
func newRepoB(b *testing.B) string {
	b.Helper()
	dir := b.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			b.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.name", "Test")
	run("config", "user.email", "test@test.com")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0644); err != nil {
		b.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-m", "initial commit")
	return dir
}
