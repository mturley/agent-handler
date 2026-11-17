package cmd

import (
	"testing"

	gitpkg "github.com/mturley/agent-handler/git"
)

// TestFormatGitLineUnchanged pins formatGitLine output; the golden strings
// were captured from formatGitLine before the git status cache was added.
func TestFormatGitLineUnchanged(t *testing.T) {
	cases := []struct {
		name string
		s    gitpkg.Status
		want string
	}{
		{"clean main", gitpkg.Status{InGit: true, Branch: "main", DefaultBranch: "main"},
			"on \x1b[1;37mmain\x1b[0m · \x1b[2;32mclean\x1b[0m"},
		{"dirty", gitpkg.Status{InGit: true, Branch: "main", DefaultBranch: "main", Modified: 2, Untracked: 1, UncommittedAdds: 5, UncommittedDels: 3},
			"on \x1b[1;37mmain\x1b[0m · \x1b[33m2 modified\x1b[0m, \x1b[33m1 untracked\x1b[0m (\x1b[32m+5\x1b[0m \x1b[31m−3\x1b[0m)"},
		{"ahead behind", gitpkg.Status{InGit: true, Branch: "feat", DefaultBranch: "main", Ahead: 2, Behind: 3, CommittedAdds: 10, CommittedDels: 4, Modified: 1},
			"\x1b[1;37mfeat\x1b[0m \x1b[32m↑2\x1b[0m (\x1b[32m+10\x1b[0m \x1b[31m−4\x1b[0m) · \x1b[33m1 modified\x1b[0m \x1b[2m↓3 behind main\x1b[0m"},
		{"detached", gitpkg.Status{InGit: true, Branch: "HEAD", DefaultBranch: "main"},
			"\x1b[1;37mHEAD\x1b[0m · \x1b[2;32mclean\x1b[0m"},
		{"rebasing", gitpkg.Status{InGit: true, Branch: "feat", DefaultBranch: "main", Rebasing: true, RebaseBranch: "feat", Modified: 1},
			"\x1b[33mrebasing\x1b[0m \x1b[1;37mfeat\x1b[0m · \x1b[33m1 modified\x1b[0m"},
	}
	for _, c := range cases {
		s := c.s
		if got := formatGitLine(&s); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
