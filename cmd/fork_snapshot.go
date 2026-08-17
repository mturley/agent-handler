package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
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

// preCompactInput is the JSON payload Claude Code sends on stdin for the
// PreCompact hook.
type preCompactInput struct {
	SessionID          string  `json:"session_id"`
	TranscriptPath     string  `json:"transcript_path"`
	Trigger            string  `json:"trigger"`
	CustomInstructions *string `json:"custom_instructions"`
}

func parsePreCompactInput(r io.Reader) (preCompactInput, error) {
	var in preCompactInput
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return in, fmt.Errorf("failed to parse PreCompact stdin: %w", err)
	}
	if in.SessionID == "" {
		return in, fmt.Errorf("PreCompact stdin missing session_id")
	}
	if in.TranscriptPath == "" {
		return in, fmt.Errorf("PreCompact stdin missing transcript_path")
	}
	return in, nil
}

// buildSnapshotBody renders the event body: a copy-pasteable resume command
// plus the compaction trigger and any custom instructions.
func buildSnapshotBody(newID, name, trigger string, customInstructions *string) string {
	var b strings.Builder
	b.WriteString("A pre-compaction fork of this session was saved.\n")
	b.WriteString("Resume it (rewindable to the pre-compaction state) with:\n\n")
	b.WriteString(fmt.Sprintf("  claude --resume %s --fork-session --name %s\n\n", newID, name))
	b.WriteString(fmt.Sprintf("Compaction trigger: %s\n", trigger))
	if customInstructions != nil && *customInstructions != "" {
		b.WriteString(fmt.Sprintf("Custom /compact instructions: %s\n", *customInstructions))
	}
	return b.String()
}

var forkSnapshotCmd = &cobra.Command{
	Use:    "fork-snapshot",
	Short:  "Fork the session transcript before compaction and emit a resume command",
	Hidden: true, // invoked by the PreCompact hook, not by humans
	RunE:   runForkSnapshot,
}

func init() {
	forkSnapshotCmd.GroupID = "agent"
	rootCmd.AddCommand(forkSnapshotCmd)
}

func runForkSnapshot(cmd *cobra.Command, args []string) error {
	in, err := parsePreCompactInput(os.Stdin)
	if err != nil {
		return err
	}

	newID, _, err := copyTranscript(in.TranscriptPath)
	if err != nil {
		return err
	}

	d, err := openDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	// Base name: the session's current display name, or the short session ID
	// if the session is unnamed or unregistered.
	base := in.SessionID
	if len(base) >= 8 {
		base = base[:8]
	}
	session, err := d.GetSession(in.SessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fork-snapshot: could not look up session name (%v); using id-based name\n", err)
	} else if session != nil && session.SessionName != "" {
		base = session.SessionName
	}

	name, err := d.NextForkSnapshotName(in.SessionID, base)
	if err != nil {
		return err
	}

	body := buildSnapshotBody(newID, name, in.Trigger, in.CustomInstructions)
	sessionID := in.SessionID
	evt := db.Event{
		ID:        uuid.New().String(),
		TS:        time.Now().UTC().Format(time.RFC3339),
		Source:    "agent",
		SessionID: &sessionID,
		Type:      "pre_compact_snapshot",
		Title:     "Pre-compaction snapshot saved",
		Body:      &body,
	}
	if err := d.InsertEvent(evt, nil, nil); err != nil {
		return fmt.Errorf("failed to insert snapshot event: %w", err)
	}

	fmt.Printf("✓ Pre-compaction fork saved: %s (resume as %s)\n", newID, name)
	return nil
}
