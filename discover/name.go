package discover

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// DiscoverSessionName scans the JSONL file line by line, looks for
// "type":"agent-name" entries (extracting agentName field) and
// "type":"ai-title" entries (extracting aiTitle field).
// Returns the last agentName found, falling back to the last aiTitle.
// Returns empty string if neither found.
func DiscoverSessionName(jsonlPath string) (string, error) {
	file, err := os.Open(jsonlPath)
	if err != nil {
		return "", fmt.Errorf("failed to open JSONL file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Set buffer size to 1MB to handle large lines
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 1024*1024)

	var lastAgentName string
	var lastAITitle string

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Parse the line as JSON to check its type
		var entry map[string]interface{}
		if err := json.Unmarshal(line, &entry); err != nil {
			// Skip malformed lines
			continue
		}

		entryType, ok := entry["type"].(string)
		if !ok {
			continue
		}

		switch entryType {
		case "agent-name":
			if agentName, ok := entry["agentName"].(string); ok {
				lastAgentName = agentName
			}
		case "ai-title":
			if aiTitle, ok := entry["aiTitle"].(string); ok {
				lastAITitle = aiTitle
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading JSONL file: %w", err)
	}

	// Return agentName if found, otherwise fall back to aiTitle
	if lastAgentName != "" {
		return lastAgentName, nil
	}
	return lastAITitle, nil
}
