package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mturley/agent-handler/db"
)

func buildHandler(t *testing.T) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "handler")
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to build handler: %v", err)
	}
	return binPath
}

func runHandler(t *testing.T, bin string, handlerHome string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "HANDLER_HOME="+handlerHome)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestIntegrationLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	bin := buildHandler(t)
	home := t.TempDir()

	// Without setup, commands should fail
	out, err := runHandler(t, bin, home, "status")
	if err == nil {
		t.Fatal("expected status to fail before setup")
	}
	if !strings.Contains(out, "not set up yet") {
		t.Errorf("expected 'not set up yet' message, got: %s", out)
	}

	// Create the directory structure and initialize DB manually (skip hooks/skills
	// config since we don't have ~/.claude in the test environment)
	dataDir := filepath.Join(home, "data")
	os.MkdirAll(filepath.Join(dataDir, "sessions"), 0755)
	os.MkdirAll(filepath.Join(dataDir, "logs"), 0755)

	// Initialize the DB so the setup check passes
	dbPath := filepath.Join(dataDir, "handler.db")
	testDB, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to init test DB: %v", err)
	}
	testDB.Close()

	// Register a session
	out, err = runHandler(t, bin, home, "register",
		"--session-id", "test-session-1",
		"--branch", "feature-auth",
		"--repo", "mturley/myrepo",
		"--pid", "99999",
		"--jsonl-path", "/dev/null")
	if err != nil {
		t.Fatalf("register failed: %s\nerr: %v", out, err)
	}
	if !strings.Contains(out, "test-session-1") {
		t.Errorf("register output should contain session ID, got: %s", out)
	}

	// Status should show the session
	out, err = runHandler(t, bin, home, "status")
	if err != nil {
		t.Fatalf("status failed: %s\nerr: %v", out, err)
	}
	if !strings.Contains(out, "feature-auth") {
		t.Errorf("status should show branch, got: %s", out)
	}

	// Emit events
	out, err = runHandler(t, bin, home, "emit",
		"--type", "milestone",
		"--title", "Found root cause",
		"--session-id", "test-session-1")
	if err != nil {
		t.Fatalf("emit failed: %s\nerr: %v", out, err)
	}
	if !strings.Contains(out, "emitted") || !strings.Contains(out, "Event") {
		t.Errorf("emit output should confirm, got: %s", out)
	}

	out, err = runHandler(t, bin, home, "emit",
		"--type", "decision",
		"--title", "Going with approach B",
		"--body", "Approach A had too many edge cases",
		"--session-id", "test-session-1")
	if err != nil {
		t.Fatalf("emit decision failed: %s\nerr: %v", out, err)
	}

	// Subscribe to a resource
	out, err = runHandler(t, bin, home, "subscribe",
		"--resource", "pr:mturley/myrepo#42",
		"--url", "https://github.com/mturley/myrepo/pull/42",
		"--session-id", "test-session-1")
	if err != nil {
		t.Fatalf("subscribe failed: %s\nerr: %v", out, err)
	}

	// List subscriptions
	out, err = runHandler(t, bin, home, "subscriptions", "--session-id", "test-session-1")
	if err != nil {
		t.Fatalf("subscriptions failed: %s\nerr: %v", out, err)
	}
	if !strings.Contains(out, "mturley/myrepo#42") {
		t.Errorf("subscriptions should list the PR, got: %s", out)
	}

	// Unread should be empty (we emitted events from this session, not to it)
	out, err = runHandler(t, bin, home, "unread", "--session-id", "test-session-1")
	if err != nil {
		t.Fatalf("unread failed: %s\nerr: %v", out, err)
	}
	if !strings.Contains(strings.ToLower(out), "no unread") {
		t.Errorf("expected no unread events, got: %s", out)
	}

	// Emit a broadcast — should appear as unread
	out, err = runHandler(t, bin, home, "emit",
		"--type", "message",
		"--title", "System announcement",
		"--broadcast",
		"--source", "handler")
	if err != nil {
		t.Fatalf("emit broadcast failed: %s\nerr: %v", out, err)
	}

	// Now unread should show the broadcast
	out, err = runHandler(t, bin, home, "unread", "--session-id", "test-session-1")
	if err != nil {
		t.Fatalf("unread after broadcast failed: %s\nerr: %v", out, err)
	}
	if !strings.Contains(out, "System announcement") {
		t.Errorf("unread should show broadcast, got: %s", out)
	}

	// Ack
	out, err = runHandler(t, bin, home, "ack", "--session-id", "test-session-1")
	if err != nil {
		t.Fatalf("ack failed: %s\nerr: %v", out, err)
	}

	// Unread should be empty again
	out, err = runHandler(t, bin, home, "unread", "--session-id", "test-session-1")
	if err != nil {
		t.Fatalf("unread after ack failed: %s\nerr: %v", out, err)
	}
	if !strings.Contains(strings.ToLower(out), "no unread") {
		t.Errorf("expected no unread after ack, got: %s", out)
	}

	// Log should show our events
	out, err = runHandler(t, bin, home, "log", "--session-id", "test-session-1")
	if err != nil {
		t.Fatalf("log failed: %s\nerr: %v", out, err)
	}
	if !strings.Contains(out, "Found root cause") {
		t.Errorf("log should contain milestone event, got: %s", out)
	}

	// Health
	out, err = runHandler(t, bin, home, "health")
	if err != nil {
		t.Fatalf("health failed: %s\nerr: %v", out, err)
	}
	if !strings.Contains(strings.ToLower(out), "db size") || !strings.Contains(strings.ToLower(out), "database") {
		t.Errorf("health should show DB info, got: %s", out)
	}

	// Schema
	out, err = runHandler(t, bin, home, "schema")
	if err != nil {
		t.Fatalf("schema failed: %s\nerr: %v", out, err)
	}
	if !strings.Contains(out, "CREATE TABLE") {
		t.Errorf("schema should show DDL, got: %s", out)
	}

	// Query
	out, err = runHandler(t, bin, home, "query", "SELECT type, COUNT(*) as count FROM events GROUP BY type")
	if err != nil {
		t.Fatalf("query failed: %s\nerr: %v", out, err)
	}
	if !strings.Contains(out, "milestone") {
		t.Errorf("query should show milestone event type, got: %s", out)
	}

	// Query should reject writes
	out, err = runHandler(t, bin, home, "query", "DELETE FROM events")
	if err == nil {
		t.Fatal("expected query to reject write operation")
	}
	if !strings.Contains(strings.ToLower(out), "not allowed") {
		t.Errorf("expected write rejection message, got: %s", out)
	}

	// JSON output
	out, err = runHandler(t, bin, home, "--json", "status")
	if err != nil {
		t.Fatalf("status --json failed: %s\nerr: %v", out, err)
	}
	if !strings.Contains(out, "\"session_id\"") {
		t.Errorf("JSON status should contain session_id field, got: %s", out)
	}

	// Statusline
	out, err = runHandler(t, bin, home, "statusline", "--session", "test-session-1")
	if err != nil {
		t.Fatalf("statusline failed: %s\nerr: %v", out, err)
	}
	if !strings.Contains(out, "/inbox") {
		t.Errorf("statusline should contain /inbox, got: %s", out)
	}
	if !strings.Contains(out, "/inbox_mode") {
		t.Errorf("statusline should contain /inbox_mode, got: %s", out)
	}

	// Register a second session for related-session testing
	runHandler(t, bin, home, "register",
		"--session-id", "test-session-2",
		"--branch", "feature-search",
		"--repo", "mturley/myrepo",
		"--pid", "99998",
		"--jsonl-path", "/dev/null")

	// Cleanup (sessions have fake PIDs so they'll be detected as dead)
	out, err = runHandler(t, bin, home, "cleanup")
	if err != nil {
		t.Fatalf("cleanup failed: %s\nerr: %v", out, err)
	}
	if !strings.Contains(out, "Archived") {
		t.Errorf("cleanup should archive dead sessions, got: %s", out)
	}

	// Status --all should still show archived sessions
	out, err = runHandler(t, bin, home, "status", "--all")
	if err != nil {
		t.Fatalf("status --all failed: %s\nerr: %v", out, err)
	}
	if !strings.Contains(out, "feature-auth") {
		t.Errorf("status --all should show archived sessions, got: %s", out)
	}
}

