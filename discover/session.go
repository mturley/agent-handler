package discover

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DiscoverSessionID computes the Claude project directory path from cwd,
// scans for .jsonl files, and returns the filename (minus .jsonl) of the
// most recently modified one as the session ID. Returns its full path as jsonlPath.
func DiscoverSessionID(claudeHome, cwd string) (sessionID, jsonlPath string, err error) {
	projectDir := cwdToProjectDir(claudeHome, cwd)

	// Check if project directory exists
	if _, err := os.Stat(projectDir); os.IsNotExist(err) {
		return "", "", fmt.Errorf("project directory does not exist: %s", projectDir)
	}

	// Find all .jsonl files in the project directory
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return "", "", fmt.Errorf("failed to read project directory: %w", err)
	}

	var mostRecentPath string
	var mostRecentTime time.Time

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}

		fullPath := filepath.Join(projectDir, name)
		info, err := entry.Info()
		if err != nil {
			continue // Skip files we can't stat
		}

		modTime := info.ModTime()
		if mostRecentPath == "" || modTime.After(mostRecentTime) {
			mostRecentPath = fullPath
			mostRecentTime = modTime
		}
	}

	if mostRecentPath == "" {
		return "", "", fmt.Errorf("no .jsonl files found in %s", projectDir)
	}

	// Extract session ID from filename
	base := filepath.Base(mostRecentPath)
	sessionID = strings.TrimSuffix(base, ".jsonl")

	return sessionID, mostRecentPath, nil
}

// cwdToProjectDir finds the Claude project directory for a given cwd.
// Claude encodes the path by replacing "/" and "." with "-" and prepending "-".
// We try the canonical encoding first, then fall back to "/" only.
func cwdToProjectDir(claudeHome, cwd string) string {
	cwd = filepath.Clean(cwd)
	projectsDir := filepath.Join(claudeHome, "projects")

	// Primary encoding: replace both / and . with -
	r := strings.NewReplacer("/", "-", ".", "-")
	projectName := r.Replace(cwd)
	if !strings.HasPrefix(projectName, "-") {
		projectName = "-" + projectName
	}
	candidate := filepath.Join(projectsDir, projectName)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}

	// Fallback: replace only / with -
	projectName = strings.ReplaceAll(cwd, "/", "-")
	if !strings.HasPrefix(projectName, "-") {
		projectName = "-" + projectName
	}
	return filepath.Join(projectsDir, projectName)
}
