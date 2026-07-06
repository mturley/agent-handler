package jira

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/watcher"
)

// Poll polls Jira for issue updates and emits events.
func Poll(d *db.DB, cfg *config.Config, resources []watcher.Resource, logger *log.Logger) error {
	if cfg.Services.Jira == nil || cfg.Services.Jira.Token == "" {
		return fmt.Errorf("Jira token not configured")
	}

	client := &Client{
		BaseURL: cfg.Services.Jira.URL,
		Email:   cfg.Services.Jira.Email,
		Token:   cfg.Services.Jira.Token,
	}

	customFields := make(map[string]string)
	if cfg.Services.Jira != nil && cfg.Services.Jira.CustomFields != nil {
		customFields = cfg.Services.Jira.CustomFields
	}

	eventCount := 0
	for _, resource := range resources {
		// Parse issue key from resource ID (it's just the key)
		issueKey := resource.ResourceID

		// Fetch issue from Jira
		logger.Printf("Fetching issue %s...", issueKey)
		issueData, err := client.FetchIssue(issueKey, customFields)
		if err != nil {
			logger.Printf("ERROR: failed to fetch issue %s: %v", issueKey, err)
			errBody := fmt.Sprintf("Failed to fetch issue: %v", err)
			if err := watcher.EmitWatcherError(d, "jira", fmt.Sprintf("Failed to fetch %s", issueKey), &errBody, resource); err != nil {
				logger.Printf("ERROR: failed to emit watcher error: %v", err)
			}
			d.RecordWatcherError("jira", errBody)
			continue
		}

		// Process the issue
		count, err := processIssue(d, cfg, issueData, resource, logger)
		if err != nil {
			logger.Printf("ERROR: failed to process issue %s: %v", issueKey, err)
			errBody := fmt.Sprintf("Failed to process issue: %v", err)
			if err := watcher.EmitWatcherError(d, "jira", fmt.Sprintf("Error processing %s", issueKey), &errBody, resource); err != nil {
				logger.Printf("ERROR: failed to emit watcher error: %v", err)
			}
			continue
		}
		eventCount += count

		// Write resource state
		stateJSON := buildJiraStateJSON(issueData)
		now := time.Now().UTC().Format(time.RFC3339)
		if err := d.UpsertResourceState("jira", issueKey, stateJSON, issueData.UpdatedAt, now); err != nil {
			logger.Printf("WARNING: failed to upsert resource state for %s: %v", issueKey, err)
		}
	}

	logger.Printf("Emitted %d events", eventCount)
	d.RecordWatcherSuccess("jira")
	return nil
}

