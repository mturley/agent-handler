package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type Status struct {
	InGit         bool
	Branch        string
	DefaultBranch string
	Ahead         int
	Behind        int
	CommittedAdds int
	CommittedDels int
	Modified      int
	Untracked     int
	UncommittedAdds int
	UncommittedDels int
	Rebasing      bool
	RebaseBranch  string
}

var shortstatInsertions = regexp.MustCompile(`(\d+) insertion`)
var shortstatDeletions = regexp.MustCompile(`(\d+) deletion`)

func gitCmd(cwd string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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
	s := &Status{DefaultBranch: "main"}

	// Check if we're in a git repo
	check := exec.Command("git", "-C", cwd, "rev-parse", "--git-dir")
	if err := check.Run(); err != nil {
		return s
	}
	s.InGit = true

	// Phase 1: independent lookups (parallel)
	var branch, defaultRaw, porcelain, uncommittedStat string
	var wg sync.WaitGroup
	wg.Add(4)
	go func() { defer wg.Done(); branch = gitCmd(cwd, "rev-parse", "--abbrev-ref", "HEAD") }()
	go func() { defer wg.Done(); defaultRaw = gitCmd(cwd, "symbolic-ref", "refs/remotes/origin/HEAD") }()
	go func() { defer wg.Done(); porcelain = gitCmd(cwd, "status", "--porcelain") }()
	go func() { defer wg.Done(); uncommittedStat = gitCmd(cwd, "diff", "HEAD", "--shortstat") }()
	wg.Wait()

	s.Branch = branch
	if defaultRaw != "" {
		s.DefaultBranch = strings.TrimPrefix(defaultRaw, "refs/remotes/origin/")
	}

	// Parse porcelain
	for _, line := range strings.Split(porcelain, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "??") {
			s.Untracked++
		} else {
			s.Modified++
		}
	}

	// Parse uncommitted stats
	s.UncommittedAdds, s.UncommittedDels = parseShortstat(uncommittedStat)

	// Detect rebase
	gitDir := gitCmd(cwd, "rev-parse", "--git-dir")
	if gitDir != "" {
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(cwd, gitDir)
		}
		for _, rebaseDir := range []string{"rebase-merge", "rebase-apply"} {
			headNameFile := filepath.Join(gitDir, rebaseDir, "head-name")
			if data, err := os.ReadFile(headNameFile); err == nil {
				s.Rebasing = true
				s.RebaseBranch = strings.TrimPrefix(strings.TrimSpace(string(data)), "refs/heads/")
				if s.Branch == "HEAD" {
					s.Branch = s.RebaseBranch
				}
				break
			}
		}
	}

	// Phase 2: merge-base dependent (parallel)
	if s.Branch != "" && s.Branch != s.DefaultBranch {
		baseRef := s.DefaultBranch
		for _, candidate := range []string{"upstream/" + s.DefaultBranch, "origin/" + s.DefaultBranch} {
			if gitCmd(cwd, "rev-parse", "--verify", candidate) != "" {
				baseRef = candidate
				break
			}
		}

		mergeBase := gitCmd(cwd, "merge-base", baseRef, "HEAD")
		if mergeBase != "" {
			var aheadStr, behindStr, diffStat string
			wg.Add(3)
			go func() { defer wg.Done(); aheadStr = gitCmd(cwd, "rev-list", "--count", mergeBase+"..HEAD") }()
			go func() { defer wg.Done(); behindStr = gitCmd(cwd, "rev-list", "--count", "HEAD.."+baseRef) }()
			go func() { defer wg.Done(); diffStat = gitCmd(cwd, "diff", "--shortstat", mergeBase+"..HEAD") }()
			wg.Wait()

			s.Ahead, _ = strconv.Atoi(aheadStr)
			s.Behind, _ = strconv.Atoi(behindStr)
			s.CommittedAdds, s.CommittedDels = parseShortstat(diffStat)
		}
	}

	return s
}
