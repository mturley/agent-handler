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
			return true, "awaiting approval"
		}
		if strings.Contains(trimmed, "shift+tab to approve") {
			return true, "awaiting approval"
		}
	}

	return false, ""
}
