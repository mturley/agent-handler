package github

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

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
		for _, r := range resources {
			if err := watcher.EmitWatcherError(d, "github", "GitHub API error", &errBody, r); err != nil {
				logger.Printf("ERROR: failed to emit watcher error: %v", err)
			}
		}
		d.RecordWatcherError("github", errBody)
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

		count, err := processPR(d, prData, resource, token, logger)
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
func processPR(d *db.DB, prData PRData, resource watcher.Resource, token string, logger *log.Logger) (int, error) {
	eventCount := 0

	// Get cursor (last seen external timestamp)
	cursor := watcher.EventCursor(d, "github", resource.ResourceType, resource.ResourceID)

	// First poll: emit watch_started event
	if cursor == "" {
		title := fmt.Sprintf("Started watching PR: %s", prData.Title)
		body := fmt.Sprintf("PR #%d in %s/%s\nState: %s", prData.Number, prData.Owner, prData.Repo, prData.State)
		if err := watcher.EmitWatcherEvent(d, "github", watcher.EventTypeWatchStarted, title, &body, prData.UpdatedAt, nil, nil, resource); err != nil {
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
		if watcher.IsDuplicate(d, "github", resource.ResourceType, resource.ResourceID, reviewEventType(review.State), review.SubmittedAt) {
			continue
		}

		// Emit event based on review state
		if review.State == "APPROVED" {
			title := fmt.Sprintf("PR approved by %s", review.Author)
			if err := watcher.EmitWatcherEvent(d, "github", watcher.EventTypePRApproved, title, &review.Body, review.SubmittedAt, &review.Author, &review.AuthorType, resource); err != nil {
				return eventCount, fmt.Errorf("failed to emit pr_approved event: %w", err)
			}
			eventCount++
			logger.Printf("Emitted pr_approved for %s by %s", resource.ResourceID, review.Author)
		} else if review.State == "CHANGES_REQUESTED" {
			title := fmt.Sprintf("Changes requested by %s", review.Author)
			if err := watcher.EmitWatcherEvent(d, "github", watcher.EventTypePRReviewComment, title, &review.Body, review.SubmittedAt, &review.Author, &review.AuthorType, resource); err != nil {
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

		if watcher.IsDuplicate(d, "github", resource.ResourceType, resource.ResourceID, watcher.EventTypePRComment, comment.CreatedAt) {
			continue
		}

		title := fmt.Sprintf("Comment by %s", comment.Author)
		if err := watcher.EmitWatcherEvent(d, "github", watcher.EventTypePRComment, title, &comment.Body, comment.CreatedAt, &comment.Author, &comment.AuthorType, resource); err != nil {
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

		if watcher.IsDuplicate(d, "github", resource.ResourceType, resource.ResourceID, watcher.EventTypePRReviewComment, reviewComment.CreatedAt) {
			continue
		}

		title := fmt.Sprintf("Review comment by %s on %s", reviewComment.Author, reviewComment.Path)
		if err := watcher.EmitWatcherEvent(d, "github", watcher.EventTypePRReviewComment, title, &reviewComment.Body, reviewComment.CreatedAt, &reviewComment.Author, &reviewComment.AuthorType, resource); err != nil {
			return eventCount, fmt.Errorf("failed to emit pr_review_comment event: %w", err)
		}
		eventCount++
		logger.Printf("Emitted pr_review_comment for %s by %s", resource.ResourceID, reviewComment.Author)
	}

	// Fetch remaining check contexts if paginated
	if prData.CheckRunsHasMore && prData.CheckRunsEndCursor != "" {
		moreRuns, err := FetchRemainingCheckContexts(token, prData.Owner, prData.Repo, prData.Number, prData.CheckRunsEndCursor)
		if err != nil {
			logger.Printf("WARNING: failed to paginate check contexts for %s: %v", resource.ResourceID, err)
		} else {
			prData.CheckRuns = append(prData.CheckRuns, moreRuns...)
		}
	}

	// Process check runs as a bundle per commit
	if len(prData.CheckRuns) > 0 && prData.Commits.LatestSHA != "" {
		hasNewChecks := false
		for _, cr := range prData.CheckRuns {
			if cr.CompletedAt != "" && cr.CompletedAt > cursor {
				hasNewChecks = true
				break
			}
		}

		if hasNewChecks || cursor == "" {
			passed, failed, pending := 0, 0, 0
			var latestTS string
			var failedNames, pendingNames, passedNames []string

			for _, cr := range prData.CheckRuns {
				if cr.Status == "COMPLETED" {
					_, isCI := checkRunEventType(cr.Conclusion)
					if !isCI {
						continue
					}
					if cr.CompletedAt > latestTS {
						latestTS = cr.CompletedAt
					}
					switch cr.Conclusion {
					case "SUCCESS", "NEUTRAL", "SKIPPED":
						passed++
						passedNames = append(passedNames, fmt.Sprintf("✓ %s", cr.Name))
					default:
						failed++
						failedNames = append(failedNames, fmt.Sprintf("✗ %s: %s", cr.Name, cr.Conclusion))
					}
				} else if cr.Status == "IN_PROGRESS" || cr.Status == "QUEUED" {
					pending++
					pendingNames = append(pendingNames, fmt.Sprintf("⧖ %s", cr.Name))
				}
			}

			total := passed + failed + pending
			if total == 0 {
				goto skipCIBundle
			}

			shortSHA := prData.Commits.LatestSHA
			if len(shortSHA) > 7 {
				shortSHA = shortSHA[:7]
			}

			var eventType watcher.EventType
			var title string
			prLabel := fmt.Sprintf("PR #%d", prData.Number)

			if failed > 0 && pending > 0 {
				eventType = watcher.EventTypeCIPartialFailure
				title = fmt.Sprintf("CI failing for %s @ %s (%d failed, %d/%d passed)", prLabel, shortSHA, failed, passed, total)
			} else if failed > 0 {
				eventType = watcher.EventTypeCIFailed
				title = fmt.Sprintf("CI failed for %s @ %s (%d failed, %d passed)", prLabel, shortSHA, failed, passed)
			} else if pending > 0 {
				eventType = watcher.EventTypeCIPending
				title = fmt.Sprintf("CI running for %s @ %s (%d/%d passed)", prLabel, shortSHA, passed, total)
			} else {
				eventType = watcher.EventTypeCIPassed
				title = fmt.Sprintf("CI passed for %s @ %s (%d/%d checks)", prLabel, shortSHA, passed, total)
			}

			// Build body: failures first, then pending, then passes
			var bodyLines []string
			bodyLines = append(bodyLines, failedNames...)
			bodyLines = append(bodyLines, pendingNames...)
			bodyLines = append(bodyLines, passedNames...)
			body := strings.Join(bodyLines, "\n")

			if latestTS == "" {
				latestTS = time.Now().UTC().Format(time.RFC3339)
			}

			if err := watcher.UpsertCIBundle(d, prData.Commits.LatestSHA, eventType, title, body, latestTS, resource); err != nil {
				return eventCount, fmt.Errorf("failed to upsert CI bundle: %w", err)
			}
			eventCount++
			logger.Printf("Upserted CI bundle for %s (%s): %s", resource.ResourceID, prData.Commits.LatestSHA[:8], title)
		}
	}
skipCIBundle:

	// Check PR state
	if prData.State == "MERGED" || prData.State == "CLOSED" {
		eventType := watcher.EventTypePRMerged
		if prData.State == "CLOSED" {
			eventType = watcher.EventTypePRClosed
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

		// Don't auto-unsubscribe — the terminal event needs to be delivered
		// to sessions via the subscription join. Let sessions or users
		// unsubscribe explicitly, or let the subscription go idle.
	}

	// Detect new commits by comparing latest SHA against previous state
	if prData.Commits.LatestSHA != "" {
		prevState, _ := d.GetResourceState("pr", resource.ResourceID)
		if prevState != nil {
			var prev map[string]interface{}
			if json.Unmarshal([]byte(prevState.StateJSON), &prev) == nil {
				prevSHA, _ := prev["latest_commit_sha"].(string)
				if prevSHA != "" && prevSHA != prData.Commits.LatestSHA {
					if !watcher.IsDuplicate(d, "github", resource.ResourceType, resource.ResourceID, watcher.EventTypePRNewCommits, prData.Commits.LatestDate) {
						title := fmt.Sprintf("New commits pushed to PR #%d", prData.Number)
						body := formatNewCommitsBody(prData, prevSHA)
						if err := watcher.EmitWatcherEvent(d, "github", watcher.EventTypePRNewCommits, title, &body, prData.Commits.LatestDate, nil, nil, resource); err != nil {
							logger.Printf("WARNING: failed to emit new commits event: %v", err)
						} else {
							eventCount++
							logger.Printf("Emitted pr_new_commits for %s", resource.ResourceID)
						}
					}
				}
			}
		}
	}

	// Write resource state
	stateJSON := buildPRStateJSON(prData)
	now := time.Now().UTC().Format(time.RFC3339)
	if err := d.UpsertResourceState("pr", resource.ResourceID, stateJSON, prData.UpdatedAt, now); err != nil {
		logger.Printf("WARNING: failed to upsert resource state for %s: %v", resource.ResourceID, err)
	}

	return eventCount, nil
}

func reviewEventType(state string) watcher.EventType {
	if state == "APPROVED" {
		return watcher.EventTypePRApproved
	}
	return watcher.EventTypePRReviewComment
}

func checkRunEventType(conclusion string) (watcher.EventType, bool) {
	switch conclusion {
	case "SUCCESS", "NEUTRAL", "SKIPPED":
		return watcher.EventTypeCICheckPassed, true
	case "FAILURE", "TIMED_OUT", "ACTION_REQUIRED", "CANCELLED", "STALE":
		return watcher.EventTypeCICheckFailed, true
	default:
		return "", false
	}
}

// derivePRReviewDecision computes the overall review decision based on latest review per author.
func derivePRReviewDecision(reviews []Review) string {
	latestByAuthor := make(map[string]Review)
	for _, r := range reviews {
		if r.State == "DISMISSED" {
			continue
		}
		existing, ok := latestByAuthor[r.Author]
		if !ok || r.SubmittedAt > existing.SubmittedAt {
			latestByAuthor[r.Author] = r
		}
	}

	if len(latestByAuthor) == 0 {
		return "NONE"
	}

	for _, r := range latestByAuthor {
		if r.State == "CHANGES_REQUESTED" {
			return "CHANGES_REQUESTED"
		}
	}

	allApproved := true
	for _, r := range latestByAuthor {
		if r.State != "APPROVED" {
			allApproved = false
			break
		}
	}
	if allApproved {
		return "APPROVED"
	}

	return "REVIEW_REQUIRED"
}

// deriveCIStatus computes the overall CI status based on check runs.
func deriveCIStatus(checkRuns []CheckRun) string {
	if len(checkRuns) == 0 {
		return "NONE"
	}
	hasPending := false
	for _, cr := range checkRuns {
		switch cr.Conclusion {
		case "FAILURE", "TIMED_OUT", "ACTION_REQUIRED", "CANCELLED":
			return "FAILURE"
		case "":
			hasPending = true
		}
	}
	if hasPending {
		return "PENDING"
	}
	return "SUCCESS"
}

// hasNewCommitsSinceReview checks if there are commits after the latest review.
func hasNewCommitsSinceReview(prData PRData) bool {
	if prData.Commits.LatestDate == "" {
		return false
	}
	latestReviewDate := ""
	for _, r := range prData.Reviews {
		if r.SubmittedAt > latestReviewDate {
			latestReviewDate = r.SubmittedAt
		}
	}
	if latestReviewDate == "" {
		return false
	}
	return prData.Commits.LatestDate > latestReviewDate
}

// buildPRStateJSON constructs the state JSON for a PR.
func formatNewCommitsBody(prData PRData, prevSHA string) string {
	// Find commits newer than prevSHA
	var newCommits []CommitEntry
	foundPrev := false
	for _, c := range prData.Commits.Recent {
		if c.SHA == prevSHA {
			foundPrev = true
			continue
		}
		if foundPrev {
			newCommits = append(newCommits, c)
		}
	}
	// If we didn't find prevSHA in the recent list, show all recent commits
	if !foundPrev {
		newCommits = prData.Commits.Recent
	}

	if len(newCommits) == 0 {
		return fmt.Sprintf("Latest commit: %s", prData.Commits.LatestSHA[:7])
	}

	var lines []string
	for _, c := range newCommits {
		msg := c.MessageHeadline
		if len(msg) > 72 {
			msg = msg[:69] + "..."
		}
		lines = append(lines, fmt.Sprintf("• %s %s", c.SHA[:7], msg))
	}
	return fmt.Sprintf("%s", strings.Join(lines, "\n"))
}

func buildPRStateJSON(prData PRData) string {
	state := map[string]interface{}{
		"title":                        prData.Title,
		"state":                        prData.State,
		"review_decision":              derivePRReviewDecision(prData.Reviews),
		"has_new_commits_since_review": hasNewCommitsSinceReview(prData),
		"ci_status":                    deriveCIStatus(prData.CheckRuns),
		"latest_commit_sha":            prData.Commits.LatestSHA,
	}
	data, _ := json.Marshal(state)
	return string(data)
}

