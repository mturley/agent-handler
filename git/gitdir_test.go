package git

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResolveGitDirMainRepo(t *testing.T) {
	repo := newRepo(t)
	dirs, ok := ResolveGitDir(repo)
	if !ok {
		t.Fatalf("expected ok")
	}
	want := filepath.Join(repo, ".git")
	if dirs.GitDir != want {
		t.Errorf("GitDir = %q, want %q", dirs.GitDir, want)
	}
	if dirs.CommonDir != dirs.GitDir {
		t.Errorf("CommonDir = %q, want %q", dirs.CommonDir, dirs.GitDir)
	}
}

func TestResolveGitDirFromSubdir(t *testing.T) {
	repo := newRepo(t)
	sub := filepath.Join(repo, "a", "b")
	if err := exec.Command("mkdir", "-p", sub).Run(); err != nil {
		t.Fatal(err)
	}
	dirs, ok := ResolveGitDir(sub)
	if !ok {
		t.Fatalf("expected ok")
	}
	want := filepath.Join(repo, ".git")
	if dirs.GitDir != want {
		t.Errorf("GitDir = %q, want %q", dirs.GitDir, want)
	}
	if dirs.CommonDir != dirs.GitDir {
		t.Errorf("CommonDir = %q, want %q", dirs.CommonDir, dirs.GitDir)
	}
}

func TestResolveGitDirLinkedWorktree(t *testing.T) {
	repo := newRepo(t)
	wt := filepath.Join(filepath.Dir(repo), filepath.Base(repo)+"-wt")
	runIn(t, repo, "worktree", "add", wt, "-b", "feat")

	dirs, ok := ResolveGitDir(wt)
	if !ok {
		t.Fatalf("expected ok")
	}

	wantGitDir, err := filepath.EvalSymlinks(filepath.Join(repo, ".git", "worktrees", filepath.Base(wt)))
	if err != nil {
		t.Fatal(err)
	}
	gotGitDir, err := filepath.EvalSymlinks(dirs.GitDir)
	if err != nil {
		t.Fatal(err)
	}
	if gotGitDir != wantGitDir {
		t.Errorf("GitDir = %q, want %q", gotGitDir, wantGitDir)
	}

	wantCommonDir, err := filepath.EvalSymlinks(filepath.Join(repo, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	gotCommonDir, err := filepath.EvalSymlinks(dirs.CommonDir)
	if err != nil {
		t.Fatal(err)
	}
	if gotCommonDir != wantCommonDir {
		t.Errorf("CommonDir = %q, want %q", gotCommonDir, wantCommonDir)
	}
}

func TestResolveGitDirNonGit(t *testing.T) {
	dir := t.TempDir()
	_, ok := ResolveGitDir(dir)
	if ok {
		t.Errorf("expected ok=false for non-git dir")
	}
}
