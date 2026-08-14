# PreCompact Fork Snapshots Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Before a session compacts, capture a resumable/rewindable fork of its transcript and emit a copy-pasteable `claude --resume <uuid> --name <unique-name>` command in the `pre_compact_snapshot` ledger event.

**Architecture:** A new Go subcommand `handler fork-snapshot` reads the PreCompact hook's stdin JSON, copies the transcript JSONL verbatim to a new UUID file in the same project dir (no `claude` invocation, zero added turns — safe even for imminent auto-compaction), computes a unique session name by counting prior `pre_compact_snapshot` events for the session, and emits the event with the resume command in the body. The installed `pre_compact.sh` hook becomes a thin pipe: it forwards stdin to `handler fork-snapshot`. All feature logic lives in Go so it is unit-testable; the hook holds no logic.

**Tech Stack:** Go, cobra (CLI), SQLite (via `db` package), bash (thin hook), Go `testing`.

**Spec:** No separate spec doc — the design was settled through conversation and recorded in memory `project_precompact-fork-snapshots`. This plan is the authoritative spec. Key verified facts:
- Pure `cp` of the transcript produces a resumable, rewindable fork (verified empirically 2026-08-14).
- Editing the copied file's `agentName` field desyncs handler registration (statusline shows stale name) — DO NOT edit the copied file. Naming is deferred to resume via `--name` in the emitted command.
- Real PreCompact stdin payload: `session_id`, `transcript_path`, `cwd`, `prompt_id`, `hook_event_name` (="PreCompact"), `trigger` ("manual"|"auto"), `custom_instructions` (string arg to `/compact`, else null).
- The method works for both `manual` and `auto` triggers, so `trigger` is metadata, not a branch condition.

## Global Constraints

- Event type string is exactly `pre_compact_snapshot` (already an established type per `docs/superpowers/specs/2026-06-15-agent-handler-design.md`; free-form, no enum validation).
- The events table has NO metadata/JSON column (`db/schema.sql`). All structured data (`trigger`, `custom_instructions`, the resume command) goes into the event `body` string.
- Session ID is read from the PreCompact stdin `session_id` field (authoritative, matches `transcript_path`) — NOT from the PPID cache.
- New forked transcript file is a byte-verbatim copy — no field edits of any kind.
- Emitted resume command form: `claude --resume <new-uuid> --name <base>-precompact` (suffix `-2`, `-3`, … when prior snapshots exist for the session).
- The hook must always `exit 0`, even on failure, so a snapshot problem never blocks the user's compaction. Failures are logged to stderr only.
- All timestamps ISO 8601 UTC; event IDs are UUIDs (existing conventions).
- Follow existing cobra command patterns in `cmd/` (one file per subcommand, `GroupID = "agent"`, register in `init()`).

---

### Task 1: `NextForkSnapshotName` — unique-name computation in the DB layer

**Files:**
- Modify: `db/events.go` (add function near `QueryEvents`, `db/events.go:127`)
- Test: `db/events_test.go` (add to existing test file)

**Interfaces:**
- Consumes: existing `(*DB).QueryEvents(EventFilter)` (`db/events.go:127`), `EventFilter{SessionID *string, Type *string}` (`db/events.go:66`).
- Produces: `func (db *DB) NextForkSnapshotName(sessionID, base string) (string, error)` — returns `base + "-precompact"` when no prior `pre_compact_snapshot` events exist for `sessionID`, else `base + "-precompact-" + strconv.Itoa(n+1)` where `n` is the count of prior such events. (First snapshot → `<base>-precompact`; second → `<base>-precompact-2`; third → `<base>-precompact-3`.)

- [ ] **Step 1: Write the failing test**

Add to `db/events_test.go`. Use the existing test-DB helper in that file (match how other tests open a `*DB` — likely `newTestDB(t)` or similar; mirror the surrounding tests exactly).

