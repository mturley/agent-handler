# Resource State Caching and Handler Briefing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cache external resource state (PR status, Jira priority) from watcher polls, surface it in triage output, and rewrite the /handler skill to deliver a prioritized briefing with peek results and timeline.

**Architecture:** Watchers already fetch resource data on each poll — this adds a `resource_state` table to cache current state, configurable Jira custom fields in `config.yaml`, enriched triage output, and a handler skill rewrite that combines triage + peek + timeline into a prioritized briefing.

**Tech Stack:** Go, SQLite, GitHub GraphQL API, Jira REST API v3, YAML config

## Global Constraints

- Go binary — pure-Go SQLite (`modernc.org/sqlite`)
- All timestamps ISO 8601 UTC
- `--json` flag on all CLI commands
- Tests must pass: `go test ./...`
- Use `--signoff` on all commits
- Follow existing cobra command patterns in `cmd/`
- Follow existing test patterns using `testutil.testDB(t)` and `db.seedSession()`

---

### Task 1: Resource State DB Layer

**Files:**
- Create: `db/resource_state.go`
- Create: `db/resource_state_test.go`
- Modify: `db/schema.sql`
- Modify: `db/db.go`

**Interfaces:**
- Consumes: `db.Open()`, `db.DB` (existing)
- Produces:
  - `db.ResourceState` struct: `ResourceType string`, `ResourceID string`, `StateJSON string`, `ResourceUpdatedAt string`, `WatcherUpdatedAt string`
  - `db.UpsertResourceState(resourceType, resourceID, stateJSON, resourceUpdatedAt, watcherUpdatedAt string) error`
  - `db.GetResourceState(resourceType, resourceID string) (*ResourceState, error)`
  - `db.DeleteResourceState(resourceType, resourceID string) error`
  - `db.ListResourceStatesForSession(sessionID string) ([]ResourceStateWithSubscription, error)`

- [ ] **Step 1: Write tests for ResourceState CRUD**

Create `db/resource_state_test.go`:

```go
package db

import "testing"

func TestUpsertAndGetResourceState(t *testing.T) {
	d := testDB(t)

	err := d.UpsertResourceState("pr", "owner/repo#1", `{"state":"open"}`, "2026-07-06T10:00:00Z", "2026-07-06T10:01:00Z")
	if err != nil {
		t.Fatalf("UpsertResourceState failed: %v", err)
	}

	rs, err := d.GetResourceState("pr", "owner/repo#1")
	if err != nil {
		t.Fatalf("GetResourceState failed: %v", err)
	}
	if rs == nil {
		t.Fatal("expected non-nil ResourceState")
	}
	if rs.StateJSON != `{"state":"open"}` {
		t.Errorf("expected state JSON %q, got %q", `{"state":"open"}`, rs.StateJSON)
	}
	if rs.ResourceUpdatedAt != "2026-07-06T10:00:00Z" {
		t.Errorf("expected resource_updated_at %q, got %q", "2026-07-06T10:00:00Z", rs.ResourceUpdatedAt)
	}
	if rs.WatcherUpdatedAt != "2026-07-06T10:01:00Z" {
		t.Errorf("expected watcher_updated_at %q, got %q", "2026-07-06T10:01:00Z", rs.WatcherUpdatedAt)
	}
}

func TestUpsertResourceStateOverwrites(t *testing.T) {
	d := testDB(t)

	d.UpsertResourceState("pr", "owner/repo#1", `{"state":"open"}`, "2026-07-06T10:00:00Z", "2026-07-06T10:01:00Z")
	d.UpsertResourceState("pr", "owner/repo#1", `{"state":"merged"}`, "2026-07-06T11:00:00Z", "2026-07-06T11:01:00Z")

	rs, _ := d.GetResourceState("pr", "owner/repo#1")
	if rs.StateJSON != `{"state":"merged"}` {
		t.Errorf("expected updated state, got %q", rs.StateJSON)
	}
}

func TestGetResourceStateNotFound(t *testing.T) {
	d := testDB(t)

	rs, err := d.GetResourceState("pr", "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rs != nil {
		t.Error("expected nil for nonexistent resource")
	}
}

func TestDeleteResourceState(t *testing.T) {
	d := testDB(t)

	d.UpsertResourceState("jira", "PROJ-1", `{"status":"open"}`, "2026-07-06T10:00:00Z", "2026-07-06T10:01:00Z")
	err := d.DeleteResourceState("jira", "PROJ-1")
	if err != nil {
		t.Fatalf("DeleteResourceState failed: %v", err)
	}

	rs, _ := d.GetResourceState("jira", "PROJ-1")
	if rs != nil {
		t.Error("expected nil after delete")
	}
}

func TestListResourceStatesForSession(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "rs-session-test")

	now := "2026-07-06T10:00:00Z"
	d.Subscribe(Subscription{
		ID: "sub-1", SessionID: "rs-session-test",
		ResourceType: "pr", ResourceID: "owner/repo#1",
		CreatedAt: now,
	})
	d.Subscribe(Subscription{
		ID: "sub-2", SessionID: "rs-session-test",
		ResourceType: "jira", ResourceID: "PROJ-100",
		CreatedAt: now,
	})

	d.UpsertResourceState("pr", "owner/repo#1", `{"state":"open"}`, now, now)
	d.UpsertResourceState("jira", "PROJ-100", `{"status":"In Progress"}`, now, now)

	results, err := d.ListResourceStatesForSession("rs-session-test")
	if err != nil {
		t.Fatalf("ListResourceStatesForSession failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./db/ -run TestUpsertAndGetResourceState -v`
