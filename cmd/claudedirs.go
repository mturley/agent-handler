package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// detectClaudeDirs scans home for Claude Code configuration directories by
// grepping for the marker file each one uses: the default ~/.claude pairs
// with ~/.claude.json at the home root, while any CLAUDE_CONFIG_DIR-style
// directory (e.g. ~/.claude-personal) keeps its own .claude.json inside
// itself. Returns absolute paths, sorted, with ~/.claude first if present.
func detectClaudeDirs(home string) []string {
	var found []string

	if _, err := os.Stat(filepath.Join(home, ".claude.json")); err == nil {
		if info, err := os.Stat(filepath.Join(home, ".claude")); err == nil && info.IsDir() {
			found = append(found, filepath.Join(home, ".claude"))
		}
	}

	entries, err := os.ReadDir(home)
	if err != nil {
		return found
	}
	var others []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == ".claude" || !strings.HasPrefix(name, ".claude") {
			continue
		}
		if _, err := os.Stat(filepath.Join(home, name, ".claude.json")); err == nil {
			others = append(others, filepath.Join(home, name))
		}
	}
	sort.Strings(others)
	found = append(found, others...)

	return found
}

// selectClaudeDirs detects available Claude config directories and, when
// more than one is found and the caller is interactive, asks which to use
// (defaulting to all). Falls back to ~/.claude when none are detected, so
// behavior on a machine with a single conventional install is unchanged.
func selectClaudeDirs(home string, yes bool) ([]string, error) {
	detected := detectClaudeDirs(home)
	if len(detected) == 0 {
		return []string{filepath.Join(home, ".claude")}, nil
	}
	if len(detected) == 1 {
		return detected, nil
	}

	fmt.Println("Detected multiple Claude Code configuration directories:")
	for i, dir := range detected {
		fmt.Printf("  %d) %s\n", i+1, dir)
	}
	fmt.Println("")

	if yes {
		fmt.Println("  Using all detected directories (non-interactive mode).")
		fmt.Println("")
		return detected, nil
	}

	fmt.Printf("Which should be used? [1-%d, comma-separated, default: all] ", len(detected))
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		fmt.Println("")
		return detected, nil
	}

	var selected []string
	seen := make(map[int]bool)
	for _, part := range strings.Split(line, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		idx, err := strconv.Atoi(part)
		if err != nil || idx < 1 || idx > len(detected) {
			return nil, fmt.Errorf("invalid selection %q", part)
		}
		if seen[idx] {
			continue
		}
		seen[idx] = true
		selected = append(selected, detected[idx-1])
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no directories selected")
	}
	sort.Strings(selected)
	fmt.Println("")
	return selected, nil
}
