package worktree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadResources(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, ".worktree-resources")

	// Write a file with 2 entries
	content := "pr:123 https://github.com/org/repo/pull/123\nissue:456 https://github.com/org/repo/issues/456\n"
	err := os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Read it back
	resources, err := ReadResources(filePath)
	if err != nil {
		t.Fatalf("ReadResources failed: %v", err)
	}

	if len(resources) != 2 {
		t.Fatalf("Expected 2 resources, got %d", len(resources))
	}

	if resources[0].ID != "pr:123" || resources[0].URL != "https://github.com/org/repo/pull/123" {
		t.Errorf("First resource mismatch: %+v", resources[0])
	}

	if resources[1].ID != "issue:456" || resources[1].URL != "https://github.com/org/repo/issues/456" {
		t.Errorf("Second resource mismatch: %+v", resources[1])
	}
}

func TestReadResourcesSkipsMalformed(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, ".worktree-resources")

	// Write a file with valid, empty, and malformed lines
	content := "pr:123 https://github.com/org/repo/pull/123\n\nmalformed-line\nissue:456 https://github.com/org/repo/issues/456\n"
	err := os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Read it back
	resources, err := ReadResources(filePath)
	if err != nil {
		t.Fatalf("ReadResources failed: %v", err)
	}

	// Should only get the 2 valid entries
	if len(resources) != 2 {
		t.Fatalf("Expected 2 resources, got %d", len(resources))
	}

	if resources[0].ID != "pr:123" {
		t.Errorf("First resource ID mismatch: %s", resources[0].ID)
	}

	if resources[1].ID != "issue:456" {
		t.Errorf("Second resource ID mismatch: %s", resources[1].ID)
	}
}

func TestReadResourcesFileNotExist(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, ".worktree-resources")

	// Read non-existent file
	resources, err := ReadResources(filePath)
	if err != nil {
		t.Fatalf("Expected no error for missing file, got: %v", err)
	}

	if resources != nil {
		t.Errorf("Expected nil resources for missing file, got: %+v", resources)
	}
}

func TestAppendResource(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, ".worktree-resources")

	// Append to empty file
	err := AppendResource(filePath, "pr:123", "https://github.com/org/repo/pull/123")
	if err != nil {
		t.Fatalf("AppendResource failed: %v", err)
	}

	// Read it back
	resources, err := ReadResources(filePath)
	if err != nil {
		t.Fatalf("ReadResources failed: %v", err)
	}

	if len(resources) != 1 {
		t.Fatalf("Expected 1 resource, got %d", len(resources))
	}

	if resources[0].ID != "pr:123" || resources[0].URL != "https://github.com/org/repo/pull/123" {
		t.Errorf("Resource mismatch: %+v", resources[0])
	}
}

func TestAppendResourceDeduplicates(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, ".worktree-resources")

	// Append the same ID twice
	err := AppendResource(filePath, "pr:123", "https://github.com/org/repo/pull/123")
	if err != nil {
		t.Fatalf("First AppendResource failed: %v", err)
	}

	err = AppendResource(filePath, "pr:123", "https://github.com/org/repo/pull/123")
	if err != nil {
		t.Fatalf("Second AppendResource failed: %v", err)
	}

	// Read it back
	resources, err := ReadResources(filePath)
	if err != nil {
		t.Fatalf("ReadResources failed: %v", err)
	}

	// Should only have 1 entry
	if len(resources) != 1 {
		t.Fatalf("Expected 1 resource after deduplication, got %d", len(resources))
	}

	if resources[0].ID != "pr:123" {
		t.Errorf("Resource ID mismatch: %s", resources[0].ID)
	}
}

func TestRemoveResource(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, ".worktree-resources")

	// Write a file with 2 entries
	content := "pr:123 https://github.com/org/repo/pull/123\nissue:456 https://github.com/org/repo/issues/456\n"
	err := os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Remove one entry
	err = RemoveResource(filePath, "pr:123")
	if err != nil {
		t.Fatalf("RemoveResource failed: %v", err)
	}

	// Read it back
	resources, err := ReadResources(filePath)
	if err != nil {
		t.Fatalf("ReadResources failed: %v", err)
	}

	// Should only have 1 entry left
	if len(resources) != 1 {
		t.Fatalf("Expected 1 resource after removal, got %d", len(resources))
	}

	if resources[0].ID != "issue:456" {
		t.Errorf("Remaining resource ID mismatch: %s", resources[0].ID)
	}
}

func TestParseResourceID(t *testing.T) {
	tests := []struct {
		input        string
		expectedType string
		expectedID   string
	}{
		{"pr:123", "pr", "123"},
		{"issue:456", "issue", "456"},
		{"jira:PROJ-789", "jira", "PROJ-789"},
		{"no-colon", "", "no-colon"},
		{"multiple:colons:here", "multiple", "colons:here"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			resType, resID := ParseResourceID(tt.input)
			if resType != tt.expectedType {
				t.Errorf("Expected type %q, got %q", tt.expectedType, resType)
			}
			if resID != tt.expectedID {
				t.Errorf("Expected ID %q, got %q", tt.expectedID, resID)
			}
		})
	}
}