```go
func TestNextForkSnapshotName(t *testing.T) {
	d := newTestDB(t) // match the helper used by other tests in this file

	sid := "11111111-1111-1111-1111-111111111111"
	snapType := "pre_compact_snapshot"

	// No prior snapshots → base-precompact
	name, err := d.NextForkSnapshotName(sid, "auth-work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "auth-work-precompact" {
		t.Fatalf("want auth-work-precompact, got %q", name)
	}

	// Insert one prior snapshot for this session
	mustInsertEvent(t, d, sid, snapType) // helper defined below

	name, err = d.NextForkSnapshotName(sid, "auth-work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "auth-work-precompact-2" {
		t.Fatalf("want auth-work-precompact-2, got %q", name)
	}

	// A second prior snapshot → -3
	mustInsertEvent(t, d, sid, snapType)
	name, _ = d.NextForkSnapshotName(sid, "auth-work")
	if name != "auth-work-precompact-3" {
		t.Fatalf("want auth-work-precompact-3, got %q", name)
	}

	// Snapshots for a DIFFERENT session don't affect the count
	other := "22222222-2222-2222-2222-222222222222"
	name, _ = d.NextForkSnapshotName(other, "auth-work")
	if name != "auth-work-precompact" {
		t.Fatalf("want auth-work-precompact for fresh session, got %q", name)
	}
}

func mustInsertEvent(t *testing.T, d *db.DB, sessionID, typ string) {
	t.Helper()
	body := ""
	evt := db.Event{
		ID:        uuid.New().String(),
		TS:        time.Now().UTC().Format(time.RFC3339Nano),
		Source:    "agent",
		SessionID: &sessionID,
		Type:      typ,
		Title:     "snapshot",
		Body:      &body,
	}
	if err := d.InsertEvent(evt, nil, nil); err != nil {
		t.Fatalf("insert: %v", err)
	}
}
```

