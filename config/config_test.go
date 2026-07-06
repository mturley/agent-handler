package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadWriteConfig(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testPath := filepath.Join(tmpDir, "config.yaml")

	// Create a config with both services
	cfg := &Config{
		Services: Services{
			GitHub: &GitHubConfig{
				Token: "ghp_test123",
			},
			Jira: &JiraConfig{
				URL:          "https://jira.example.com",
				Email:        "test@example.com",
				Token:        "jira_token_abc",
				BotUsernames: []string{"bot1", "bot2"},
			},
		},
	}

	// Write the config
	if err := Write(testPath, cfg); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Verify file permissions are 0600
	info, err := os.Stat(testPath)
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("Expected file permissions 0600, got %v", mode)
	}

	// Read the config back
	readCfg, err := Read(testPath)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	// Verify all fields
	if readCfg.Services.GitHub == nil {
		t.Fatal("GitHub config is nil")
	}
	if readCfg.Services.GitHub.Token != "ghp_test123" {
		t.Errorf("Expected GitHub token 'ghp_test123', got '%s'", readCfg.Services.GitHub.Token)
	}

	if readCfg.Services.Jira == nil {
		t.Fatal("Jira config is nil")
	}
	if readCfg.Services.Jira.URL != "https://jira.example.com" {
		t.Errorf("Expected Jira URL 'https://jira.example.com', got '%s'", readCfg.Services.Jira.URL)
	}
	if readCfg.Services.Jira.Email != "test@example.com" {
		t.Errorf("Expected Jira email 'test@example.com', got '%s'", readCfg.Services.Jira.Email)
	}
	if readCfg.Services.Jira.Token != "jira_token_abc" {
		t.Errorf("Expected Jira token 'jira_token_abc', got '%s'", readCfg.Services.Jira.Token)
	}
	if len(readCfg.Services.Jira.BotUsernames) != 2 {
		t.Fatalf("Expected 2 bot usernames, got %d", len(readCfg.Services.Jira.BotUsernames))
	}
	if readCfg.Services.Jira.BotUsernames[0] != "bot1" || readCfg.Services.Jira.BotUsernames[1] != "bot2" {
		t.Errorf("Expected bot usernames [bot1, bot2], got %v", readCfg.Services.Jira.BotUsernames)
	}
}

func TestReadMissingFile(t *testing.T) {
	// Try to read a file that doesn't exist
	cfg, err := Read("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("Expected no error for missing file, got: %v", err)
	}

	// Should return empty config
	if cfg == nil {
		t.Fatal("Expected non-nil config")
	}
	if cfg.Services.GitHub != nil {
		t.Error("Expected nil GitHub config for missing file")
	}
	if cfg.Services.Jira != nil {
		t.Error("Expected nil Jira config for missing file")
	}
}

func TestDefaultResourceURL(t *testing.T) {
	cfg := &Config{
		Services: Services{
			Jira: &JiraConfig{URL: "https://jira.example.com"},
		},
	}

	tests := []struct {
		name         string
		resourceType string
		resourceID   string
		expected     string
	}{
		{"PR", "pr", "owner/repo#123", "https://github.com/owner/repo/pull/123"},
		{"PR with org", "pr", "my-org/my-repo#42", "https://github.com/my-org/my-repo/pull/42"},
		{"PR missing hash", "pr", "owner/repo", ""},
		{"PR empty number", "pr", "owner/repo#", ""},
		{"Jira", "jira", "PROJ-456", "https://jira.example.com/browse/PROJ-456"},
		{"Jira trailing slash", "jira", "PROJ-1", "https://jira.example.com/browse/PROJ-1"},
		{"Unknown type", "slack", "chan-123", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cfg.DefaultResourceURL(tt.resourceType, tt.resourceID)
			if result != tt.expected {
				t.Errorf("DefaultResourceURL(%s, %s) = %q, want %q", tt.resourceType, tt.resourceID, result, tt.expected)
			}
		})
	}

	// Test with trailing slash on Jira URL
	cfgSlash := &Config{
		Services: Services{
			Jira: &JiraConfig{URL: "https://jira.example.com/"},
		},
	}
	result := cfgSlash.DefaultResourceURL("jira", "PROJ-1")
	if result != "https://jira.example.com/browse/PROJ-1" {
		t.Errorf("Trailing slash: got %q, want %q", result, "https://jira.example.com/browse/PROJ-1")
	}

	// Test with no Jira config
	cfgNoJira := &Config{}
	result = cfgNoJira.DefaultResourceURL("jira", "PROJ-1")
	if result != "" {
		t.Errorf("No Jira config: got %q, want empty", result)
	}
}

func TestIsServiceConfigured(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		service  string
		expected bool
	}{
		{
			name: "GitHub configured",
			config: &Config{
				Services: Services{
					GitHub: &GitHubConfig{Token: "test"},
				},
			},
			service:  "github",
			expected: true,
		},
		{
			name: "GitHub not configured - nil",
			config: &Config{
				Services: Services{},
			},
			service:  "github",
			expected: false,
		},
		{
			name: "GitHub not configured - empty token",
			config: &Config{
				Services: Services{
					GitHub: &GitHubConfig{Token: ""},
				},
			},
			service:  "github",
			expected: false,
		},
		{
			name: "Jira configured",
			config: &Config{
				Services: Services{
					Jira: &JiraConfig{Token: "test"},
				},
			},
			service:  "jira",
			expected: true,
		},
		{
			name: "Jira not configured - nil",
			config: &Config{
				Services: Services{},
			},
			service:  "jira",
			expected: false,
		},
		{
			name: "Jira not configured - empty token",
			config: &Config{
				Services: Services{
					Jira: &JiraConfig{Token: ""},
				},
			},
			service:  "jira",
			expected: false,
		},
		{
			name:     "Unknown service",
			config:   &Config{},
			service:  "unknown",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.IsServiceConfigured(tt.service)
			if result != tt.expected {
				t.Errorf("Expected IsServiceConfigured(%s) to be %v, got %v", tt.service, tt.expected, result)
			}
		})
	}
}
