package discover

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoverSessionID(t *testing.T) {
	// Create temp Claude home directory
	claudeHome := t.TempDir()
	cwd := "/Users/test/project"

	// Create the expected project directory
	projectDir := cwdToProjectDir(claudeHome, cwd)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	// Create a JSONL file
	sessionID := "session-abc123"
	jsonlPath := filepath.Join(projectDir, sessionID+".jsonl")
	if err := os.WriteFile(jsonlPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to write JSONL file: %v", err)
	}

	// Discover the session
	gotID, gotPath, err := DiscoverSessionID(claudeHome, cwd)
	if err != nil {
		t.Fatalf("DiscoverSessionID failed: %v", err)
	}

	if gotID != sessionID {
		t.Errorf("session ID: got %q, want %q", gotID, sessionID)
	}

	if gotPath != jsonlPath {
		t.Errorf("JSONL path: got %q, want %q", gotPath, jsonlPath)
	}
}

func TestDiscoverSessionIDPicksMostRecent(t *testing.T) {
	// Create temp Claude home directory
	claudeHome := t.TempDir()
	cwd := "/Users/test/project"

	// Create the expected project directory
	projectDir := cwdToProjectDir(claudeHome, cwd)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	// Create two JSONL files
	olderSession := "session-old"
	newerSession := "session-new"

	olderPath := filepath.Join(projectDir, olderSession+".jsonl")
	newerPath := filepath.Join(projectDir, newerSession+".jsonl")

	if err := os.WriteFile(olderPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to write older JSONL file: %v", err)
	}
	if err := os.WriteFile(newerPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to write newer JSONL file: %v", err)
	}

	// Make the older file actually older by setting its modification time
	oldTime := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(olderPath, oldTime, oldTime); err != nil {
		t.Fatalf("failed to set older file time: %v", err)
	}

	// Discover the session - should pick the newer one
	gotID, gotPath, err := DiscoverSessionID(claudeHome, cwd)
	if err != nil {
		t.Fatalf("DiscoverSessionID failed: %v", err)
	}

	if gotID != newerSession {
		t.Errorf("session ID: got %q, want %q (should pick most recent)", gotID, newerSession)
	}

	if gotPath != newerPath {
		t.Errorf("JSONL path: got %q, want %q", gotPath, newerPath)
	}
}

func TestDiscoverSessionIDNoJSONLFiles(t *testing.T) {
	// Create temp Claude home directory
	claudeHome := t.TempDir()
	cwd := "/Users/test/project"

	// Create the expected project directory but no JSONL files
	projectDir := cwdToProjectDir(claudeHome, cwd)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	// Discover the session - should fail
	_, _, err := DiscoverSessionID(claudeHome, cwd)
	if err == nil {
		t.Fatal("expected error when no .jsonl files exist, got nil")
	}
}

func TestDiscoverSessionIDProjectDirNotExist(t *testing.T) {
	// Create temp Claude home directory but don't create project dir
	claudeHome := t.TempDir()
	cwd := "/Users/test/nonexistent"

	// Discover the session - should fail
	_, _, err := DiscoverSessionID(claudeHome, cwd)
	if err == nil {
		t.Fatal("expected error when project directory doesn't exist, got nil")
	}
}

func TestCwdToProjectDir(t *testing.T) {
	tests := []struct {
		name       string
		claudeHome string
		cwd        string
		want       string
	}{
		{
			name:       "basic path",
			claudeHome: "/home/user/.claude",
			cwd:        "/Users/test/project",
			want:       "/home/user/.claude/projects/-Users-test-project",
		},
		{
			name:       "nested path",
			claudeHome: "/home/user/.claude",
			cwd:        "/Users/test/deep/nested/project",
			want:       "/home/user/.claude/projects/-Users-test-deep-nested-project",
		},
		{
			name:       "root path",
			claudeHome: "/home/user/.claude",
			cwd:        "/project",
			want:       "/home/user/.claude/projects/-project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cwdToProjectDir(tt.claudeHome, tt.cwd)
			if got != tt.want {
				t.Errorf("cwdToProjectDir() = %q, want %q", got, tt.want)
			}
		})
	}
}