Expected: compilation error — types don't exist yet.

- [ ] **Step 3: Add resource_state table to schema.sql**

Append to `db/schema.sql`:

```sql
CREATE TABLE IF NOT EXISTS resource_state (
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    state_json TEXT NOT NULL,
    resource_updated_at TEXT NOT NULL,
    watcher_updated_at TEXT NOT NULL,
    PRIMARY KEY (resource_type, resource_id)
);
```

- [ ] **Step 4: Implement resource_state.go**

Create `db/resource_state.go`:

```go
package db

import (
	"database/sql"
	"fmt"
)

// ResourceState represents cached state of an external resource.
type ResourceState struct {
	ResourceType      string `json:"resource_type"`
	ResourceID        string `json:"resource_id"`
	StateJSON         string `json:"state_json"`
	ResourceUpdatedAt string `json:"resource_updated_at"`
	WatcherUpdatedAt  string `json:"watcher_updated_at"`
}

// ResourceStateWithSubscription pairs a resource state with subscription metadata.
type ResourceStateWithSubscription struct {
	ResourceType      string  `json:"resource_type"`
	ResourceID        string  `json:"resource_id"`
	ResourceURL       *string `json:"resource_url,omitempty"`
	StateJSON         string  `json:"state_json"`
	ResourceUpdatedAt string  `json:"resource_updated_at"`
	WatcherUpdatedAt  string  `json:"watcher_updated_at"`
}

func (db *DB) UpsertResourceState(resourceType, resourceID, stateJSON, resourceUpdatedAt, watcherUpdatedAt string) error {
	_, err := db.conn.Exec(`
		INSERT INTO resource_state (resource_type, resource_id, state_json, resource_updated_at, watcher_updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(resource_type, resource_id) DO UPDATE SET
			state_json = excluded.state_json,
			resource_updated_at = excluded.resource_updated_at,
			watcher_updated_at = excluded.watcher_updated_at
	`, resourceType, resourceID, stateJSON, resourceUpdatedAt, watcherUpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to upsert resource state: %w", err)
	}
	return nil
}

func (db *DB) GetResourceState(resourceType, resourceID string) (*ResourceState, error) {
	var rs ResourceState
	err := db.conn.QueryRow(`
		SELECT resource_type, resource_id, state_json, resource_updated_at, watcher_updated_at
		FROM resource_state
		WHERE resource_type = ? AND resource_id = ?
	`, resourceType, resourceID).Scan(&rs.ResourceType, &rs.ResourceID, &rs.StateJSON, &rs.ResourceUpdatedAt, &rs.WatcherUpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get resource state: %w", err)
	}
	return &rs, nil
}

func (db *DB) DeleteResourceState(resourceType, resourceID string) error {
	_, err := db.conn.Exec(`DELETE FROM resource_state WHERE resource_type = ? AND resource_id = ?`,
		resourceType, resourceID)
	if err != nil {
		return fmt.Errorf("failed to delete resource state: %w", err)
	}
	return nil
}

// ListResourceStatesForSession returns resource states for all active subscriptions of a session.
func (db *DB) ListResourceStatesForSession(sessionID string) ([]ResourceStateWithSubscription, error) {
	rows, err := db.conn.Query(`
		SELECT s.resource_type, s.resource_id, s.resource_url,
		       COALESCE(rs.state_json, '{}'), COALESCE(rs.resource_updated_at, ''), COALESCE(rs.watcher_updated_at, '')
		FROM subscriptions s
		LEFT JOIN resource_state rs ON rs.resource_type = s.resource_type AND rs.resource_id = s.resource_id
		WHERE s.session_id = ? AND s.deleted_at IS NULL
		ORDER BY s.created_at
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list resource states: %w", err)
	}
	defer rows.Close()

	var results []ResourceStateWithSubscription
	for rows.Next() {
		var r ResourceStateWithSubscription
		if err := rows.Scan(&r.ResourceType, &r.ResourceID, &r.ResourceURL,
			&r.StateJSON, &r.ResourceUpdatedAt, &r.WatcherUpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan resource state: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./db/ -v`
Expected: all tests pass.

- [ ] **Step 6: Run full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 7: Commit**

```bash
git add db/resource_state.go db/resource_state_test.go db/schema.sql
git commit --signoff -m "feat: resource_state table for caching external resource state"
```

---

### Task 2: Configurable Jira Custom Fields

**Files:**
- Modify: `config/config.go`
- Modify: `config/config_test.go`

**Interfaces:**
- Consumes: existing `Config` struct
- Produces: `JiraConfig.CustomFields map[string]string` field

- [ ] **Step 1: Write test for CustomFields config**

Add to `config/config_test.go`:

```go
func TestJiraCustomFieldsConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	content := `services:
  jira:
    url: https://jira.example.com
    email: test@example.com
    token: test-token
    custom_fields:
      epic_key: "customfield_10014"
      blocked: "customfield_10517"
      story_points: "customfield_10028"
`
	os.WriteFile(path, []byte(content), 0600)

	cfg, err := Read(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if cfg.Services.Jira == nil {
		t.Fatal("expected Jira config")
	}
	if len(cfg.Services.Jira.CustomFields) != 3 {
		t.Errorf("expected 3 custom fields, got %d", len(cfg.Services.Jira.CustomFields))
	}
	if cfg.Services.Jira.CustomFields["epic_key"] != "customfield_10014" {
		t.Errorf("expected epic_key = customfield_10014, got %q", cfg.Services.Jira.CustomFields["epic_key"])
	}
}

func TestJiraNoCustomFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	content := `services:
  jira:
    url: https://jira.example.com
    email: test@example.com
    token: test-token
`
	os.WriteFile(path, []byte(content), 0600)

	cfg, err := Read(path)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if cfg.Services.Jira.CustomFields != nil && len(cfg.Services.Jira.CustomFields) != 0 {
		t.Errorf("expected nil or empty custom fields, got %v", cfg.Services.Jira.CustomFields)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./config/ -run TestJiraCustomFields -v`
Expected: compilation error — `CustomFields` field doesn't exist.

- [ ] **Step 3: Add CustomFields to JiraConfig**

In `config/config.go`, update `JiraConfig`:

```go
type JiraConfig struct {
	URL          string            `yaml:"url"`
	Email        string            `yaml:"email"`
	Token        string            `yaml:"token"`
	BotUsernames []string          `yaml:"bot_usernames,omitempty"`
	CustomFields map[string]string `yaml:"custom_fields,omitempty"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./config/ -v`
Expected: all tests pass.

- [ ] **Step 5: Run full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add config/config.go config/config_test.go
git commit --signoff -m "feat: configurable Jira custom fields in config.yaml"
```

---

### Task 3: Jira Watcher State Updates

**Files:**
- Modify: `watcher/jira/client.go`
- Modify: `watcher/jira/poller.go`

**Interfaces:**
- Consumes: `db.UpsertResourceState()` from Task 1, `JiraConfig.CustomFields` from Task 2, `config.Config` (existing)
- Produces: resource state rows written after each Jira issue poll

- [ ] **Step 1: Update IssueData struct**

In `watcher/jira/client.go`, add new fields to `IssueData`:

```go
type IssueData struct {
	Key          string
	Summary      string
	Status       string
	Priority     string
	IssueType    string
	Assignee     *string
	Labels       []string
	CreatedAt    string
	UpdatedAt    string
	Comments     []IssueComment
	Changelog    []ChangelogEntry
	CustomFields map[string]interface{}
}
```

- [ ] **Step 2: Update FetchIssue to accept custom field IDs and fetch new base fields**

In `watcher/jira/client.go`, change `FetchIssue` signature to accept custom field IDs:

```go
func (c *Client) FetchIssue(issueKey string, customFieldIDs map[string]string) (*IssueData, error) {
```

Update the URL to include the new base fields and configured custom fields:

```go
fields := "summary,status,assignee,labels,comment,priority,issuetype,created,updated"
for _, fieldID := range customFieldIDs {
	fields += "," + fieldID
}
url := fmt.Sprintf("%s/rest/api/3/issue/%s?expand=changelog&fields=%s", c.BaseURL, issueKey, fields)
```

Update the raw struct to parse the new fields:

```go
var raw struct {
	Key    string `json:"key"`
	Fields struct {
		Summary  string `json:"summary"`
		Status   struct {
			Name string `json:"name"`
		} `json:"status"`
		Priority struct {
			Name string `json:"name"`
		} `json:"priority"`
		IssueType struct {
			Name string `json:"name"`
		} `json:"issuetype"`
		Assignee *struct {
			DisplayName string `json:"displayName"`
		} `json:"assignee"`
		Labels    []string    `json:"labels"`
		Created   string      `json:"created"`
		Updated   string      `json:"updated"`
		Comment   struct { /* same as before */ } `json:"comment"`
	} `json:"fields"`
	Changelog struct { /* same as before */ } `json:"changelog"`
}
```

After decoding `raw`, also decode the full response into `map[string]interface{}` to extract custom fields:

```go
// Re-read body for custom fields (decode raw JSON fields)
var rawMap map[string]interface{}
// ... use a second decoder pass or buffer the body
```

Actually, a cleaner approach: decode the response body into both the typed struct and a raw map. Buffer the response body first:

```go
bodyBytes, err := io.ReadAll(resp.Body)
if err != nil {
	return nil, fmt.Errorf("failed to read response: %w", err)
}

// Decode typed fields
if err := json.Unmarshal(bodyBytes, &raw); err != nil {
	return nil, fmt.Errorf("failed to decode response: %w", err)
}

// Decode raw for custom fields
var rawMap map[string]json.RawMessage
if err := json.Unmarshal(bodyBytes, &rawMap); err != nil {
	return nil, fmt.Errorf("failed to decode raw response: %w", err)
}
var fieldsMap map[string]json.RawMessage
if rawFields, ok := rawMap["fields"]; ok {
	json.Unmarshal(rawFields, &fieldsMap)
}
```

Build the IssueData including custom fields:

```go
issue := &IssueData{
	Key:       raw.Key,
	Summary:   raw.Fields.Summary,
	Status:    raw.Fields.Status.Name,
	Priority:  raw.Fields.Priority.Name,
	IssueType: raw.Fields.IssueType.Name,
	Labels:    raw.Fields.Labels,
	CreatedAt: raw.Fields.Created,
	UpdatedAt: raw.Fields.Updated,
	CustomFields: make(map[string]interface{}),
}

// Extract custom fields
for displayName, fieldID := range customFieldIDs {
	if rawVal, ok := fieldsMap[fieldID]; ok {
		issue.CustomFields[displayName] = extractFieldValue(rawVal)
	}
}
```

Add the generic extraction function:

```go
// extractFieldValue extracts a display value from a Jira field's raw JSON.
// Objects with .value or .name use that string. Strings, numbers, nulls are direct.
func extractFieldValue(raw json.RawMessage) interface{} {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}

	var num float64
	if json.Unmarshal(raw, &num) == nil {
		return num
	}

	var obj map[string]interface{}
	if json.Unmarshal(raw, &obj) == nil {
		if v, ok := obj["value"]; ok {
			return v
		}
		if v, ok := obj["name"]; ok {
			return v
		}
		return obj
	}

	var arr []interface{}
	if json.Unmarshal(raw, &arr) == nil {
		return arr
	}

	return nil
}
```

Remove the hardcoded `customfield_12311140` (Red Hat's DC-era epic link) from the fields list and the `EpicKey` field from `IssueData`. Epic link is now configurable via `custom_fields`.

- [ ] **Step 3: Update poller.go to pass config and write resource state**

In `watcher/jira/poller.go`, update the `Poll` function to pass custom fields:

```go
func Poll(d *db.DB, cfg *config.Config, resources []watcher.Resource, logger *log.Logger) error {
	// ...existing setup...

	customFields := make(map[string]string)
	if cfg.Services.Jira != nil && cfg.Services.Jira.CustomFields != nil {
		customFields = cfg.Services.Jira.CustomFields
	}

	for _, resource := range resources {
		issueKey := resource.ResourceID

		issueData, err := client.FetchIssue(issueKey, customFields)
		// ...existing error handling...

		count, err := processIssue(d, cfg, issueData, resource, logger)
		// ...existing error handling...

		// Write resource state
		stateJSON := buildJiraStateJSON(issueData)
		now := time.Now().UTC().Format(time.RFC3339)
		if err := d.UpsertResourceState("jira", issueKey, stateJSON, issueData.UpdatedAt, now); err != nil {
			logger.Printf("WARNING: failed to upsert resource state for %s: %v", issueKey, err)
		}

		eventCount += count
	}
	// ...
}
```

Add the state JSON builder:

```go
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
```

- [ ] **Step 4: Update processIssue references**

The `processIssue` function and `latestTimestamp` use `issue.EpicKey` — remove these references since `EpicKey` is no longer a base field. Check all references in the file.

- [ ] **Step 5: Run full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 6: Test manually with a real Jira issue**

```bash
make build
handler watcher run jira --resources RHOAIENG-69748
handler query "SELECT state_json FROM resource_state WHERE resource_id = 'RHOAIENG-69748'" 2>/dev/null
```

Verify the state JSON contains the base fields and any configured custom fields.

- [ ] **Step 7: Commit**

```bash
git add watcher/jira/client.go watcher/jira/poller.go
git commit --signoff -m "feat: Jira watcher caches resource state with configurable custom fields"
```

---

### Task 4: GitHub Watcher State Updates

**Files:**
- Modify: `watcher/github/graphql.go`
- Modify: `watcher/github/poller.go`

**Interfaces:**
- Consumes: `db.UpsertResourceState()` from Task 1, `PRData` (existing)
- Produces: resource state rows written after each PR poll

- [ ] **Step 1: Add committedDate to GraphQL query and CommitInfo struct**

In `watcher/github/graphql.go`, update `CommitInfo`:

```go
type CommitInfo struct {
	TotalCount    int
	LatestSHA     string
	LatestDate    string
}
```

In the GraphQL query string (around line 215), add `committedDate` to the commit query:

```graphql
commits(last: 1) {
  totalCount
  nodes {
    commit {
      oid
      committedDate
      checkSuites(last: 10) {
```

Update `commitNode` struct to include `CommittedDate`:

```go
type commitNode struct {
	Commit struct {
		OID           string `json:"oid"`
		CommittedDate string `json:"committedDate"`
		CheckSuites   struct { /* same */ } `json:"checkSuites"`
	} `json:"commit"`
}
```

Update `parsePRNode` (around line 446) to populate `LatestDate`:

```go
if len(node.Commits.Nodes) > 0 {
	data.Commits.LatestSHA = node.Commits.Nodes[0].Commit.OID
	data.Commits.LatestDate = node.Commits.Nodes[0].Commit.CommittedDate
}
```

- [ ] **Step 2: Add state derivation functions to poller.go**

In `watcher/github/poller.go`, add:

```go
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

func buildPRStateJSON(prData PRData) string {
	state := map[string]interface{}{
		"title":                        prData.Title,
		"state":                        prData.State,
		"review_decision":              derivePRReviewDecision(prData.Reviews),
		"has_new_commits_since_review": hasNewCommitsSinceReview(prData),
		"ci_status":                    deriveCIStatus(prData.CheckRuns),
	}
	data, _ := json.Marshal(state)
	return string(data)
}
```

- [ ] **Step 3: Write resource state in processPR**

At the end of `processPR` in `watcher/github/poller.go`, before the return, add:

```go
// Write resource state
stateJSON := buildPRStateJSON(prData)
now := time.Now().UTC().Format(time.RFC3339)
if err := d.UpsertResourceState("pr", resource.ResourceID, stateJSON, prData.UpdatedAt, now); err != nil {
	logger.Printf("WARNING: failed to upsert resource state for %s: %v", resource.ResourceID, err)
}
```

Add the required imports (`encoding/json`, `time`).

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 5: Test manually with a real PR**

```bash
make build
handler watcher run github --resources opendatahub-io/odh-dashboard#8312
handler query "SELECT state_json FROM resource_state WHERE resource_id = 'opendatahub-io/odh-dashboard#8312'" 2>/dev/null
```

Verify the state JSON contains title, state, review_decision, ci_status, has_new_commits_since_review.

- [ ] **Step 6: Commit**

```bash
git add watcher/github/graphql.go watcher/github/poller.go
git commit --signoff -m "feat: GitHub watcher caches PR state with review decision and CI status"
```

---

### Task 5: Resource State Cleanup on Unsubscribe

**Files:**
- Modify: `db/subscriptions.go`
- Modify: `db/resource_state_test.go`

**Interfaces:**
- Consumes: `db.DeleteResourceState()` from Task 1, `db.Unsubscribe()` (existing)
- Produces: automatic resource_state cleanup when last subscription is removed

- [ ] **Step 1: Write test for cleanup behavior**

Add to `db/resource_state_test.go`:

```go
func TestResourceStateCleanupOnLastUnsubscribe(t *testing.T) {
	d := testDB(t)
	seedSession(t, d, "cleanup-sess-1")
	seedSession(t, d, "cleanup-sess-2")

	now := "2026-07-06T10:00:00Z"
	d.Subscribe(Subscription{ID: "sub-a", SessionID: "cleanup-sess-1", ResourceType: "pr", ResourceID: "owner/repo#1", CreatedAt: now})
	d.Subscribe(Subscription{ID: "sub-b", SessionID: "cleanup-sess-2", ResourceType: "pr", ResourceID: "owner/repo#1", CreatedAt: now})
	d.UpsertResourceState("pr", "owner/repo#1", `{"state":"open"}`, now, now)

	// Unsubscribe first session — state should remain (other session still subscribed)
	d.Unsubscribe("cleanup-sess-1", "pr", "owner/repo#1")
	rs, _ := d.GetResourceState("pr", "owner/repo#1")
	if rs == nil {
		t.Fatal("resource state should still exist after first unsubscribe")
	}

	// Unsubscribe second session — state should be deleted (no more subscribers)
	d.Unsubscribe("cleanup-sess-2", "pr", "owner/repo#1")
	rs, _ = d.GetResourceState("pr", "owner/repo#1")
	if rs != nil {
		t.Fatal("resource state should be deleted after last unsubscribe")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./db/ -run TestResourceStateCleanup -v`
Expected: FAIL — cleanup not implemented yet.

- [ ] **Step 3: Update Unsubscribe to clean up resource state**

In `db/subscriptions.go`, at the end of `Unsubscribe` (after the successful soft-delete), add:

```go
// Check if this was the last active subscription for this resource
var remaining int
db.conn.QueryRow(`
	SELECT COUNT(*) FROM subscriptions
	WHERE resource_type = ? AND resource_id = ? AND deleted_at IS NULL
`, resourceType, resourceID).Scan(&remaining)

if remaining == 0 {
	db.DeleteResourceState(resourceType, resourceID)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./db/ -v`
Expected: all tests pass.

- [ ] **Step 5: Run full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add db/subscriptions.go db/resource_state_test.go
git commit --signoff -m "feat: clean up resource_state when last subscription is removed"
```

---

### Task 6: Enhanced Triage with Resource State

**Files:**
- Modify: `cmd/triage.go`

**Interfaces:**
- Consumes: `db.ListResourceStatesForSession()` from Task 1, `config.ResourceTypeToService()` (existing)
- Produces: `session_resources` and `stale_resources` in triage JSON output

- [ ] **Step 1: Add new types to triage output**

In `cmd/triage.go`, add types and update `triageOutput`:

```go
type sessionResource struct {
	SessionID    string            `json:"session_id"`
	SessionName  string            `json:"session_name"`
	Resources    []resourceDetail  `json:"resources"`
}

type resourceDetail struct {
	ResourceType     string          `json:"resource_type"`
	ResourceID       string          `json:"resource_id"`
	ResourceURL      *string         `json:"resource_url,omitempty"`
	State            json.RawMessage `json:"state"`
	WatcherUpdatedAt string          `json:"watcher_updated_at,omitempty"`
}

type staleResource struct {
	ResourceType     string `json:"resource_type"`
	ResourceID       string `json:"resource_id"`
	WatcherUpdatedAt string `json:"watcher_updated_at"`
	StaleMinutes     int    `json:"stale_minutes"`
}
```

Add new fields to `triageOutput`:

```go
type triageOutput struct {
	// ...existing fields...
	SessionResources []sessionResource `json:"session_resources"`
	StaleResources   []staleResource   `json:"stale_resources"`
}
```

- [ ] **Step 2: Populate session resources and detect stale data**

In `runTriage`, after the existing session processing, add:

```go
// Gather resource state per session
output.SessionResources = []sessionResource{}
output.StaleResources = []staleResource{}
staleThreshold := 5 * time.Minute
seenResources := make(map[string]bool) // dedup stale resources across sessions

for _, s := range sessions {
	if s.Status != "active" {
		continue
	}
	// Skip dead sessions
	isDead := false
	for _, ds := range output.DeadSessions {
		if ds.SessionID == s.SessionID {
			isDead = true
			break
		}
	}
	if isDead {
		continue
	}

	states, err := d.ListResourceStatesForSession(s.SessionID)
	if err != nil || len(states) == 0 {
		continue
	}

	sr := sessionResource{
		SessionID:   s.SessionID,
		SessionName: s.SessionName,
		Resources:   []resourceDetail{},
	}

	for _, rs := range states {
		rd := resourceDetail{
			ResourceType:     rs.ResourceType,
			ResourceID:       rs.ResourceID,
			ResourceURL:      rs.ResourceURL,
			State:            json.RawMessage(rs.StateJSON),
			WatcherUpdatedAt: rs.WatcherUpdatedAt,
		}
		sr.Resources = append(sr.Resources, rd)

		// Check staleness
		key := rs.ResourceType + ":" + rs.ResourceID
		if !seenResources[key] && rs.WatcherUpdatedAt != "" {
			seenResources[key] = true
			wut, err := time.Parse(time.RFC3339, rs.WatcherUpdatedAt)
			if err == nil && time.Since(wut) > staleThreshold {
				output.StaleResources = append(output.StaleResources, staleResource{
					ResourceType:     rs.ResourceType,
					ResourceID:       rs.ResourceID,
					WatcherUpdatedAt: rs.WatcherUpdatedAt,
					StaleMinutes:     int(time.Since(wut).Minutes()),
				})
			}
		}
	}

	output.SessionResources = append(output.SessionResources, sr)
}

// Trigger catch-up for stale resources (best-effort, non-blocking)
if len(output.StaleResources) > 0 {
	cfg, _ := config.Read(config.DefaultPath())
	if cfg != nil {
		staleByService := make(map[string][]string)
		for _, sr := range output.StaleResources {
			svc := config.ResourceTypeToService(sr.ResourceType)
			if svc != "" && cfg.IsServiceConfigured(svc) {
				staleByService[svc] = append(staleByService[svc], sr.ResourceID)
			}
		}
		for svc, resources := range staleByService {
			resourceList := strings.Join(resources, ",")
			go func(s, r string) {
				exec.Command("handler", "watcher", "run", s, "--resources", r).Run()
			}(svc, resourceList)
		}
	}
}
```

Add required imports: `encoding/json`, `os/exec`, `time`, `github.com/mturley/agent-handler/config`.

- [ ] **Step 3: Update text output for resource info**

Add a resources section to the text output:

```go
if len(output.SessionResources) > 0 {
	fmt.Println("\nSession Resources:")
	for _, sr := range output.SessionResources {
		name := sr.SessionName
		if name == "" {
			name = sr.SessionID[:8]
		}
		for _, r := range sr.Resources {
			fmt.Printf("  %s → %s:%s", name, r.ResourceType, r.ResourceID)
			if r.WatcherUpdatedAt != "" {
				wut, err := time.Parse(time.RFC3339, r.WatcherUpdatedAt)
				if err == nil {
					fmt.Printf(" (updated %s ago)", formatDuration(time.Since(wut)))
				}
			}
			fmt.Println()
		}
	}
}

if len(output.StaleResources) > 0 {
	fmt.Println("\nStale Resources (catch-up triggered):")
	for _, sr := range output.StaleResources {
		fmt.Printf("  %s:%s — last updated %dm ago\n", sr.ResourceType, sr.ResourceID, sr.StaleMinutes)
	}
}
```

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 5: Test manually**

```bash
make build
./bin/handler triage --json | python3 -m json.tool | grep -A 20 session_resources
```

- [ ] **Step 6: Commit**

```bash
git add cmd/triage.go
git commit --signoff -m "feat: enhanced triage with resource state and staleness detection"
```

---

### Task 7: Setup Mentions Custom Fields

**Files:**
- Modify: `cmd/setup.go`

**Interfaces:**
- Consumes: existing setup flow
- Produces: informational message about custom fields after Jira setup

- [ ] **Step 1: Find the Jira setup completion point in cmd/setup.go**

Read `cmd/setup.go` to find where Jira configuration is confirmed (look for "Jira is already configured" or the token validation success message).

- [ ] **Step 2: Add custom fields message**

After the Jira token validation success message, add:

```go
fmt.Println("\n  Custom Jira fields can be configured in config.yaml under services.jira.custom_fields.")
fmt.Println("  Adding custom fields (e.g. priority, blocked status, epic links) provides additional")
fmt.Println("  context when the handler session triages work across sessions.")
fmt.Println("  See the commented examples in config.yaml for common fields.")
```

- [ ] **Step 3: Add commented examples to the config file during setup**

When setup writes a new config file (or when Jira is first configured), include commented custom_fields examples in the YAML output. Check how the config is currently written — if using `yaml.Marshal`, you may need to write the comments separately since Go's yaml library doesn't support comments. A pragmatic approach: after writing the config, append the commented section if `custom_fields` is not already present.

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/setup.go
git commit --signoff -m "feat: setup mentions Jira custom fields configuration"
```

---

### Task 8: /handler Skill Rewrite

**Files:**
- Modify: `skills/handler/SKILL.md`

**Interfaces:**
- Consumes: `handler triage --json` (enhanced from Task 6), `handler log --global --since-cursor --json` (existing), `handler peek --session <id> --json` (existing from Phase 4)
- Produces: updated /handler skill with prioritized briefing workflow

- [ ] **Step 1: Rewrite skills/handler/SKILL.md**

Replace the full content of `skills/handler/SKILL.md` with:

```markdown
---
name: handler
description: "Turn this session into the handler — a command center for managing all active sessions. Use when you want a global view of all sessions, events, and resources."
---

# /handler — Handler Session

## On invocation

1. Set this session's role (if not already set):
```bash
handler configure --role handler
```

2. Gather data (run both in parallel):
```bash
handler triage --json
handler log --global --since-cursor --json
```

3. Peek at all peekable sessions — for each session in the triage output with `"peekable": true` and `display_state` of `"active"` or `"idle"`, spawn a Haiku subagent that:
   - Runs `handler peek --session <session_id> --json`
   - Answers: "Is this session waiting for user input (permission prompt, question, or approval)? If yes, what exactly is it asking? If no, say 'working' or 'idle at prompt'."
   - Returns a 1-2 sentence summary

4. Present a prioritized briefing with three sections:

### Action Items

Ordered by priority. Use your judgment to rank items, but default to this order:

1. **Sessions waiting for input** — permission prompts, questions, approval requests (from peek results). Sessions working on higher-priority resources (Blocker/Critical Jira issues, PRs with failing CI) rank higher.
2. **Blocked sessions** — from triage `blocked_sessions`
3. **Unread external events** — PR reviews with changes requested, new comments on your PRs, Jira status changes. Derive what needs attention from triage `sessions_with_unread` combined with `session_resources` state.
4. **Stale resources** — from triage `stale_resources`, where watcher data couldn't be refreshed

Weight priority by resource importance: a session working on a Blocker/Critical Jira issue ranks higher than one on a Normal issue. A PR with `ci_status: "FAILURE"` or `review_decision: "CHANGES_REQUESTED"` ranks higher than one with passing CI.

### Timeline

Chronological list of events since last report (from `handler log --global --since-cursor`). Group by session, showing milestones, decisions, status updates, and external events.

### Session Overview

Table of all sessions with: name, branch, display state, peek summary, subscribed resources with their current state (priority, status, review decision, CI status).

5. Advance the cursor after presenting.

6. Set up a polling loop (check CronList first — skip if already exists):
```
CronCreate:
  cron: "*/1 * * * *"
  durable: false
  recurring: true
  prompt: "MANDATORY: You MUST call the Bash tool to run: handler log --global --since-cursor --agent-only --json 2>/dev/null. NEVER skip this Bash call. Also run handler unread --count to check for direct messages. If there are new events or direct messages, summarize them. For direct messages, present them as action items. If no events, say 'No new events.'"
```

7. Tell the user what they can ask.

## What the user can ask

- "What's going on?" → re-run the full briefing (steps 2-4 above)
- "What changed since last time?" → `handler log --global --since-cursor --json`
- "What should I work on?" → re-run triage, reason about priorities using resource state
- "Tell session X about Y" → `handler emit --type message --title "Y" --to <target>`
- "Show me everything about PR #123" → `handler resource history pr:owner/repo#123`
- "Which sessions are related to X?" → `handler resource related --session <id>`
- "What is session X doing?" → spawn a subagent with `handler peek --session <id> --json`
- "Check on all sessions" → peek at each peekable session via subagents, summarize

Use `handler <command> --help` for flag details on any command.

## Peeking at sessions

Always use subagents for peek — raw captures can be hundreds of lines and will flood your context. Each subagent distills the capture to a short summary.

Use Haiku for peek subagents — the task is focused (detect permission prompts/questions) and fast.

**When to peek:**
- During every briefing (step 3 above)
- When the user asks about a specific session
- Sessions that appear stuck, blocked, or idle

## Idempotent

Re-invoking /handler re-runs the full briefing. If the cron job already exists, don't create a duplicate.
```

- [ ] **Step 2: Update skills/using-handler/SKILL.md**

In the "Key commands" section, add or update:

```markdown
- `handler triage` — aggregates what needs attention: sessions, resources, blockers, unread events with resource state
```

- [ ] **Step 3: Commit**

```bash
git add skills/handler/SKILL.md skills/using-handler/SKILL.md
git commit --signoff -m "feat: rewrite /handler skill for prioritized briefing with peek and resource state"
```
