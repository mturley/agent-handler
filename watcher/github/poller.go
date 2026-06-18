package github

import (
	"fmt"
	"log"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	"github.com/mturley/agent-handler/watcher"
)

// Poll polls GitHub for PR updates and emits events.
func Poll(d *db.DB, cfg *config.Config, resources []watcher.Resource, logger *log.Logger) error {
	if cfg.Services.GitHub == nil || cfg.Services.GitHub.Token == "" {
		return fmt.Errorf("GitHub token not configured")
	}

	token := cfg.Services.GitHub.Token

	// Parse all resource IDs into PRRefs
	var prRefs []PRRef
	resourceMap := make(map[string]watcher.Resource) // key: "owner/repo#number"
	for _, r := range resources {
		ref, err := ParsePRResourceID(r.ResourceID)
		if err != nil {
			logger.Printf("ERROR: failed to parse resource ID %q: %v", r.ResourceID, err)
			// Emit error event for this resource
			errBody := fmt.Sprintf("Failed to parse resource ID: %v", err)
			if err := watcher.EmitWatcherError(d, "github", "Invalid PR resource ID", &errBody, r); err != nil {
				logger.Printf("ERROR: failed to emit watcher error: %v", err)
			}
			continue
		}
		prRefs = append(prRefs, ref)
		resourceMap[r.ResourceID] = r
	}

	if len(prRefs) == 0 {
		logger.Printf("No valid PRs to poll")
		return nil
	}

	// Fetch PR data
	logger.Printf("Fetching data for %d PRs...", len(prRefs))
	prDataList, rateLimit, err := FetchPRs(token, prRefs)
	if err != nil {
		logger.Printf("ERROR: failed to fetch PRs: %v", err)
		// Emit error events for all resources
		errBody := fmt.Sprintf("Failed to fetch PR data: %v", err)
		d.RecordWatcherError("github", errBody)
		for _, r := range resources {
			if err := watcher.EmitWatcherError(d, "github", "GitHub API error", &errBody, r); err != nil {
				logger.Printf("ERROR: failed to emit watcher error: %v", err)
			}
		}
		return err
	}

	logger.Printf("Rate limit: %d/%d remaining", rateLimit.Remaining, rateLimit.Limit)

	// Process each PR
	eventCount := 0
	for _, prData := range prDataList {
		resourceID := fmt.Sprintf("%s/%s#%d", prData.Owner, prData.Repo, prData.Number)
		resource, ok := resourceMap[resourceID]
		if !ok {
			logger.Printf("WARNING: received data for unknown resource %q", resourceID)
			continue
		}

		count, err := processPR(d, prData, resource, logger)
		if err != nil {
			logger.Printf("ERROR: failed to process PR %s: %v", resourceID, err)
			// Emit error event
			errBody := fmt.Sprintf("Failed to process PR: %v", err)
			if err := watcher.EmitWatcherError(d, "github", "PR processing error", &errBody, resource); err != nil {
				logger.Printf("ERROR: failed to emit watcher error: %v", err)
			}
			continue
		}
		eventCount += count
	}

	logger.Printf("Emitted %d events", eventCount)
	d.RecordWatcherSuccess("github")
	return nil
}

