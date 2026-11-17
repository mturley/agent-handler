package git

import (
	"os/exec"
	"strings"
)

// Runner executes a git command in the given working directory and returns
// its trimmed stdout. Tests swap the package-level runGit var to observe or
// stub git invocations without spawning real processes.
type Runner func(cwd string, args ...string) (string, error)

// runGit is the swappable package-level git command runner.
var runGit Runner = execGit

// execGit runs `git <args...>` with cwd set via `-C`, returning trimmed
// stdout.
func execGit(cwd string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
