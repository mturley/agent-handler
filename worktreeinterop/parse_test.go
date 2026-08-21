package worktreeinterop

import "testing"

func TestParseResourceID(t *testing.T) {
	tests := []struct {
		input        string
		expectedType string
		expectedID   string
	}{
		{"pr:123", "pr", "123"},
		{"issue:456", "issue", "456"},
		{"jira:PROJ-789", "jira", "PROJ-789"},
		{"no-colon", "", "no-colon"},
		{"multiple:colons:here", "multiple", "colons:here"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			resType, resID := ParseResourceID(tt.input)
			if resType != tt.expectedType {
				t.Errorf("Expected type %q, got %q", tt.expectedType, resType)
			}
			if resID != tt.expectedID {
				t.Errorf("Expected ID %q, got %q", tt.expectedID, resID)
			}
		})
	}
}
