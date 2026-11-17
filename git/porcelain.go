package git

import "strings"

// porcelainV2 is the parsed result of `git status --porcelain=v2 --branch`.
type porcelainV2 struct {
	Head      string // branch name, or "(detached)"
	Oid       string
	Modified  int
	Untracked int
}

// parsePorcelainV2 parses the output of
// `git status --porcelain=v2 --branch --no-lock-index`.
func parsePorcelainV2(out string) porcelainV2 {
	var p porcelainV2
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "# branch.oid "):
			p.Oid = strings.TrimPrefix(line, "# branch.oid ")
		case strings.HasPrefix(line, "# branch.head "):
			p.Head = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "), strings.HasPrefix(line, "u "):
			p.Modified++
		case strings.HasPrefix(line, "? "):
			p.Untracked++
		}
		// Other headers (e.g. "# branch.upstream", "# branch.ab") and "!"
		// (ignored) entries are intentionally ignored.
	}
	return p
}
