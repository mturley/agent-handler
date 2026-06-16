package discover

import (
	"encoding/json"
	"io"
	"os"
)

// DiscoverSessionNameFast checks only the tail of the JSONL for a recent
// name change. Returns the name if found in the last ~8KB of the file,
// empty string otherwise. Much cheaper than DiscoverSessionName for large files.
func DiscoverSessionNameFast(jsonlPath string) string {
	f, err := os.Open(jsonlPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	// Seek to near the end of the file
	const tailSize = 8192
	stat, err := f.Stat()
	if err != nil {
		return ""
	}
	offset := stat.Size() - tailSize
	if offset < 0 {
		offset = 0
	}
	f.Seek(offset, io.SeekStart)

	data, err := io.ReadAll(f)
	if err != nil {
		return ""
	}

	// Scan lines from the tail, track the last agent-name or ai-title
	var lastAgentName, lastAITitle string
	start := 0
	// Skip partial first line if we seeked into the middle
	if offset > 0 {
		for i, b := range data {
			if b == '\n' {
				start = i + 1
				break
			}
		}
	}

	for i := start; i < len(data); i++ {
		if data[i] == '\n' || i == len(data)-1 {
			end := i
			if i == len(data)-1 && data[i] != '\n' {
				end = i + 1
			}
			line := data[start:end]
			if len(line) > 0 {
				var entry struct {
					Type      string `json:"type"`
					AgentName string `json:"agentName"`
					AITitle   string `json:"aiTitle"`
				}
				if json.Unmarshal(line, &entry) == nil {
					switch entry.Type {
					case "agent-name":
						lastAgentName = entry.AgentName
					case "ai-title":
						lastAITitle = entry.AITitle
					}
				}
			}
			start = i + 1
		}
	}

	if lastAgentName != "" {
		return lastAgentName
	}
	return lastAITitle
}
