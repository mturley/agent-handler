package fscache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type entry struct {
	A string
	B int
}

func TestWriteJSONAtomicRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "e.json")
	if err := WriteJSONAtomic(path, entry{"x", 3}); err != nil {
		t.Fatal(err)
	}
	var got entry
	if !ReadJSON(path, &got) || got != (entry{"x", 3}) {
		t.Fatalf("got %+v", got)
	}
	ents, _ := os.ReadDir(filepath.Dir(path))
	if len(ents) != 1 {
		t.Errorf("leftover temp files: %v", ents)
	}
}

func TestReadJSONCorruptIsFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "e.json")
	os.WriteFile(path, []byte("{"), 0o644)
	var got entry
	if ReadJSON(path, &got) {
		t.Error("corrupt file read as ok")
	}
	if ReadJSON(filepath.Join(dir, "missing.json"), &got) {
		t.Error("missing file read as ok")
	}
}

func TestPruneRemovesOldKeepsNew(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	old := filepath.Join(dir, "old.json")
	fresh := filepath.Join(dir, "new.json")
	os.WriteFile(old, []byte("{}"), 0o644)
	os.WriteFile(fresh, []byte("{}"), 0o644)
	os.Chtimes(old, now.Add(-8*24*time.Hour), now.Add(-8*24*time.Hour))
	Prune(dir, 7*24*time.Hour, now)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("old file not removed")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("new file removed")
	}
	if _, err := os.Stat(filepath.Join(dir, ".pruned")); err != nil {
		t.Error(".pruned not created")
	}
}

func TestPruneRateLimited(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	Prune(dir, 7*24*time.Hour, now)
	old := filepath.Join(dir, "old.json")
	os.WriteFile(old, []byte("{}"), 0o644)
	os.Chtimes(old, now.Add(-8*24*time.Hour), now.Add(-8*24*time.Hour))
	Prune(dir, 7*24*time.Hour, now.Add(time.Hour))
	if _, err := os.Stat(old); err != nil {
		t.Error("prune ran again within 24h")
	}
	// .pruned is itself old by now+25h, but must never be removed.
	Prune(dir, time.Hour, now.Add(25*time.Hour))
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("prune did not run after 24h")
	}
	if _, err := os.Stat(filepath.Join(dir, ".pruned")); err != nil {
		t.Error(".pruned removed")
	}
}