// processIssue processes a single Jira issue and emits events.
func processIssue(d *db.DB, cfg *config.Config, issue *IssueData, resource watcher.Resource, logger *log.Logger) (int, error) {
	eventCount := 0

	// Get cursor (last seen external timestamp)
	cursor := watcher.EventCursor(d, "jira", resource.ResourceType, resource.ResourceID)

	// First poll: emit watch_started event
	if cursor == "" {
		title := fmt.Sprintf("Started watching issue: %s", issue.Summary)
		body := fmt.Sprintf("%s\nStatus: %s", issue.Key, issue.Status)
		// Use the most recent timestamp from the issue (latest changelog or comment)
		latestTS := latestTimestamp(issue)
		if err := watcher.EmitWatcherEvent(d, "jira", watcher.EventTypeWatchStarted, title, &body, latestTS, nil, nil, resource); err != nil {
			return eventCount, fmt.Errorf("failed to emit watch_started event: %w", err)
		}
		eventCount++
		logger.Printf("Emitted watch_started for %s", resource.ResourceID)

		return eventCount, nil
	}

	// Process new comments
	for _, comment := range issue.Comments {
		if comment.CreatedAt <= cursor {
			continue
		}

		if watcher.IsDuplicate(d, "jira", resource.ResourceType, resource.ResourceID, watcher.EventTypeJiraComment, comment.CreatedAt) {
			continue
		}

		title := fmt.Sprintf("Comment by %s on %s", comment.Author, issue.Key)
		authorType := authorTypeFromUsername(cfg.Services.Jira.BotUsernames, comment.Author)
		if err := watcher.EmitWatcherEvent(d, "jira", watcher.EventTypeJiraComment, title, &comment.Body, comment.CreatedAt, &comment.Author, &authorType, resource); err != nil {
			return eventCount, fmt.Errorf("failed to emit jira_comment event: %w", err)
		}
		eventCount++
		logger.Printf("Emitted jira_comment for %s by %s", resource.ResourceID, comment.Author)
	}

	// Process changelog entries
	for _, entry := range issue.Changelog {
		if entry.CreatedAt <= cursor {
			continue
		}

		// Process based on field type
		switch entry.Field {
		case "status":
			if watcher.IsDuplicate(d, "jira", resource.ResourceType, resource.ResourceID, watcher.EventTypeJiraStatusChange, entry.CreatedAt) {
				continue
			}
			title := fmt.Sprintf("%s: %s → %s", issue.Key, entry.From, entry.To)
			authorType := authorTypeFromUsername(cfg.Services.Jira.BotUsernames, entry.Author)
			if err := watcher.EmitWatcherEvent(d, "jira", watcher.EventTypeJiraStatusChange, title, nil, entry.CreatedAt, &entry.Author, &authorType, resource); err != nil {
				return eventCount, fmt.Errorf("failed to emit jira_status_change event: %w", err)
			}
			eventCount++
			logger.Printf("Emitted jira_status_change for %s: %s → %s", resource.ResourceID, entry.From, entry.To)

		case "assignee":
			if watcher.IsDuplicate(d, "jira", resource.ResourceType, resource.ResourceID, watcher.EventTypeJiraAssigned, entry.CreatedAt) {
				continue
			}
			title := fmt.Sprintf("%s assigned to %s", issue.Key, entry.To)
			authorType := authorTypeFromUsername(cfg.Services.Jira.BotUsernames, entry.Author)
			if err := watcher.EmitWatcherEvent(d, "jira", watcher.EventTypeJiraAssigned, title, nil, entry.CreatedAt, &entry.Author, &authorType, resource); err != nil {
				return eventCount, fmt.Errorf("failed to emit jira_assigned event: %w", err)
			}
			eventCount++
			logger.Printf("Emitted jira_assigned for %s: %s", resource.ResourceID, entry.To)

		case "description":
			if watcher.IsDuplicate(d, "jira", resource.ResourceType, resource.ResourceID, watcher.EventTypeJiraDescChanged, entry.CreatedAt) {
				continue
			}
			title := fmt.Sprintf("%s description changed", issue.Key)
			authorType := authorTypeFromUsername(cfg.Services.Jira.BotUsernames, entry.Author)
			if err := watcher.EmitWatcherEvent(d, "jira", watcher.EventTypeJiraDescChanged, title, nil, entry.CreatedAt, &entry.Author, &authorType, resource); err != nil {
				return eventCount, fmt.Errorf("failed to emit jira_description_changed event: %w", err)
			}
			eventCount++
			logger.Printf("Emitted jira_description_changed for %s", resource.ResourceID)

		case "labels":
			if watcher.IsDuplicate(d, "jira", resource.ResourceType, resource.ResourceID, watcher.EventTypeJiraLabelsChanged, entry.CreatedAt) {
				continue
			}
			title := labelChangeTitle(issue.Key, entry.From, entry.To)
			authorType := authorTypeFromUsername(cfg.Services.Jira.BotUsernames, entry.Author)
			if err := watcher.EmitWatcherEvent(d, "jira", watcher.EventTypeJiraLabelsChanged, title, nil, entry.CreatedAt, &entry.Author, &authorType, resource); err != nil {
				return eventCount, fmt.Errorf("failed to emit jira_labels_changed event: %w", err)
			}
			eventCount++
			logger.Printf("Emitted jira_labels_changed for %s", resource.ResourceID)

		default:
			// Skip other fields
			continue
		}
	}

	// Don't auto-unsubscribe on terminal status — the terminal event needs
	// to be delivered to sessions via the subscription join. Let sessions
	// or users unsubscribe explicitly.

	return eventCount, nil
}

