package git

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type Status struct {
	InGit           bool
	Branch          string
	DefaultBranch   string
	Ahead           int
	Behind          int
	CommittedAdds   int
	CommittedDels   int
	Modified        int
	Untracked       int
	UncommittedAdds int
	UncommittedDels int
	Rebasing        bool
	RebaseBranch    string
}

// baseInfo holds everything derived from comparing the current branch
// against its default/base branch.
type baseInfo struct {
	DefaultBranch string
	BaseRef       string
	MergeBase     string
	Ahead         int
	Behind        int
	CommittedAdds int
	CommittedDels int
}

var shortstatInsertions = regexp.MustCompile(`(\d+) insertion`)
var shortstatDeletions = regexp.MustCompile(`(\d+) deletion`)

// gitCmd is a thin wrapper over runGit that returns "" on error, preserving
// the old call-site ergonomics for code that doesn't care about errors.
func gitCmd(cwd string, args ...string) string {
	out, err := runGit(cwd, args...)
	if err != nil {
		return ""
	}
	return out
}

func parseShortstat(s string) (adds, dels int) {
	if m := shortstatInsertions.FindStringSubmatch(s); len(m) > 1 {
		adds, _ = strconv.Atoi(m[1])
	}
	if m := shortstatDeletions.FindStringSubmatch(s); len(m) > 1 {
		dels, _ = strconv.Atoi(m[1])
	}
	return
}

// GetStatus gathers git status for the given working directory.
func GetStatus(cwd string) *Status {
	dirs, ok := ResolveGitDir(cwd)
	if !ok {
		return &Status{DefaultBranch: "main"}
	}
	return computeStatus(cwd, dirs, func(branch string) baseInfo {
		return computeBase(cwd, dirs, branch)
	})
}

// computeStatus computes a full, uncached Status for a resolved git
// repository: one `status --porcelain=v2 --branch` call and one
// `diff HEAD --shortstat` call in parallel, rebase detection via the
// filesystem, and base(branch) for the default-branch comparison (GetStatus
// passes computeBase; GetStatusCached injects a cached lookup).
func computeStatus(cwd string, dirs GitDirs, base func(branch string) baseInfo) *Status {
	s := &Status{InGit: true, DefaultBranch: "main"}

	var statusOut, uncommittedStat string
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		// --no-optional-locks is a global flag (must precede the subcommand)
		// that avoids taking the index lock while other git processes run;
		// the old code passed the nonexistent "--no-lock-index" as a status
		// flag, which git rejects outright (see status_parity_test.go).
		statusOut = gitCmd(cwd, "--no-optional-locks", "status", "--porcelain=v2", "--branch")
	}()
	go func() {
		defer wg.Done()
		uncommittedStat = gitCmd(cwd, "diff", "HEAD", "--shortstat")
	}()
	wg.Wait()

	p := parsePorcelainV2(statusOut)
	switch {
	case p.Head == "(detached)":
		s.Branch = "HEAD"
	case p.Oid == "(initial)":
		s.Branch = ""
	default:
		s.Branch = p.Head
	}
	s.Modified = p.Modified
	s.Untracked = p.Untracked

	s.UncommittedAdds, s.UncommittedDels = parseShortstat(uncommittedStat)

	// Detect rebase via the filesystem.
	for _, rebaseDir := range []string{"rebase-merge", "rebase-apply"} {
		headNameFile := filepath.Join(dirs.GitDir, rebaseDir, "head-name")
		if data, err := os.ReadFile(headNameFile); err == nil {
			s.Rebasing = true
			s.RebaseBranch = strings.TrimPrefix(strings.TrimSpace(string(data)), "refs/heads/")
			if s.Branch == "HEAD" {
				s.Branch = s.RebaseBranch
			}
			break
		}
	}

	b := base(s.Branch)
	s.DefaultBranch = b.DefaultBranch
	if s.Branch != "" && s.Branch != s.DefaultBranch {
		s.Ahead = b.Ahead
		s.Behind = b.Behind
		s.CommittedAdds = b.CommittedAdds
		s.CommittedDels = b.CommittedDels
	}

	return s
}

// computeBase resolves the default branch and, when branch differs from it,
// the base ref, merge base, ahead/behind counts and committed diff stats.
// This is the Phase-2 logic from the pre-refactor GetStatus, moved out
// unchanged, plus a filesystem-first default-branch lookup.
func computeBase(cwd string, dirs GitDirs, branch string) baseInfo {
	b := baseInfo{DefaultBranch: "main"}

	if def := defaultBranchFromFile(dirs.CommonDir); def != "" {
		b.DefaultBranch = def
	} else if defaultRaw := gitCmd(cwd, "symbolic-ref", "refs/remotes/origin/HEAD"); defaultRaw != "" {
		b.DefaultBranch = strings.TrimPrefix(defaultRaw, "refs/remotes/origin/")
	}

	if branch == "" || branch == b.DefaultBranch {
		return b
	}

	baseRef := b.DefaultBranch
	for _, candidate := range []string{"upstream/" + b.DefaultBranch, "origin/" + b.DefaultBranch} {
		if gitCmd(cwd, "rev-parse", "--verify", candidate) != "" {
			baseRef = candidate
			break
		}
	}
	b.BaseRef = baseRef

	mergeBase := gitCmd(cwd, "merge-base", baseRef, "HEAD")
	if mergeBase == "" {
		return b
	}
	b.MergeBase = mergeBase

	var aheadStr, behindStr, diffStat string
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); aheadStr = gitCmd(cwd, "rev-list", "--count", mergeBase+"..HEAD") }()
	go func() { defer wg.Done(); behindStr = gitCmd(cwd, "rev-list", "--count", "HEAD.."+baseRef) }()
	go func() { defer wg.Done(); diffStat = gitCmd(cwd, "diff", "--shortstat", mergeBase+"..HEAD") }()
	wg.Wait()

	b.Ahead, _ = strconv.Atoi(aheadStr)
	b.Behind, _ = strconv.Atoi(behindStr)
	b.CommittedAdds, b.CommittedDels = parseShortstat(diffStat)

	return b
}

// defaultBranchFromFile reads <commonDir>/refs/remotes/origin/HEAD, which
// holds "ref: refs/remotes/origin/<branch>" when present, avoiding a
// symbolic-ref spawn in the common case. Returns "" if the file is missing
// or unparseable.
func defaultBranchFromFile(commonDir string) string {
	data, err := os.ReadFile(filepath.Join(commonDir, "refs", "remotes", "origin", "HEAD"))
	if err != nil {
		return ""
	}
	content := strings.TrimSpace(string(data))
	const prefix = "ref: refs/remotes/origin/"
	if !strings.HasPrefix(content, prefix) {
		return ""
	}
	return strings.TrimPrefix(content, prefix)
}