func TestIntegrationUnregister(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	bin := buildHandler(t)
	home := t.TempDir()

	dataDir := filepath.Join(home, "data")
	os.MkdirAll(filepath.Join(dataDir, "sessions"), 0755)
	os.MkdirAll(filepath.Join(dataDir, "logs"), 0755)
	testDB, err := db.Open(filepath.Join(dataDir, "handler.db"))
	if err != nil {
		t.Fatalf("failed to init test DB: %v", err)
	}
	testDB.Close()

	// Register
	runHandler(t, bin, home, "register",
		"--session-id", "unreg-test",
		"--branch", "temp-branch",
		"--repo", "mturley/myrepo",
		"--pid", "99997",
		"--jsonl-path", "/dev/null")

	// Subscribe
	runHandler(t, bin, home, "subscribe",
		"--resource", "jira:RHOAIENG-100",
		"--session-id", "unreg-test")

	// Unregister
	out, err := runHandler(t, bin, home, "unregister", "--session-id", "unreg-test")
	if err != nil {
		t.Fatalf("unregister failed: %s\nerr: %v", out, err)
	}

	// Status should not show it (archived)
	out, _ = runHandler(t, bin, home, "status")
	if strings.Contains(out, "temp-branch") {
		t.Errorf("unregistered session should not appear in default status, got: %s", out)
	}

	// But --all should
	out, _ = runHandler(t, bin, home, "status", "--all")
	if !strings.Contains(out, "temp-branch") {
		t.Errorf("unregistered session should appear in status --all, got: %s", out)
	}
}