// processPR processes a single PR and emits events.
// Returns the count of events emitted.
func processPR(d *db.DB, prData PRData, resource watcher.Resource, logger *log.Logger) (int, error) {
	eventCount := 0

	// Get cursor (last seen external timestamp)
	cursor := watcher.EventCursor(d, "github", resource.ResourceType, resource.ResourceID)

	// First poll: emit watch_started event
	if cursor == "" {
		title := fmt.Sprintf("Started watching PR: %s", prData.Title)
		body := fmt.Sprintf("PR #%d in %s/%s\nState: %s", prData.Number, prData.Owner, prData.Repo, prData.State)
		if err := watcher.EmitWatcherEvent(d, "github", "watch_started", title, &body, prData.UpdatedAt, nil, nil, resource); err != nil {
			return eventCount, fmt.Errorf("failed to emit watch_started event: %w", err)
		}
		eventCount++
		logger.Printf("Emitted watch_started for %s", resource.ResourceID)
		return eventCount, nil
	}

	// Process reviews
	for _, review := range prData.Reviews {
		if review.SubmittedAt <= cursor {
			continue
		}

		// Skip duplicate events
		if watcher.IsDuplicate(d, "github", resource.ResourceType, resource.ResourceID, eventTypeForReview(review.State), review.SubmittedAt) {
			continue
		}

		// Emit event based on review state
		if review.State == "APPROVED" {
			title := fmt.Sprintf("PR approved by %s", review.Author)
			if err := watcher.EmitWatcherEvent(d, "github", "pr_approved", title, &review.Body, review.SubmittedAt, &review.Author, &review.AuthorType, resource); err != nil {
				return eventCount, fmt.Errorf("failed to emit pr_approved event: %w", err)
			}
			eventCount++
			logger.Printf("Emitted pr_approved for %s by %s", resource.ResourceID, review.Author)
		} else if review.State == "CHANGES_REQUESTED" {
			title := fmt.Sprintf("Changes requested by %s", review.Author)
			if err := watcher.EmitWatcherEvent(d, "github", "pr_review_comment", title, &review.Body, review.SubmittedAt, &review.Author, &review.AuthorType, resource); err != nil {
				return eventCount, fmt.Errorf("failed to emit pr_review_comment event: %w", err)
			}
			eventCount++
			logger.Printf("Emitted pr_review_comment for %s by %s", resource.ResourceID, review.Author)
		}
	}

	// Process comments
	for _, comment := range prData.Comments {
		if comment.CreatedAt <= cursor {
			continue
		}

		if watcher.IsDuplicate(d, "github", resource.ResourceType, resource.ResourceID, "pr_comment", comment.CreatedAt) {
			continue
		}

		title := fmt.Sprintf("Comment by %s", comment.Author)
		if err := watcher.EmitWatcherEvent(d, "github", "pr_comment", title, &comment.Body, comment.CreatedAt, &comment.Author, &comment.AuthorType, resource); err != nil {
			return eventCount, fmt.Errorf("failed to emit pr_comment event: %w", err)
		}
		eventCount++
		logger.Printf("Emitted pr_comment for %s by %s", resource.ResourceID, comment.Author)
	}

	// Process review comments (inline code comments)
	for _, reviewComment := range prData.ReviewComments {
		if reviewComment.CreatedAt <= cursor {
			continue
		}

		if watcher.IsDuplicate(d, "github", resource.ResourceType, resource.ResourceID, "pr_review_comment", reviewComment.CreatedAt) {
			continue
		}

		title := fmt.Sprintf("Review comment by %s on %s", reviewComment.Author, reviewComment.Path)
		if err := watcher.EmitWatcherEvent(d, "github", "pr_review_comment", title, &reviewComment.Body, reviewComment.CreatedAt, &reviewComment.Author, &reviewComment.AuthorType, resource); err != nil {
			return eventCount, fmt.Errorf("failed to emit pr_review_comment event: %w", err)
		}
		eventCount++
		logger.Printf("Emitted pr_review_comment for %s by %s", resource.ResourceID, reviewComment.Author)
	}

	// Process check runs
	for _, checkRun := range prData.CheckRuns {
		if checkRun.CompletedAt <= cursor {
			continue
		}

		eventType := checkEventType(checkRun.Conclusion)
		if eventType == "" {
			// Skip in-progress or unknown conclusions
			continue
		}

		if watcher.IsDuplicate(d, "github", resource.ResourceType, resource.ResourceID, eventType, checkRun.CompletedAt) {
			continue
		}

		title := fmt.Sprintf("Check %s: %s", checkRun.Name, checkRun.Conclusion)
		if err := watcher.EmitWatcherEvent(d, "github", eventType, title, nil, checkRun.CompletedAt, nil, nil, resource); err != nil {
			return eventCount, fmt.Errorf("failed to emit %s event: %w", eventType, err)
		}
		eventCount++
		logger.Printf("Emitted %s for %s: %s", eventType, resource.ResourceID, checkRun.Name)
	}

	// Check PR state
	if prData.State == "MERGED" || prData.State == "CLOSED" {
		eventType := "pr_merged"
		if prData.State == "CLOSED" {
			eventType = "pr_closed"
		}

		if !watcher.IsDuplicate(d, "github", resource.ResourceType, resource.ResourceID, eventType, prData.UpdatedAt) {
			title := fmt.Sprintf("PR %s", prData.State)
			body := fmt.Sprintf("PR #%d: %s", prData.Number, prData.Title)
			if err := watcher.EmitWatcherEvent(d, "github", eventType, title, &body, prData.UpdatedAt, nil, nil, resource); err != nil {
				return eventCount, fmt.Errorf("failed to emit %s event: %w", eventType, err)
			}
			eventCount++
			logger.Printf("Emitted %s for %s", eventType, resource.ResourceID)
		}

		// Soft-delete subscriptions for this PR
		if err := softDeletePRSubscriptions(d, resource, logger); err != nil {
			logger.Printf("WARNING: failed to soft-delete subscriptions for %s: %v", resource.ResourceID, err)
		}
	}

	return eventCount, nil
}

// eventTypeForReview returns the event type for a review based on its state.
func eventTypeForReview(state string) string {
	if state == "APPROVED" {
		return "pr_approved"
	}
	return "pr_review_comment"
}

// checkEventType returns the event type for a check run based on its conclusion.
func checkEventType(conclusion string) string {
	switch conclusion {
	case "SUCCESS", "NEUTRAL", "SKIPPED":
		return "ci_check_passed"
	case "FAILURE", "TIMED_OUT", "ACTION_REQUIRED", "CANCELLED", "STALE":
		return "ci_check_failed"
	default:
		return "" // Unknown or in-progress
	}
}

// softDeletePRSubscriptions soft-deletes all subscriptions for the given PR resource.
func softDeletePRSubscriptions(d *db.DB, resource watcher.Resource, logger *log.Logger) error {
	// Query all sessions with active subscriptions to this resource
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

	// Soft-delete subscription for each session
	for _, sessionID := range sessionIDs {
		if err := d.Unsubscribe(sessionID, resource.ResourceType, resource.ResourceID); err != nil {
			logger.Printf("WARNING: failed to unsubscribe session %s from %s: %v", sessionID, resource.ResourceID, err)
		} else {
			logger.Printf("Soft-deleted subscription for session %s to %s", sessionID, resource.ResourceID)
		}
	}

	return nil
}
