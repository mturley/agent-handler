package worktreeinterop

import "strings"

// Resource is a worktree-tracked resource as emitted by
// `worktree resources list --json`.
type Resource struct {
	Type              string `json:"type"`
	ID                string `json:"id"`
	URL               string `json:"url"`
	CustomName        string `json:"custom_name"`
	CustomDescription string `json:"custom_description"`
	UpdatedAt         string `json:"updated_at"`
}

// ParseResourceID splits a "type:id" resource identifier (e.g.
// "pr:owner/repo#42") into its parts. A missing colon yields ("", input).
func ParseResourceID(resourceID string) (resourceType, id string) {
	parts := strings.SplitN(resourceID, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", resourceID
}
