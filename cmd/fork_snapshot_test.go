package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyTranscript(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa.jsonl")
	content := []byte(`{"type":"user"}` + "\n" + `{"type":"assistant"}` + "\n")
	if err := os.WriteFile(src, content, 0o600); err != nil {
		t.Fatal(err)
	}

	newID, newPath, err := copyTranscript(src)
	if err != nil {
		t.Fatalf("copyTranscript: %v", err)
	}
	if newID == "" {
		t.Fatal("expected non-empty new ID")
	}
	if filepath.Dir(newPath) != dir {
		t.Fatalf("new file should be in same dir; got %s", newPath)
	}
	if filepath.Base(newPath) != newID+".jsonl" {
		t.Fatalf("new file name should be <id>.jsonl; got %s", filepath.Base(newPath))
	}
	got, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("copy not byte-identical:\nwant %q\ngot  %q", content, got)
	}
	// New ID must differ from source ID
	if newID == "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa" {
		t.Fatal("new ID must differ from source ID")
	}
}

func TestCopyTranscriptMissingSource(t *testing.T) {
	_, _, err := copyTranscript(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err == nil {
		t.Fatal("expected error for missing source, got nil")
	}
}