// authorTypeFromUsername determines if a username is a bot.
func authorTypeFromUsername(botUsernames []string, username string) string {
	for _, bot := range botUsernames {
		if username == bot {
			return "bot"
		}
	}
	return "human"
}

// labelChangeTitle creates a title for label changes showing +added and -removed.
func labelChangeTitle(issueKey, from, to string) string {
	fromLabels := parseLabels(from)
	toLabels := parseLabels(to)

	var added, removed []string
	fromSet := make(map[string]bool)
	toSet := make(map[string]bool)

	for _, label := range fromLabels {
		fromSet[label] = true
	}
	for _, label := range toLabels {
		toSet[label] = true
	}

	for _, label := range toLabels {
		if !fromSet[label] {
			added = append(added, label)
		}
	}
	for _, label := range fromLabels {
		if !toSet[label] {
			removed = append(removed, label)
		}
	}

	var parts []string
	if len(added) > 0 {
		parts = append(parts, "+"+strings.Join(added, " +"))
	}
	if len(removed) > 0 {
		parts = append(parts, "-"+strings.Join(removed, " -"))
	}

	if len(parts) == 0 {
		return fmt.Sprintf("%s labels changed", issueKey)
	}

	return fmt.Sprintf("%s labels: %s", issueKey, strings.Join(parts, ", "))
}

// parseLabels parses space-separated labels from a string.
func parseLabels(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

// isTerminalStatus checks if a status is terminal (issue is done/closed).
func isTerminalStatus(status string) bool {
	terminal := []string{"Done", "Resolved", "Won't Fix", "Closed", "Won't Do", "Cancelled"}
	for _, t := range terminal {
		if strings.EqualFold(status, t) {
			return true
		}
	}
	return false
}

// linkEpic creates or updates a resource relationship linking this issue to its epic.
func linkEpic(d *db.DB, resource watcher.Resource, epicKey string, logger *log.Logger) error {
	// Link issue (child) → epic (parent)
	var childURL *string
	if resource.ResourceURL != "" {
		childURL = &resource.ResourceURL
	}

	rel := db.ResourceRelationship{
		ID:           uuid.New().String(),
		ChildType:    resource.ResourceType,
		ChildID:      resource.ResourceID,
		ChildURL:     childURL,
		ParentType:   "jira",
		ParentID:     epicKey,
		ParentURL:    nil,
		Relationship: "epic",
		Source:       "jira",
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if err := d.LinkResources(rel); err != nil {
		return fmt.Errorf("failed to link epic: %w", err)
	}
	logger.Printf("Linked %s to epic %s", resource.ResourceID, epicKey)
	return nil
}

// latestTimestamp returns the most recent timestamp from an issue's comments and changelog.
func latestTimestamp(issue *IssueData) string {
	latest := ""
	for _, comment := range issue.Comments {
		if comment.CreatedAt > latest {
			latest = comment.CreatedAt
		}
	}
	for _, entry := range issue.Changelog {
		if entry.CreatedAt > latest {
			latest = entry.CreatedAt
		}
	}
	// If no comments or changelog, use a default timestamp
	if latest == "" {
		latest = "2000-01-01T00:00:00.000+0000"
	}
	return latest
}

// buildJiraStateJSON builds a JSON representation of a Jira issue's current state.
func buildJiraStateJSON(issue *IssueData) string {
	state := map[string]interface{}{
		"summary":    issue.Summary,
		"status":     issue.Status,
		"priority":   issue.Priority,
		"assignee":   issue.Assignee,
		"issue_type": issue.IssueType,
		"labels":     issue.Labels,
		"created_at": issue.CreatedAt,
		"updated_at": issue.UpdatedAt,
	}
	for k, v := range issue.CustomFields {
		state[k] = v
	}
	data, _ := json.Marshal(state)
	return string(data)
}
