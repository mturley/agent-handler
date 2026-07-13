package terminal

import "strings"

// NeedsInput checks terminal capture content for patterns indicating the
// session is waiting for user input. Returns whether input is needed and
// a short reason string.
func NeedsInput(content string) (bool, string) {
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.Contains(trimmed, "Esc to cancel") {
			return true, "approval prompt"
		}
	}

	// Check last few non-empty lines for mode indicators (idle at prompt)
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-5; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "mode on") && (strings.Contains(trimmed, "⏸") || strings.Contains(trimmed, "⏵")) {
			return true, "idle at prompt"
		}
	}

	return false, ""
}