Note: if `events_test.go` is `package db` (internal), drop the `db.` prefixes and the import. Check the existing package clause at the top of the file and match it — adjust `db.Event`→`Event`, etc., accordingly. Ensure `uuid` and `time` imports are present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./db/ -run TestNextForkSnapshotName -v`
Expected: FAIL — `d.NextForkSnapshotName undefined`.

- [ ] **Step 3: Write minimal implementation**

Add to `db/events.go` (import `strconv` if not already imported):

```go
// NextForkSnapshotName returns a unique session name for a pre-compaction fork.
// The first snapshot for a session is "<base>-precompact"; subsequent ones get
// a numeric suffix ("<base>-precompact-2", "-3", ...) based on how many
// pre_compact_snapshot events already exist for the session.
func (db *DB) NextForkSnapshotName(sessionID, base string) (string, error) {
	snapType := "pre_compact_snapshot"
	prior, err := db.QueryEvents(EventFilter{
		SessionID: &sessionID,
		Type:      &snapType,
	})
	if err != nil {
		return "", fmt.Errorf("failed to count prior snapshots: %w", err)
	}
	if len(prior) == 0 {
		return base + "-precompact", nil
	}
	return base + "-precompact-" + strconv.Itoa(len(prior)+1), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./db/ -run TestNextForkSnapshotName -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add db/events.go db/events_test.go
git commit --signoff -m "feat(db): add NextForkSnapshotName for unique fork names"
```

---

### Task 2: `CopyTranscript` — verbatim transcript copy helper

**Files:**
- Create: `cmd/fork_snapshot.go` (this file will also hold the command in Task 3; start it here with the helper + package/imports)
- Test: `cmd/fork_snapshot_test.go`

**Interfaces:**
- Consumes: nothing from other tasks; std `os`, `io`, `path/filepath`, `github.com/google/uuid`.
- Produces:
  - `func copyTranscript(srcPath string) (newID string, newPath string, err error)` — generates a new lowercase UUID, copies `srcPath` byte-for-byte to `<dir(srcPath)>/<newID>.jsonl`, returns the new ID and full path. Errors if `srcPath` does not exist or is not readable.

- [ ] **Step 1: Write the failing test**

Create `cmd/fork_snapshot_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestCopyTranscript -v`
Expected: FAIL — `copyTranscript undefined` (and file may not compile yet).

- [ ] **Step 3: Write minimal implementation**

Create `cmd/fork_snapshot.go` with package/imports and the helper:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/ -run TestCopyTranscript -v`
Expected: PASS (both `TestCopyTranscript` and `TestCopyTranscriptMissingSource`).

- [ ] **Step 5: Commit**

```bash
git add cmd/fork_snapshot.go cmd/fork_snapshot_test.go
git commit --signoff -m "feat(cmd): add copyTranscript helper for verbatim fork copy"
```

---

### Task 3: `handler fork-snapshot` command — parse stdin, copy, name, emit

**Files:**
- Modify: `cmd/fork_snapshot.go` (add the cobra command + stdin parsing + orchestration)
- Test: `cmd/fork_snapshot_test.go` (add stdin-parse + body-format tests)

**Interfaces:**
- Consumes: `copyTranscript` (Task 2); `(*DB).NextForkSnapshotName` (Task 1); `(*DB).GetSession(sessionID string) (*Session, error)` (`db/sessions.go:82`, `Session.SessionName string` at `db/sessions.go:15`); existing `openDB()` helper used by `cmd/emit.go`; existing `db.Event` + `(*DB).InsertEvent(Event, []EventRecipient, []EventResource)` (`db/events.go:76`).
- Produces:
  - `type preCompactInput struct` with JSON tags for `session_id`, `transcript_path`, `trigger`, `custom_instructions`.
  - `func parsePreCompactInput(r io.Reader) (preCompactInput, error)`.
  - `func buildSnapshotBody(newID, name, trigger string, customInstructions *string) string` — the event body text (contains the resume command + metadata).
  - The cobra command `forkSnapshotCmd` registered under `GroupID = "agent"`.

- [ ] **Step 1: Write the failing test**

Add to `cmd/fork_snapshot_test.go`:

```go
import (
	"strings"
	// ...existing imports (os, path/filepath, testing)
)

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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run 'TestParsePreCompactInput|TestBuildSnapshotBody' -v`
Expected: FAIL — `parsePreCompactInput` / `buildSnapshotBody` undefined.

- [ ] **Step 3: Write minimal implementation**

Append to `cmd/fork_snapshot.go` (add `encoding/json`, `strings`, `time`, `github.com/spf13/cobra`, and the module's `db` import path to the import block — check `cmd/emit.go` for the exact `db` import path and reuse it):

```go
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
	b.WriteString(fmt.Sprintf("  claude --resume %s --name %s\n\n", newID, name))
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
	if session, err := d.GetSession(in.SessionID); err == nil && session != nil && session.SessionName != "" {
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
```

Note: confirm the `db` import path from `cmd/emit.go` (the top import block) and add it; confirm `openDB()` is the correct helper name (emit uses `openDB()`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -run 'TestParsePreCompactInput|TestBuildSnapshotBody|TestCopyTranscript' -v`
Expected: PASS.
Then build to confirm the command compiles and registers:
Run: `go build ./... && ./$(go env GOEXE >/dev/null; echo bin/handler) fork-snapshot --help 2>/dev/null || go run . fork-snapshot --help`
Expected: help text prints (command exists). If `go run . --help` lists commands, `fork-snapshot` may be hidden — that's expected (`Hidden: true`).

- [ ] **Step 5: Commit**

```bash
git add cmd/fork_snapshot.go cmd/fork_snapshot_test.go
git commit --signoff -m "feat(cmd): add hidden fork-snapshot command for PreCompact hook"
```

---

### Task 4: Rewrite `pre_compact.sh` to pipe stdin into `handler fork-snapshot`

**Files:**
- Modify: `hooks/pre_compact.sh`

**Interfaces:**
- Consumes: `handler fork-snapshot` (Task 3), reading PreCompact JSON from stdin.
- Produces: nothing for later tasks. The hook must `exit 0` unconditionally.

- [ ] **Step 1: Rewrite the hook**

Replace the entire contents of `hooks/pre_compact.sh` with:

```bash
#!/usr/bin/env bash
# PreCompact hook for agent-handler.
# Forks the session transcript before compaction and emits a pre_compact_snapshot
# event with a copy-pasteable resume command. All logic lives in Go
# (`handler fork-snapshot`); this hook is a thin pipe. It must never block
# compaction, so it always exits 0.
set -uo pipefail

if ! command -v handler &>/dev/null; then
    exit 0
fi

# The PreCompact hook receives a JSON payload on stdin (session_id,
# transcript_path, trigger, custom_instructions). Forward it to handler.
handler fork-snapshot >&2 || echo "agent-handler: fork-snapshot failed (compaction proceeding)" >&2

exit 0
```

Rationale for `set -uo pipefail` without `-e`: an error in `handler fork-snapshot` must not abort the script before `exit 0`; the `|| echo` swallows failure and we exit 0 regardless.

- [ ] **Step 2: Verify the hook is syntactically valid**

Run: `bash -n hooks/pre_compact.sh`
Expected: no output, exit 0.

- [ ] **Step 3: Smoke-test the hook end to end against a throwaway transcript**

Run (creates a fake transcript, pipes a fake PreCompact payload, checks a copy appears — uses a temp HOME-independent transcript dir):

```bash
TDIR=$(mktemp -d)
SRC="$TDIR/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa.jsonl"
printf '{"type":"user"}\n{"type":"assistant"}\n' > "$SRC"
echo "{\"session_id\":\"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa\",\"transcript_path\":\"$SRC\",\"trigger\":\"manual\",\"custom_instructions\":null}" \
  | go run . fork-snapshot
ls "$TDIR"/*.jsonl
```

Expected: prints a `✓ Pre-compaction fork saved: <uuid> ...` line, and `ls` shows TWO `.jsonl` files in `$TDIR` (the original + the fork). Clean up: `rm -rf "$TDIR"`.
Note: this writes a real `pre_compact_snapshot` event to the dev DB for session `aaaaaaaa-...`; harmless, but you may delete it via `handler` if desired.

- [ ] **Step 4: Commit**

```bash
git add hooks/pre_compact.sh
git commit --signoff -m "feat(hooks): fork transcript before compaction via handler fork-snapshot"
```

---

### Task 5: Document the PreCompact hook stdin payload

**Files:**
- Modify: `docs/claude-hook-stdin.md` (add a `## PreCompact` section; the file currently has `## PostCompact` at line ~297 but no PreCompact section)

**Interfaces:** none (docs only).

- [ ] **Step 1: Add the PreCompact section**

Insert a `## PreCompact` section immediately BEFORE the existing `## PostCompact` section (so pre/post read in order). Match the formatting of the surrounding sections (JSON block, then an "Additional fields" table). Content:

````markdown
## PreCompact

Received immediately before context compaction runs. The hook is synchronous —
compaction waits for it to exit before proceeding, and the on-disk transcript
still holds the full pre-compaction history at this point. (Verified 2026-08-14.)

```json
{
  "session_id": "dcbed3d2-1b23-4af8-aa1e-61d5d9b7dd99",
  "transcript_path": "/Users/mturley/.claude/projects/-Users-mturley-tmp-x/dcbed3d2-1b23-4af8-aa1e-61d5d9b7dd99.jsonl",
  "cwd": "/Users/mturley/tmp/x",
  "prompt_id": "8bd8c7cc-8a7d-4140-819d-37f97ad329aa",
  "hook_event_name": "PreCompact",
  "trigger": "manual",
  "custom_instructions": null
}
```

### Additional fields

| Field | Type | Description |
|-------|------|-------------|
| `trigger` | string | What caused compaction: `"manual"` (user ran `/compact`) or `"auto"` (context limit reached). |
| `custom_instructions` | string \| null | Text argument passed to `/compact` (e.g. `/compact focus on the DB layer`); `null` when none was given or on auto-compaction. |

Note: unlike `PostCompact`, PreCompact fires *before* the transcript is
rewritten, so `transcript_path` still points at the complete pre-compaction
history. agent-handler's PreCompact hook uses this to fork the transcript
(see `handler fork-snapshot`).
````

- [ ] **Step 2: Verify the doc renders and cross-references are correct**

Run: `grep -n '## PreCompact' docs/claude-hook-stdin.md`
Expected: one match, located before the `## PostCompact` line (compare with `grep -n '## PostCompact' docs/claude-hook-stdin.md`).

- [ ] **Step 3: Commit**

```bash
git add docs/claude-hook-stdin.md
git commit --signoff -m "docs: document PreCompact hook stdin payload"
```

---

### Task 6: Full build, test, and install verification

**Files:** none (verification only).

**Interfaces:** exercises all prior tasks together.

- [ ] **Step 1: Run the full test suite**

Run: `go test ./...`
Expected: PASS across all packages.

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 3: Reinstall handler (picks up the rewritten embedded hook)**

The hook is embedded via `//go:embed` and extracted by `handler setup` during install. Reinstall so the live `~/.agent-handler/hooks/pre_compact.sh` matches the repo.

Run: `NONINTERACTIVE=1 make install`
Expected: build + install succeed.

- [ ] **Step 4: Confirm the installed hook was updated**

Run: `grep -c 'fork-snapshot' ~/.agent-handler/hooks/pre_compact.sh`
Expected: `1` (the reinstalled hook contains the new command; not the old static-emit body).

- [ ] **Step 5: Commit (if any tidy-ups were needed)**

Only if steps produced changes (e.g. `go.mod`/`go.sum` tidy). Otherwise skip.

```bash
git add -p   # stage only intended changes; do NOT use git add -A
git commit --signoff -m "chore: tidy after fork-snapshot feature"
```

---

## Self-Review

**1. Spec coverage:**
- Pure verbatim copy, no field edits → Task 2 (`copyTranscript`, byte-identical test) + Global Constraints. ✔
- Unique name with incrementing suffix from prior events → Task 1 (`NextForkSnapshotName`). ✔
- `--name` in emitted resume command, not file edit → Task 3 (`buildSnapshotBody`). ✔
- Session ID from stdin, not PPID cache → Task 3 (`parsePreCompactInput`, `session_id`). ✔
- Works for manual + auto, trigger as metadata → Task 3 (body includes trigger; no branch). ✔
- `custom_instructions` captured → Task 3 (`buildSnapshotBody`) + Task 5 (docs). ✔
- Hook never blocks compaction (exit 0) → Task 4. ✔
- Document PreCompact stdin → Task 5. ✔
- No metadata column; data in body → Task 3 body string. ✔
- Base name fallback for unnamed/unregistered session → Task 3 (short-ID fallback). ✔

**2. Placeholder scan:** No "TBD"/"handle edge cases"/"similar to" — all steps contain real code. Two spots ask the implementer to confirm existing names by inspecting `cmd/emit.go` (the `db` import path, `openDB()`) and the test-DB helper in `db/events_test.go`; these are verification-of-existing-symbols, not placeholders, and exact expected values are given.

**3. Type consistency:**
- `NextForkSnapshotName(sessionID, base string) (string, error)` — defined Task 1, called Task 3 with `(in.SessionID, base)`. ✔
- `copyTranscript(srcPath) (newID, newPath string, err error)` — defined Task 2, called Task 3 (`newID, _, err`). ✔
- `preCompactInput` fields (`SessionID`, `TranscriptPath`, `Trigger`, `CustomInstructions`) consistent between Task 3 parse + usage. ✔
- `buildSnapshotBody(newID, name, trigger string, customInstructions *string)` — signature identical in Task 3 impl and test. ✔
- `db.Event` fields (`ID`, `TS`, `Source`, `SessionID *string`, `Type`, `Title`, `Body *string`) match `db/events.go:37-50`. ✔
- `InsertEvent(evt, nil, nil)` matches `(Event, []EventRecipient, []EventResource)` at `db/events.go:76`. ✔
- `GetSession` returns `*Session` (nil-able) with `SessionName string` — nil + empty both handled. ✔

## Notes carried from investigation

- Empty `-p ""` prompt is NOT usable for CLI forking (errors on a deferred-tool marker). Irrelevant to this plan since we use file copy, but recorded so no one "optimizes" the design back toward a prompt-based fork.
- On resuming a copied fork, Claude rewrites `sessionId` on new lines to the new UUID while historical lines keep the parent's ID; resume + rewind work fine regardless. No action needed.
