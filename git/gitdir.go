package git

import (
	"os"
	"path/filepath"
	"strings"
)

// GitDirs holds the resolved locations of a repository's git directory,
// common directory (shared across linked worktrees), and work tree root.
type GitDirs struct {
	GitDir    string
	CommonDir string
	WorkTree  string
}

// ResolveGitDir walks up from cwd looking for a `.git` entry, purely via the
// filesystem (no git spawn). If `.git` is a directory, GitDir and CommonDir
// are both that directory. If `.git` is a file (a linked worktree), it is
// parsed as `gitdir: <path>` (resolved relative to the file's directory),
// and CommonDir is read from `<gitdir>/commondir` (resolved relative to
// GitDir) if present, else falls back to GitDir itself.
func ResolveGitDir(cwd string) (GitDirs, bool) {
	dir := cwd
	for {
		candidate := filepath.Join(dir, ".git")
		info, err := os.Stat(candidate)
		if err == nil {
			if info.IsDir() {
				return GitDirs{GitDir: candidate, CommonDir: candidate, WorkTree: dir}, true
			}
			dirs, ok := resolveGitFile(candidate, dir)
			if ok {
				return dirs, true
			}
			return GitDirs{}, false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return GitDirs{}, false
		}
		dir = parent
	}
}

// resolveGitFile parses a `.git` file (linked worktree pointer) at
// gitFilePath, whose containing directory is workTree.
func resolveGitFile(gitFilePath, workTree string) (GitDirs, bool) {
	data, err := os.ReadFile(gitFilePath)
	if err != nil {
		return GitDirs{}, false
	}
	content := strings.TrimSpace(string(data))
	const prefix = "gitdir:"
	if !strings.HasPrefix(content, prefix) {
		return GitDirs{}, false
	}
	gitDirRaw := strings.TrimSpace(strings.TrimPrefix(content, prefix))
	if gitDirRaw == "" {
		return GitDirs{}, false
	}
	gitDir := gitDirRaw
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(workTree, gitDir)
	}

	commonDir := gitDir
	if data, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		raw := strings.TrimSpace(string(data))
		if raw != "" {
			if filepath.IsAbs(raw) {
				commonDir = raw
			} else {
				commonDir = filepath.Join(gitDir, raw)
			}
		}
	}

	return GitDirs{GitDir: gitDir, CommonDir: commonDir, WorkTree: workTree}, true
}
