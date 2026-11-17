package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// newRepo creates a temp git repo with one commit on main, configured with
// a local user.name/email so commits succeed without global git config.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.name", "Test")
	run("config", "user.email", "test@test.com")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-m", "initial commit")
	return dir
}

func runIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestGetStatusParity(t *testing.T) {
	t.Run("clean on main", func(t *testing.T) {
		dir := newRepo(t)
		got := GetStatus(dir)
		want := &Status{InGit: true, Branch: "main", DefaultBranch: "main"}
		if *got != *want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("dirty", func(t *testing.T) {
		dir := newRepo(t)
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\nworld\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0644); err != nil {
			t.Fatal(err)
		}
		got := GetStatus(dir)
		// NOTE: today's `git status --porcelain --no-lock-index` call errors out on
		// this git version (--no-lock-index is not a recognized flag), so gitCmd
		// returns "" and Modified/Untracked stay 0. This characterizes the ACTUAL
		// current behavior, bug and all.
		want := &Status{InGit: true, Branch: "main", DefaultBranch: "main", UncommittedAdds: 1}
		if *got != *want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("staged rename", func(t *testing.T) {
		dir := newRepo(t)
		runIn(t, dir, "mv", "README.md", "RENAMED.md")
		runIn(t, dir, "add", "-A")
		got := GetStatus(dir)
		// Same --no-lock-index issue as above: Modified stays 0 on this git version.
		want := &Status{InGit: true, Branch: "main", DefaultBranch: "main"}
		if *got != *want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("feature branch ahead", func(t *testing.T) {
		dir := newRepo(t)
		runIn(t, dir, "checkout", "-b", "feat")
		for i := 0; i < 2; i++ {
			if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(strRepeat("x", i+1)), 0644); err != nil {
				t.Fatal(err)
			}
			runIn(t, dir, "add", "f.txt")
			runIn(t, dir, "commit", "-m", "commit")
		}
		got := GetStatus(dir)
		if !got.InGit || got.Branch != "feat" || got.DefaultBranch != "main" || got.Ahead != 2 || got.Behind != 0 {
			t.Errorf("got %+v", got)
		}
		if got.CommittedAdds == 0 {
			t.Errorf("expected nonzero CommittedAdds, got %+v", got)
		}
	})

	t.Run("feature branch with origin behind", func(t *testing.T) {
		dir := newRepo(t)
		remoteDir := t.TempDir()
		runIn(t, remoteDir, "init", "--bare", "-b", "main")
		runIn(t, dir, "remote", "add", "origin", remoteDir)
		runIn(t, dir, "push", "origin", "main")
		runIn(t, dir, "checkout", "-b", "feat")

		// advance origin/main by one commit from a separate clone
		cloneDir := t.TempDir()
		runIn(t, cloneDir, "clone", remoteDir, ".")
		if err := os.WriteFile(filepath.Join(cloneDir, "other.txt"), []byte("other\n"), 0644); err != nil {
			t.Fatal(err)
		}
		runIn(t, cloneDir, "add", "other.txt")
		runIn(t, cloneDir, "commit", "-m", "upstream commit")
		runIn(t, cloneDir, "push", "origin", "main")
		runIn(t, dir, "fetch", "origin")

		got := GetStatus(dir)
		if !got.InGit || got.Branch != "feat" || got.DefaultBranch != "main" || got.Ahead != 0 || got.Behind != 1 {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("detached HEAD", func(t *testing.T) {
		dir := newRepo(t)
		sha := runIn(t, dir, "rev-parse", "HEAD")
		runIn(t, dir, "checkout", trim(sha))
		got := GetStatus(dir)
		want := &Status{InGit: true, Branch: "HEAD", DefaultBranch: "main"}
		if *got != *want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("no upstream no origin", func(t *testing.T) {
		dir := newRepo(t)
		runIn(t, dir, "checkout", "-b", "solo")
		if err := os.WriteFile(filepath.Join(dir, "s.txt"), []byte("s\n"), 0644); err != nil {
			t.Fatal(err)
		}
		runIn(t, dir, "add", "s.txt")
		runIn(t, dir, "commit", "-m", "solo commit")
		got := GetStatus(dir)
		if !got.InGit || got.Branch != "solo" || got.DefaultBranch != "main" || got.Ahead != 1 || got.Behind != 0 {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("linked worktree on feature branch", func(t *testing.T) {
		dir := newRepo(t)
		wtDir := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-wt")
		runIn(t, dir, "worktree", "add", wtDir, "-b", "feat")
		got := GetStatus(wtDir)
		if !got.InGit || got.Branch != "feat" || got.DefaultBranch != "main" || got.Ahead != 0 || got.Behind != 0 {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("mid-rebase stopped on conflict", func(t *testing.T) {
		dir := newRepo(t)
		runIn(t, dir, "checkout", "-b", "feat")
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("feat change\n"), 0644); err != nil {
			t.Fatal(err)
		}
		runIn(t, dir, "add", "README.md")
		runIn(t, dir, "commit", "-m", "feat commit")

		runIn(t, dir, "checkout", "main")
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("main change\n"), 0644); err != nil {
			t.Fatal(err)
		}
		runIn(t, dir, "add", "README.md")
		runIn(t, dir, "commit", "-m", "main commit")

		runIn(t, dir, "checkout", "feat")
		cmd := exec.Command("git", "-C", dir, "rebase", "main")
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@test.com")
		_ = cmd.Run() // expected to fail with a conflict

		got := GetStatus(dir)
		if !got.InGit || !got.Rebasing || got.RebaseBranch != "feat" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("unborn", func(t *testing.T) {
		dir := t.TempDir()
		runIn(t, dir, "init", "-b", "main")
		runIn(t, dir, "config", "user.name", "Test")
		runIn(t, dir, "config", "user.email", "test@test.com")
		got := GetStatus(dir)
		want := &Status{InGit: true, Branch: "", DefaultBranch: "main"}
		if *got != *want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("cwd is subdirectory", func(t *testing.T) {
		dir := newRepo(t)
		sub := filepath.Join(dir, "sub")
		if err := os.MkdirAll(sub, 0755); err != nil {
			t.Fatal(err)
		}
		got := GetStatus(sub)
		want := &Status{InGit: true, Branch: "main", DefaultBranch: "main"}
		if *got != *want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})
}

func strRepeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out + "\n"
}

func trim(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}
