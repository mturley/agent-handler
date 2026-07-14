package terminal

import (
	"os"
	"testing"
)

func TestDetectCmux(t *testing.T) {
	os.Setenv("CMUX_SURFACE_ID", "test-surface-uuid")
	os.Setenv("CMUX_WORKSPACE_ID", "test-workspace-uuid")
	defer os.Unsetenv("CMUX_SURFACE_ID")
	defer os.Unsetenv("CMUX_WORKSPACE_ID")
	os.Unsetenv("TMUX")

	backendType, terminalID, workspaceID := Detect()
	if backendType != "cmux" {
		t.Errorf("expected backendType 'cmux', got %q", backendType)
	}
	if terminalID != "test-surface-uuid" {
		t.Errorf("expected terminalID 'test-surface-uuid', got %q", terminalID)
	}
	if workspaceID != "test-workspace-uuid" {
		t.Errorf("expected workspaceID 'test-workspace-uuid', got %q", workspaceID)
	}
}

func TestDetectTmux(t *testing.T) {
	os.Unsetenv("CMUX_SURFACE_ID")
	os.Setenv("TMUX", "/tmp/tmux-501/default,12345,0")
	defer os.Unsetenv("TMUX")

	backendType, _, _ := Detect()
	// Only check if tmux is actually available on the system
	// If tmux command fails, Detect() will return empty string
	if backendType != "" && backendType != "tmux" {
		t.Errorf("expected backendType 'tmux' or empty (if tmux unavailable), got %q", backendType)
	}
	// Note: terminalID depends on tmux being available, so we only check type here
}

func TestDetectNone(t *testing.T) {
	os.Unsetenv("CMUX_SURFACE_ID")
	os.Unsetenv("TMUX")

	backendType, terminalID, workspaceID := Detect()
	if backendType != "" {
		t.Errorf("expected empty backendType, got %q", backendType)
	}
	if terminalID != "" {
		t.Errorf("expected empty terminalID, got %q", terminalID)
	}
	if workspaceID != "" {
		t.Errorf("expected empty workspaceID, got %q", workspaceID)
	}
}

func TestDetectCmuxPriority(t *testing.T) {
	os.Setenv("CMUX_SURFACE_ID", "test-surface-uuid")
	os.Setenv("TMUX", "/tmp/tmux-501/default,12345,0")
	defer os.Unsetenv("CMUX_SURFACE_ID")
	defer os.Unsetenv("TMUX")

	backendType, _, _ := Detect()
	if backendType != "cmux" {
		t.Errorf("expected cmux to take priority, got %q", backendType)
	}
}

func TestNewBackend(t *testing.T) {
	tests := []struct {
		name        string
		backendType string
		wantErr     bool
	}{
		{"cmux", "cmux", false},
		{"tmux", "tmux", false},
		{"unknown", "unknown", true},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := NewBackend(tt.backendType)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if b == nil {
				t.Error("expected non-nil backend")
			}
		})
	}
}
