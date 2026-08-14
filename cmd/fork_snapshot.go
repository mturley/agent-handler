package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// copyTranscript copies a session transcript JSONL byte-for-byte to a new
// UUID-named file in the same directory. It performs NO edits — editing the
// copy's agentName field would desync handler registration. Returns the new
// session ID and the full path of the copy.
func copyTranscript(srcPath string) (newID string, newPath string, err error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to open transcript %s: %w", srcPath, err)
	}
	defer src.Close()

	newID = uuid.New().String()
	newPath = filepath.Join(filepath.Dir(srcPath), newID+".jsonl")

	dst, err := os.OpenFile(newPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", "", fmt.Errorf("failed to create fork file %s: %w", newPath, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", "", fmt.Errorf("failed to copy transcript: %w", err)
	}
	if err := dst.Sync(); err != nil {
		return "", "", fmt.Errorf("failed to flush fork file: %w", err)
	}
	return newID, newPath, nil
}
