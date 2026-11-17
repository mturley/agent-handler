package discover

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAndReadPIDCache(t *testing.T) {
	tmpDir := t.TempDir()
	pid := 12345
	sessionID := "test-session-123"

	// Write the PID cache
	err := WritePIDCache(tmpDir, pid, sessionID)
	if err != nil {
		t.Fatalf("WritePIDCache failed: %v", err)
	}

	// Read it back
	readID, err := ReadPIDCache(tmpDir, pid)
	if err != nil {
		t.Fatalf("ReadPIDCache failed: %v", err)
	}

	if readID != sessionID {
		t.Errorf("Expected session ID %q, got %q", sessionID, readID)
	}
}

func TestReadPIDCacheMissing(t *testing.T) {
	tmpDir := t.TempDir()
	pid := 99999

	// Try to read a non-existent PID cache
	_, err := ReadPIDCache(tmpDir, pid)
	if err == nil {
		t.Fatal("Expected error when reading missing PID cache, got nil")
	}
}

func TestCleanStalePIDCaches(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a PID cache for the current process (should be alive)
	alivePID := os.Getpid()
	err := WritePIDCache(tmpDir, alivePID, "alive-session")
	if err != nil {
		t.Fatalf("WritePIDCache failed: %v", err)
	}

	// Create a PID cache for a likely dead process
	// Using PID 1 is tricky because it's always alive on Unix systems
	// Instead, we'll use a high number that's unlikely to exist
	deadPID := 999999
	err = WritePIDCache(tmpDir, deadPID, "dead-session")
	if err != nil {
		t.Fatalf("WritePIDCache failed: %v", err)
	}

	// Create a non-PID file (should be ignored)
	nonPIDFile := filepath.Join(tmpDir, "not-a-pid.txt")
	err = os.WriteFile(nonPIDFile, []byte("some data"), 0644)
	if err != nil {
		t.Fatalf("Failed to create non-PID file: %v", err)
	}

	// Clean stale caches
	cleaned, err := CleanStalePIDCaches(tmpDir)
	if err != nil {
		t.Fatalf("CleanStalePIDCaches failed: %v", err)
	}

	// Should have cleaned exactly 1 (the dead PID)
	if cleaned != 1 {
		t.Errorf("Expected 1 cleaned cache, got %d", cleaned)
	}

	// Verify the alive PID cache still exists
	_, err = ReadPIDCache(tmpDir, alivePID)
	if err != nil {
		t.Errorf("Alive PID cache should still exist: %v", err)
	}

	// Verify the dead PID cache was removed
	_, err = ReadPIDCache(tmpDir, deadPID)
	if err == nil {
		t.Error("Dead PID cache should have been removed")
	}

	// Verify the non-PID file still exists
	if _, err := os.Stat(nonPIDFile); os.IsNotExist(err) {
		t.Error("Non-PID file should not have been removed")
	}
}

func TestIsProcessAlive(t *testing.T) {
	// Current process should be alive
	if !IsProcessAlive(os.Getpid()) {
		t.Error("Current process should be reported as alive")
	}

	// A very high PID should not exist
	if IsProcessAlive(999999) {
		t.Error("PID 999999 should not be alive")
	}
}

func TestIsProcessAliveSelf(t *testing.T) {
	if !IsProcessAlive(os.Getpid()) {
		t.Error("current process should be reported as alive")
	}

	if IsProcessAlive(0) {
		t.Error("pid 0 should not be reported as alive")
	}

	cmd := exec.Command("sleep", "0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start child: %v", err)
	}
	childPid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	if err := cmd.Wait(); err != nil {
		t.Fatalf("failed to wait for child: %v", err)
	}

	if IsProcessAlive(childPid) {
		t.Error("exited child process should not be reported as alive")
	}
}

func TestProcessCmdlineSelf(t *testing.T) {
	cmdline, err := processCmdline(os.Getpid())
	if err != nil {
		t.Fatalf("processCmdline(self) failed: %v", err)
	}
	if !strings.Contains(cmdline, filepath.Base(os.Args[0])) {
		t.Errorf("expected cmdline %q to contain %q", cmdline, filepath.Base(os.Args[0]))
	}

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start child: %v", err)
	}
	childPid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	childCmdline, err := processCmdline(childPid)
	if err != nil {
		t.Fatalf("processCmdline(child) failed: %v", err)
	}
	if childCmdline != "sleep 30" {
		t.Errorf("expected childCmdline %q, got %q", "sleep 30", childCmdline)
	}
}

func TestIsSessionProcessMatchesCmdline(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start child: %v", err)
	}
	childPid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	if IsSessionProcess(childPid, "x") {
		t.Error("expected IsSessionProcess to be false for a non-claude process")
	}
}
