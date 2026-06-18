package worktree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadResources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".worktree-resources")
	os.WriteFile(path, []byte("pr:owner/repo#42 https://github.com/owner/repo/pull/42\njira:RHOAIENG-100 https://redhat.atlassian.net/browse/RHOAIENG-100\n"), 0644)

	resources, err := ReadResources(path)
	if err != nil {
		t.Fatalf("ReadResources: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}
	if resources[0].ID != "pr:owner/repo#42" {
		t.Errorf("expected pr:owner/repo#42, got %s", resources[0].ID)
	}
	if !resources[0].Primary {
		t.Error("expected first resource to be primary")
	}
}

func TestReadResourcesWithRelated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".worktree-resources")
	content := "pr:owner/repo#42 https://github.com/owner/repo/pull/42\n~ pr:owner/repo#40 https://github.com/owner/repo/pull/40\n~ jira:RHOAIENG-99 https://redhat.atlassian.net/browse/RHOAIENG-99\n"
	os.WriteFile(path, []byte(content), 0644)

	resources, err := ReadResources(path)
	if err != nil {
		t.Fatalf("ReadResources: %v", err)
	}
	if len(resources) != 3 {
		t.Fatalf("expected 3 resources, got %d", len(resources))
	}
	if !resources[0].Primary {
		t.Error("expected first resource to be primary")
	}
	if resources[1].Primary {
		t.Error("expected second resource to be related (not primary)")
	}
	if resources[1].ID != "pr:owner/repo#40" {
		t.Errorf("expected pr:owner/repo#40, got %s", resources[1].ID)
	}
	if resources[2].Primary {
		t.Error("expected third resource to be related")
	}
}

func TestReadResourcesSkipsMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".worktree-resources")
	os.WriteFile(path, []byte("pr:owner/repo#42 https://url\n\nbadline\njira:X https://y\n"), 0644)

	resources, _ := ReadResources(path)
	if len(resources) != 2 {
		t.Errorf("expected 2 valid resources (skipping malformed), got %d", len(resources))
	}
}

func TestReadResourcesFileNotExist(t *testing.T) {
	resources, err := ReadResources("/nonexistent/.worktree-resources")
	if err != nil {
		t.Errorf("expected no error for missing file, got %v", err)
	}
	if len(resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(resources))
	}
}

func TestAppendResourcePrimary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".worktree-resources")

	err := AppendResource(path, "pr:owner/repo#42", "https://github.com/owner/repo/pull/42", true)
	if err != nil {
		t.Fatalf("AppendResource: %v", err)
	}

	resources, _ := ReadResources(path)
	if len(resources) != 1 {
		t.Fatalf("expected 1, got %d", len(resources))
	}
	if !resources[0].Primary {
		t.Error("expected primary")
	}
}

func TestAppendResourceRelated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".worktree-resources")

	AppendResource(path, "pr:owner/repo#42", "https://url", true)
	AppendResource(path, "pr:owner/repo#40", "https://url2", false)

	resources, _ := ReadResources(path)
	if len(resources) != 2 {
		t.Fatalf("expected 2, got %d", len(resources))
	}
	if !resources[0].Primary {
		t.Error("expected first to be primary")
	}
	if resources[1].Primary {
		t.Error("expected second to be related")
	}
}

func TestAppendResourceDeduplicates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".worktree-resources")

	AppendResource(path, "pr:owner/repo#42", "https://url", true)
	AppendResource(path, "pr:owner/repo#42", "https://url", true)

	resources, _ := ReadResources(path)
	if len(resources) != 1 {
		t.Errorf("expected 1 (deduplicated), got %d", len(resources))
	}
}

func TestRemoveResource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".worktree-resources")
	os.WriteFile(path, []byte("pr:owner/repo#42 https://url1\n~ jira:X https://url2\n"), 0644)

	err := RemoveResource(path, "pr:owner/repo#42")
	if err != nil {
		t.Fatalf("RemoveResource: %v", err)
	}

	resources, _ := ReadResources(path)
	if len(resources) != 1 {
		t.Fatalf("expected 1, got %d", len(resources))
	}
	if resources[0].ID != "jira:X" {
		t.Errorf("expected jira:X to remain, got %s", resources[0].ID)
	}
	if resources[0].Primary {
		t.Error("expected remaining resource to still be related")
	}
}

func TestRemoveResourcePreservesMarkers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".worktree-resources")
	os.WriteFile(path, []byte("pr:owner/repo#42 https://url1\n~ pr:owner/repo#40 https://url2\njira:X https://url3\n"), 0644)

	RemoveResource(path, "pr:owner/repo#42")

	resources, _ := ReadResources(path)
	if len(resources) != 2 {
		t.Fatalf("expected 2, got %d", len(resources))
	}
	if resources[0].Primary {
		t.Error("expected pr#40 to still be related")
	}
	if !resources[1].Primary {
		t.Error("expected jira:X to still be primary")
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
