package discover

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSessionName(t *testing.T) {
	// Create temp directory for test JSONL file
	tmpDir := t.TempDir()
	jsonlPath := filepath.Join(tmpDir, "test.jsonl")

	// JSONL with both agent-name and ai-title entries
	content := `{"type":"other","data":"ignored"}
{"type":"ai-title","aiTitle":"Some AI Title"}
{"type":"agent-name","agentName":"First Agent Name"}
{"type":"ai-title","aiTitle":"Another AI Title"}
{"type":"agent-name","agentName":"Second Agent Name"}
`
	if err := os.WriteFile(jsonlPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test JSONL: %v", err)
	}

	// Should return the last agent-name
	name, err := DiscoverSessionName(jsonlPath)
	if err != nil {
		t.Fatalf("DiscoverSessionName failed: %v", err)
	}

	want := "Second Agent Name"
	if name != want {
		t.Errorf("session name: got %q, want %q", name, want)
	}
}

func TestDiscoverSessionNameFallsBackToAITitle(t *testing.T) {
	// Create temp directory for test JSONL file
	tmpDir := t.TempDir()
	jsonlPath := filepath.Join(tmpDir, "test.jsonl")

	// JSONL with only ai-title entries
	content := `{"type":"other","data":"ignored"}
{"type":"ai-title","aiTitle":"First AI Title"}
{"type":"ai-title","aiTitle":"Second AI Title"}
`
	if err := os.WriteFile(jsonlPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test JSONL: %v", err)
	}

	// Should return the last ai-title
	name, err := DiscoverSessionName(jsonlPath)
	if err != nil {
		t.Fatalf("DiscoverSessionName failed: %v", err)
	}

	want := "Second AI Title"
	if name != want {
		t.Errorf("session name: got %q, want %q", name, want)
	}
}

func TestDiscoverSessionNameReturnsEmptyForNoEntries(t *testing.T) {
	// Create temp directory for test JSONL file
	tmpDir := t.TempDir()
	jsonlPath := filepath.Join(tmpDir, "test.jsonl")

	// JSONL with no name entries
	content := `{"type":"other","data":"ignored"}
{"type":"something-else","value":123}
`
	if err := os.WriteFile(jsonlPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test JSONL: %v", err)
	}

	// Should return empty string
	name, err := DiscoverSessionName(jsonlPath)
	if err != nil {
		t.Fatalf("DiscoverSessionName failed: %v", err)
	}

	if name != "" {
		t.Errorf("session name: got %q, want empty string", name)
	}
}

func TestDiscoverSessionNameHandlesMalformedJSON(t *testing.T) {
	// Create temp directory for test JSONL file
	tmpDir := t.TempDir()
	jsonlPath := filepath.Join(tmpDir, "test.jsonl")

	// JSONL with malformed lines mixed with valid entries
	content := `{"type":"agent-name","agentName":"Valid Name"}
{invalid json}
{"type":"ai-title","aiTitle":"Title After Error"}
not even json at all
{"type":"agent-name","agentName":"Final Valid Name"}
`
	if err := os.WriteFile(jsonlPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test JSONL: %v", err)
	}

	// Should skip malformed lines and return the last valid agent-name
	name, err := DiscoverSessionName(jsonlPath)
	if err != nil {
		t.Fatalf("DiscoverSessionName failed: %v", err)
	}

	want := "Final Valid Name"
	if name != want {
		t.Errorf("session name: got %q, want %q", name, want)
	}
}

func TestDiscoverSessionNameEmptyFile(t *testing.T) {
	// Create temp directory for test JSONL file
	tmpDir := t.TempDir()
	jsonlPath := filepath.Join(tmpDir, "test.jsonl")

	// Empty JSONL file
	if err := os.WriteFile(jsonlPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to write test JSONL: %v", err)
	}

	// Should return empty string
	name, err := DiscoverSessionName(jsonlPath)
	if err != nil {
		t.Fatalf("DiscoverSessionName failed: %v", err)
	}

	if name != "" {
		t.Errorf("session name: got %q, want empty string", name)
	}
}

func TestDiscoverSessionNameFileNotFound(t *testing.T) {
	// Use a path that doesn't exist
	jsonlPath := "/nonexistent/path/to/file.jsonl"

	// Should return an error
	_, err := DiscoverSessionName(jsonlPath)
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}
