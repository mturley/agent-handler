package cmd

import (
	"os"
	"path/filepath"
	"strings"
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

func TestParsePreCompactInput(t *testing.T) {
	raw := `{
	  "session_id": "dcbed3d2-1b23-4af8-aa1e-61d5d9b7dd99",
	  "transcript_path": "/x/y/dcbed3d2.jsonl",
	  "cwd": "/x",
	  "hook_event_name": "PreCompact",
	  "trigger": "manual",
	  "custom_instructions": null
	}`
	in, err := parsePreCompactInput(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if in.SessionID != "dcbed3d2-1b23-4af8-aa1e-61d5d9b7dd99" {
		t.Fatalf("session_id: %q", in.SessionID)
	}
	if in.TranscriptPath != "/x/y/dcbed3d2.jsonl" {
		t.Fatalf("transcript_path: %q", in.TranscriptPath)
	}
	if in.Trigger != "manual" {
		t.Fatalf("trigger: %q", in.Trigger)
	}
	if in.CustomInstructions != nil {
		t.Fatalf("custom_instructions should be nil, got %v", *in.CustomInstructions)
	}
}

func TestBuildSnapshotBody(t *testing.T) {
	body := buildSnapshotBody("newuuid-1234", "auth-work-precompact", "auto", nil)
	if !strings.Contains(body, "claude --resume newuuid-1234 --name auth-work-precompact") {
		t.Fatalf("body missing resume command:\n%s", body)
	}
	if !strings.Contains(body, "auto") {
		t.Fatalf("body missing trigger:\n%s", body)
	}

	ci := "focus on the DB layer"
	body = buildSnapshotBody("id2", "n-precompact-2", "manual", &ci)
	if !strings.Contains(body, ci) {
		t.Fatalf("body missing custom instructions:\n%s", body)
	}
}
