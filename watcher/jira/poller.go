package jira

import (
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

	eventCount := 0
	for _, resource := range resources {
		// Parse issue key from resource ID (it's just the key)
		issueKey := resource.ResourceID

		// Fetch issue from Jira
		logger.Printf("Fetching issue %s...", issueKey)
		issueData, err := client.FetchIssue(issueKey)
		if err != nil {
			logger.Printf("ERROR: failed to fetch issue %s: %v", issueKey, err)
			errBody := fmt.Sprintf("Failed to fetch issue: %v", err)
			if err := watcher.EmitWatcherError(d, "jira", fmt.Sprintf("Failed to fetch %s", issueKey), &errBody, resource); err != nil {
				logger.Printf("ERROR: failed to emit watcher error: %v", err)
			}
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
	}

	logger.Printf("Emitted %d events", eventCount)
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
		if err := watcher.EmitWatcherEvent(d, "jira", "watch_started", title, &body, latestTS, nil, nil, resource); err != nil {
			return eventCount, fmt.Errorf("failed to emit watch_started event: %w", err)
		}
		eventCount++
		logger.Printf("Emitted watch_started for %s", resource.ResourceID)

		// Write epic relationship if present
		if issue.EpicKey != nil && *issue.EpicKey != "" {
			if err := linkEpic(d, resource, *issue.EpicKey, logger); err != nil {
				logger.Printf("WARNING: failed to link epic for %s: %v", resource.ResourceID, err)
			}
		}

		return eventCount, nil
	}

	// Process new comments
	for _, comment := range issue.Comments {
		if comment.CreatedAt <= cursor {
			continue
		}

		if watcher.IsDuplicate(d, "jira", resource.ResourceType, resource.ResourceID, "jira_comment", comment.CreatedAt) {
			continue
		}

		title := fmt.Sprintf("Comment by %s on %s", comment.Author, issue.Key)
		authorType := authorTypeFromUsername(cfg.Services.Jira.BotUsernames, comment.Author)
		if err := watcher.EmitWatcherEvent(d, "jira", "jira_comment", title, &comment.Body, comment.CreatedAt, &comment.Author, &authorType, resource); err != nil {
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
			eventType := "jira_status_change"
			if watcher.IsDuplicate(d, "jira", resource.ResourceType, resource.ResourceID, eventType, entry.CreatedAt) {
				continue
			}
			title := fmt.Sprintf("%s: %s → %s", issue.Key, entry.From, entry.To)
			authorType := authorTypeFromUsername(cfg.Services.Jira.BotUsernames, entry.Author)
			if err := watcher.EmitWatcherEvent(d, "jira", eventType, title, nil, entry.CreatedAt, &entry.Author, &authorType, resource); err != nil {
				return eventCount, fmt.Errorf("failed to emit jira_status_change event: %w", err)
			}
			eventCount++
			logger.Printf("Emitted jira_status_change for %s: %s → %s", resource.ResourceID, entry.From, entry.To)

		case "assignee":
			eventType := "jira_assigned"
			if watcher.IsDuplicate(d, "jira", resource.ResourceType, resource.ResourceID, eventType, entry.CreatedAt) {
				continue
			}
			title := fmt.Sprintf("%s assigned to %s", issue.Key, entry.To)
			authorType := authorTypeFromUsername(cfg.Services.Jira.BotUsernames, entry.Author)
			if err := watcher.EmitWatcherEvent(d, "jira", eventType, title, nil, entry.CreatedAt, &entry.Author, &authorType, resource); err != nil {
				return eventCount, fmt.Errorf("failed to emit jira_assigned event: %w", err)
			}
			eventCount++
			logger.Printf("Emitted jira_assigned for %s: %s", resource.ResourceID, entry.To)

		case "description":
			eventType := "jira_description_changed"
			if watcher.IsDuplicate(d, "jira", resource.ResourceType, resource.ResourceID, eventType, entry.CreatedAt) {
				continue
			}
			title := fmt.Sprintf("%s description changed", issue.Key)
			authorType := authorTypeFromUsername(cfg.Services.Jira.BotUsernames, entry.Author)
			if err := watcher.EmitWatcherEvent(d, "jira", eventType, title, nil, entry.CreatedAt, &entry.Author, &authorType, resource); err != nil {
				return eventCount, fmt.Errorf("failed to emit jira_description_changed event: %w", err)
			}
			eventCount++
			logger.Printf("Emitted jira_description_changed for %s", resource.ResourceID)

		case "labels":
			eventType := "jira_labels_changed"
			if watcher.IsDuplicate(d, "jira", resource.ResourceType, resource.ResourceID, eventType, entry.CreatedAt) {
				continue
			}
			// Parse label changes
			title := labelChangeTitle(issue.Key, entry.From, entry.To)
			authorType := authorTypeFromUsername(cfg.Services.Jira.BotUsernames, entry.Author)
			if err := watcher.EmitWatcherEvent(d, "jira", eventType, title, nil, entry.CreatedAt, &entry.Author, &authorType, resource); err != nil {
				return eventCount, fmt.Errorf("failed to emit jira_labels_changed event: %w", err)
			}
			eventCount++
			logger.Printf("Emitted jira_labels_changed for %s", resource.ResourceID)

		default:
			// Skip other fields
			continue
		}
	}

	// Check if issue is in terminal state
	if isTerminalStatus(issue.Status) {
		if err := softDeleteIssueSubscriptions(d, resource, logger); err != nil {
			logger.Printf("WARNING: failed to soft-delete subscriptions for %s: %v", resource.ResourceID, err)
		}
	}

	// Update epic relationship if present
	if issue.EpicKey != nil && *issue.EpicKey != "" {
		if err := linkEpic(d, resource, *issue.EpicKey, logger); err != nil {
			logger.Printf("WARNING: failed to link epic for %s: %v", resource.ResourceID, err)
		}
	}

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

// softDeleteIssueSubscriptions soft-deletes all subscriptions for the given issue resource.
func softDeleteIssueSubscriptions(d *db.DB, resource watcher.Resource, logger *log.Logger) error {
	query := `
		SELECT DISTINCT session_id
		FROM subscriptions
		WHERE resource_type = ? AND resource_id = ? AND deleted_at IS NULL
	`

	rows, err := d.Query(query, resource.ResourceType, resource.ResourceID)
	if err != nil {
		return fmt.Errorf("failed to query subscriptions: %w", err)
	}
	defer rows.Close()

	var sessionIDs []string
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			return fmt.Errorf("failed to scan session ID: %w", err)
		}
		sessionIDs = append(sessionIDs, sessionID)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating sessions: %w", err)
	}

	for _, sessionID := range sessionIDs {
		if err := d.Unsubscribe(sessionID, resource.ResourceType, resource.ResourceID); err != nil {
			logger.Printf("WARNING: failed to unsubscribe session %s from %s: %v", sessionID, resource.ResourceID, err)
		} else {
			logger.Printf("Soft-deleted subscription for session %s to %s", sessionID, resource.ResourceID)
		}
	}

	return nil
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
