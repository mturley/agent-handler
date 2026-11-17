// Package fscache holds small helpers for on-disk JSON cache files: atomic
// writes, miss-on-any-error reads, and rate-limited pruning.
package fscache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const prunedMarker = ".pruned"

// pruneInterval is how often Prune actually scans a directory.
const pruneInterval = 24 * time.Hour

// WriteJSONAtomic marshals v and writes it to path via a temp file in the same
// directory followed by a rename, so readers never see a partial file.
func WriteJSONAtomic(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// ReadJSON reads and unmarshals path into v. Any error yields false.
func ReadJSON(path string, v any) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, v) == nil
}

// Prune removes regular files in dir not modified within maxAge. It runs at
// most once per 24h, tracked by the mtime of <dir>/.pruned, which it never
// removes.
func Prune(dir string, maxAge time.Duration, now time.Time) {
	marker := filepath.Join(dir, prunedMarker)
	if info, err := os.Stat(marker); err == nil {
		age := now.Sub(info.ModTime())
		if age >= 0 && age < pruneInterval {
			return
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Name() == prunedMarker || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > maxAge {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	if f, err := os.Create(marker); err == nil {
		f.Close()
	}
	os.Chtimes(marker, now, now)
}
