package cmd

import "testing"

func TestEventDisplayName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"handler-own type", "unblocked", "unblocked"},
		{"watcher type", "pr_comment", "PR comments"},
		{"unknown type falls back to raw", "totally_unknown", "totally_unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eventDisplayName(tt.input); got != tt.want {
				t.Errorf("eventDisplayName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
