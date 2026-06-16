# Phase 2: External Watchers — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add external event watchers (GitHub PR, Jira) that poll APIs on a schedule and write events to the handler ledger for sessions subscribed to those resources.

**Architecture:** Config package reads/writes `~/.agent-handler/config.yaml` for API tokens. Watcher package provides shared framework (active resource query, cursor logic, dedup, event writing). GitHub and Jira watchers implement the polling logic. CLI commands under `handler watcher` manage scheduling and auth. Existing commands (`setup`, `subscribe`, `register`, `uninstall`) gain integration points.

**Tech Stack:** Go 1.22+, `gopkg.in/yaml.v3` for config, `net/http` for API calls, `encoding/json` for GraphQL/REST, existing `modernc.org/sqlite` DB layer.

**Spec:** `docs/superpowers/specs/2026-06-16-phase2-external-watchers.md`

---

## File Structure

```
config/
├── config.go              # Config struct, Read/Write, DefaultPath
├── config_test.go         # Config read/write tests
├── validate.go            # Token validation (GitHub GraphQL, Jira REST)
└── validate_test.go       # Validation tests (mock HTTP)
watcher/
├── framework.go           # ActiveResources query, EventCursor, Dedup check, EmitEvent helper
├── framework_test.go      # Framework tests
├── scheduler.go           # Launchd plist / cron entry creation and removal
├── scheduler_test.go      # Scheduler tests
├── github/
│   ├── graphql.go         # GitHub GraphQL query builder and response types
│   ├── poller.go          # PR polling: diff against cursor, emit events
│   └── poller_test.go     # Poller tests (mock GraphQL responses)
└── jira/
    ├── client.go          # Jira REST client (comments, changelog, epic link)
    ├── poller.go          # Issue polling: diff against cursor, emit events
    └── poller_test.go     # Poller tests (mock REST responses)
cmd/watcher/
├── watcher.go             # Parent cobra command
├── run.go                 # handler watcher run
├── install.go             # handler watcher install
├── uninstall_cmd.go       # handler watcher uninstall
├── list.go                # handler watcher list
├── logs.go                # handler watcher logs
├── auth.go                # handler watcher auth
└── auth_status.go         # handler watcher auth status
```

**Modified files:**
- `cmd/root.go` — register watcher subcommand
- `cmd/setup.go` — add watcher auth step
- `cmd/subscribe.go` — add service auth guard
- `cmd/register.go` — add background catch-up watcher run
- `cmd/uninstall.go` — uninstall watcher schedules
- `go.mod` — add `gopkg.in/yaml.v3`

---

## Task 1: Config Package

**Files:**
- Create: `config/config.go`, `config/config_test.go`

- [ ] **Step 1: Add yaml dependency**

```bash
go get gopkg.in/yaml.v3@latest
```

- [ ] **Step 2: Write config tests**

```go
// config/config_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadWriteConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := &Config{
		Services: Services{
			GitHub: &GitHubConfig{Token: "ghp_test123"},
			Jira: &JiraConfig{
				URL:          "https://test.atlassian.net",
				Email:        "user@test.com",
				Token:        "jira_test",
				BotUsernames: []string{"bot1", "bot2"},
			},
		},
	}

	err := Write(path, cfg)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Services.GitHub.Token != "ghp_test123" {
		t.Errorf("expected ghp_test123, got %s", got.Services.GitHub.Token)
	}
	if got.Services.Jira.URL != "https://test.atlassian.net" {
		t.Errorf("expected test.atlassian.net, got %s", got.Services.Jira.URL)
	}
	if len(got.Services.Jira.BotUsernames) != 2 {
		t.Errorf("expected 2 bot usernames, got %d", len(got.Services.Jira.BotUsernames))
	}
}

func TestReadMissingFile(t *testing.T) {
	cfg, err := Read("/nonexistent/config.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if cfg.Services.GitHub != nil {
		t.Error("expected nil GitHub config")
	}
}

func TestIsServiceConfigured(t *testing.T) {
	cfg := &Config{
		Services: Services{
			GitHub: &GitHubConfig{Token: "ghp_test"},
		},
	}
	if !cfg.IsServiceConfigured("github") {
		t.Error("expected github configured")
	}
	if cfg.IsServiceConfigured("jira") {
		t.Error("expected jira not configured")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./config/ -v`
Expected: FAIL — package doesn't exist

- [ ] **Step 4: Implement config.go**

```go
// config/config.go
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Services Services `yaml:"services"`
}

type Services struct {
	GitHub *GitHubConfig `yaml:"github,omitempty"`
	Jira   *JiraConfig   `yaml:"jira,omitempty"`
}

type GitHubConfig struct {
	Token string `yaml:"token"`
}

type JiraConfig struct {
	URL          string   `yaml:"url"`
	Email        string   `yaml:"email"`
	Token        string   `yaml:"token"`
	BotUsernames []string `yaml:"bot_usernames,omitempty"`
}

func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agent-handler", "config.yaml")
}

func Read(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

func Write(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

func (c *Config) IsServiceConfigured(service string) bool {
	switch service {
	case "github":
		return c.Services.GitHub != nil && c.Services.GitHub.Token != ""
	case "jira":
		return c.Services.Jira != nil && c.Services.Jira.Token != ""
	default:
		return false
	}
}

func ResourceTypeToService(resourceType string) string {
	switch resourceType {
	case "pr":
		return "github"
	case "jira":
		return "jira"
	default:
		return ""
	}
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./config/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add config/ go.mod go.sum
git commit --signoff -m "feat: config package for reading/writing service API tokens"
```

---

## Task 2: Token Validation

**Files:**
- Create: `config/validate.go`, `config/validate_test.go`

- [ ] **Step 1: Write validation tests**

```go
// config/validate_test.go
package config

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateGitHubToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer valid_token" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message":"Bad credentials"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":{"viewer":{"login":"testuser"}}}`))
	}))
	defer server.Close()

	username, err := ValidateGitHubToken("valid_token", server.URL)
	if err != nil {
		t.Fatalf("expected valid, got error: %v", err)
	}
	if username != "testuser" {
		t.Errorf("expected testuser, got %s", username)
	}

	_, err = ValidateGitHubToken("bad_token", server.URL)
	if err == nil {
		t.Error("expected error for bad token")
	}
}

func TestValidateJiraToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/myself" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		user, _, ok := r.BasicAuth()
		if !ok || user != "user@test.com" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"displayName":"Test User","emailAddress":"user@test.com"}`))
	}))
	defer server.Close()

	name, err := ValidateJiraToken(server.URL, "user@test.com", "valid_token")
	if err != nil {
		t.Fatalf("expected valid, got error: %v", err)
	}
	if name != "Test User" {
		t.Errorf("expected Test User, got %s", name)
	}

	_, err = ValidateJiraToken(server.URL, "bad@test.com", "bad_token")
	if err == nil {
		t.Error("expected error for bad credentials")
	}
}
```

- [ ] **Step 2: Implement validate.go**

```go
// config/validate.go
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const githubAPIDefault = "https://api.github.com"

func ValidateGitHubToken(token string, apiURL ...string) (username string, err error) {
	url := githubAPIDefault + "/graphql"
	if len(apiURL) > 0 {
		url = apiURL[0]
	}

	query := `{"query":"{ viewer { login } }"}`
	req, err := http.NewRequest("POST", url, bytes.NewBufferString(query))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Viewer struct {
				Login string `json:"login"`
			} `json:"viewer"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing GitHub response: %w", err)
	}
	if result.Data.Viewer.Login == "" {
		return "", fmt.Errorf("GitHub token validation failed: no login returned")
	}

	return result.Data.Viewer.Login, nil
}

func ValidateJiraToken(baseURL, email, token string) (displayName string, err error) {
	url := baseURL + "/rest/api/3/myself"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(email, token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Jira API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Jira API returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		DisplayName string `json:"displayName"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing Jira response: %w", err)
	}
	if result.DisplayName == "" {
		return "", fmt.Errorf("Jira token validation failed: no display name returned")
	}

	return result.DisplayName, nil
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./config/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add config/validate.go config/validate_test.go
git commit --signoff -m "feat: GitHub and Jira token validation with test API calls"
```

---

## Task 3: Watcher CLI Subcommand Group and Auth

**Files:**
- Create: `cmd/watcher/watcher.go`, `cmd/watcher/auth.go`, `cmd/watcher/auth_status.go`
- Modify: `cmd/root.go`

- [ ] **Step 1: Create the watcher parent command**

```go
// cmd/watcher/watcher.go
package watcher

import "github.com/spf13/cobra"

var JSONOutput *bool

var WatcherCmd = &cobra.Command{
	Use:   "watcher",
	Short: "Manage external event watchers",
}
```

- [ ] **Step 2: Create handler watcher auth**

```go
// cmd/watcher/auth.go
package watcher

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mturley/agent-handler/config"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth [service]",
	Short: "Configure API tokens for external services",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runAuth,
}

func init() {
	WatcherCmd.AddCommand(authCmd)
}

func runAuth(cmd *cobra.Command, args []string) error {
	cfg, err := config.Read(config.DefaultPath())
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	reader := bufio.NewReader(os.Stdin)

	if len(args) == 0 || args[0] == "github" {
		if err := authGitHub(cfg, reader); err != nil && len(args) > 0 {
			return err
		}
	}

	if len(args) == 0 || args[0] == "jira" {
		if err := authJira(cfg, reader); err != nil && len(args) > 0 {
			return err
		}
	}

	return nil
}

func authGitHub(cfg *config.Config, reader *bufio.Reader) error {
	fmt.Println("\nGitHub Personal Access Token")
	fmt.Println("  Create one at: https://github.com/settings/tokens")
	fmt.Println("  Required scopes: repo (for private repos) or public_repo (for public only)")

	if cfg.Services.GitHub != nil && cfg.Services.GitHub.Token != "" {
		fmt.Println("  Existing token found. Press Enter to keep it, or paste a new one.")
	}

	fmt.Print("  Token (or Enter to skip): ")
	token, _ := reader.ReadString('\n')
	token = strings.TrimSpace(token)

	if token == "" {
		if cfg.Services.GitHub != nil && cfg.Services.GitHub.Token != "" {
			fmt.Print("  Validating existing token... ")
			username, err := config.ValidateGitHubToken(cfg.Services.GitHub.Token)
			if err != nil {
				fmt.Printf("invalid: %v\n", err)
				return nil
			}
			fmt.Printf("valid (authenticated as %s)\n", username)
		} else {
			fmt.Println("  Skipped.")
		}
		return nil
	}

	fmt.Print("  Validating... ")
	username, err := config.ValidateGitHubToken(token)
	if err != nil {
		fmt.Printf("failed: %v\n", err)
		return fmt.Errorf("GitHub token validation failed")
	}
	fmt.Printf("valid (authenticated as %s)\n", username)

	cfg.Services.GitHub = &config.GitHubConfig{Token: token}
	if err := config.Write(config.DefaultPath(), cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	fmt.Println("  ✓ GitHub token saved")

	return nil
}

func authJira(cfg *config.Config, reader *bufio.Reader) error {
	fmt.Println("\nJira API Token")
	fmt.Println("  Create one at: https://id.atlassian.com/manage-profile/security/api-tokens")

	if cfg.Services.Jira != nil && cfg.Services.Jira.Token != "" {
		fmt.Println("  Existing token found. Press Enter to keep it, or enter new values.")
	}

	fmt.Print("  Instance URL (e.g. https://yoursite.atlassian.net, or Enter to skip): ")
	url, _ := reader.ReadString('\n')
	url = strings.TrimSpace(url)

	if url == "" {
		if cfg.Services.Jira != nil && cfg.Services.Jira.Token != "" {
			fmt.Print("  Validating existing token... ")
			name, err := config.ValidateJiraToken(cfg.Services.Jira.URL, cfg.Services.Jira.Email, cfg.Services.Jira.Token)
			if err != nil {
				fmt.Printf("invalid: %v\n", err)
				return nil
			}
			fmt.Printf("valid (authenticated as %s)\n", name)
		} else {
			fmt.Println("  Skipped.")
		}
		return nil
	}

	fmt.Print("  Email: ")
	email, _ := reader.ReadString('\n')
	email = strings.TrimSpace(email)

	fmt.Print("  Token: ")
	token, _ := reader.ReadString('\n')
	token = strings.TrimSpace(token)

	fmt.Print("  Validating... ")
	name, err := config.ValidateJiraToken(url, email, token)
	if err != nil {
		fmt.Printf("failed: %v\n", err)
		return fmt.Errorf("Jira token validation failed")
	}
	fmt.Printf("valid (authenticated as %s)\n", name)

	cfg.Services.Jira = &config.JiraConfig{
		URL:   url,
		Email: email,
		Token: token,
	}
	if cfg.Services.Jira.BotUsernames == nil {
		cfg.Services.Jira.BotUsernames = []string{}
	}
	if err := config.Write(config.DefaultPath(), cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	fmt.Println("  ✓ Jira token saved")

	return nil
}
```

- [ ] **Step 3: Create handler watcher auth status**

```go
// cmd/watcher/auth_status.go
package watcher

import (
	"fmt"

	"github.com/mturley/agent-handler/config"
	"github.com/spf13/cobra"
)

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show which services are configured",
	RunE:  runAuthStatus,
}

func init() {
	authCmd.AddCommand(authStatusCmd)
}

func runAuthStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.Read(config.DefaultPath())
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	fmt.Println("Service authentication status:")
	if cfg.IsServiceConfigured("github") {
		fmt.Println("  ✓ GitHub: configured")
	} else {
		fmt.Println("  ✗ GitHub: not configured")
	}
	if cfg.IsServiceConfigured("jira") {
		fmt.Printf("  ✓ Jira: configured (%s)\n", cfg.Services.Jira.URL)
	} else {
		fmt.Println("  ✗ Jira: not configured")
	}

	return nil
}
```

- [ ] **Step 4: Register watcher subcommand in root.go**

Add to `cmd/root.go` imports and `init()`:
```go
import "github.com/mturley/agent-handler/cmd/watcher"

// In init():
watcher.JSONOutput = &jsonOutput
rootCmd.AddCommand(watcher.WatcherCmd)
```

Set the group ID on the watcher command:
```go
watcher.WatcherCmd.GroupID = "human"
```

- [ ] **Step 5: Build and verify**

```bash
go build ./...
./bin/handler watcher --help
./bin/handler watcher auth status
```

- [ ] **Step 6: Commit**

```bash
git add cmd/watcher/ cmd/root.go
git commit --signoff -m "feat: handler watcher subcommand with auth and auth status"
```

---

## Task 4: Watcher Framework — Active Resources and Cursors

**Files:**
- Create: `watcher/framework.go`, `watcher/framework_test.go`

- [ ] **Step 1: Write framework tests**

```go
// watcher/framework_test.go
package watcher

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mturley/agent-handler/db"
)

func testDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func seedActiveSession(t *testing.T, d *db.DB, id, branch string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	d.UpsertSession(db.Session{
		SessionID: id, Harness: "claude", Repo: "owner/repo", Branch: branch,
		Status: "active", InboxMode: "manual", LastActive: now,
		RegisteredAt: now, JSONLPath: "/p/" + id + ".jsonl",
	})
}

func TestActiveResources(t *testing.T) {
	d := testDB(t)
	seedActiveSession(t, d, "s1", "main")

	d.Subscribe(db.Subscription{
		ID: uuid.New().String(), SessionID: "s1", ResourceType: "pr",
		ResourceID: "owner/repo#42", CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	resources, err := ActiveResources(d, "pr")
	if err != nil {
		t.Fatalf("ActiveResources: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if resources[0].ResourceID != "owner/repo#42" {
		t.Errorf("expected owner/repo#42, got %s", resources[0].ResourceID)
	}
}

func TestActiveResourcesSkipsArchived(t *testing.T) {
	d := testDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	d.UpsertSession(db.Session{
		SessionID: "s1", Harness: "claude", Repo: "r", Branch: "b",
		Status: "archived", InboxMode: "manual", LastActive: now,
		RegisteredAt: now, JSONLPath: "/p/s1.jsonl",
	})
	d.Subscribe(db.Subscription{
		ID: uuid.New().String(), SessionID: "s1", ResourceType: "pr",
		ResourceID: "owner/repo#99", CreatedAt: now,
	})

	resources, _ := ActiveResources(d, "pr")
	if len(resources) != 0 {
		t.Errorf("expected 0 resources for archived session, got %d", len(resources))
	}
}

func TestEventCursorEmpty(t *testing.T) {
	d := testDB(t)
	cursor := EventCursor(d, "github", "pr", "owner/repo#42")
	if cursor != "" {
		t.Errorf("expected empty cursor, got %s", cursor)
	}
}

func TestEventCursorAfterEvent(t *testing.T) {
	d := testDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	ext := "2026-06-15T10:00:00Z"
	d.InsertEvent(db.Event{
		ID: uuid.New().String(), TS: now, ExternalTS: &ext,
		Source: "github", Type: "pr_comment", Title: "test",
	}, nil, []db.EventResource{
		{ResourceType: "pr", ResourceID: "owner/repo#42"},
	})

	cursor := EventCursor(d, "github", "pr", "owner/repo#42")
	if cursor != ext {
		t.Errorf("expected %s, got %s", ext, cursor)
	}
}

func TestIsDuplicate(t *testing.T) {
	d := testDB(t)
	ext := "2026-06-15T10:00:00Z"
	d.InsertEvent(db.Event{
		ID: uuid.New().String(), TS: time.Now().UTC().Format(time.RFC3339),
		ExternalTS: &ext, Source: "github", Type: "pr_comment", Title: "test",
	}, nil, []db.EventResource{
		{ResourceType: "pr", ResourceID: "owner/repo#42"},
	})

	if !IsDuplicate(d, "github", "pr", "owner/repo#42", "pr_comment", ext) {
		t.Error("expected duplicate")
	}
	if IsDuplicate(d, "github", "pr", "owner/repo#42", "pr_comment", "2026-06-16T10:00:00Z") {
		t.Error("expected not duplicate")
	}
}
```

- [ ] **Step 2: Implement framework.go**

```go
// watcher/framework.go
package watcher

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/mturley/agent-handler/db"
)

type Resource struct {
	ResourceType string
	ResourceID   string
	ResourceURL  string
}

func ActiveResources(d *db.DB, resourceType string) ([]Resource, error) {
	rows, err := d.Conn().Query(`
		SELECT DISTINCT sub.resource_type, sub.resource_id, COALESCE(sub.resource_url, '')
		FROM subscriptions sub
		JOIN sessions s ON s.session_id = sub.session_id
		WHERE sub.deleted_at IS NULL
		  AND sub.resource_type = ?
		  AND s.status = 'active'
	`, resourceType)
	if err != nil {
		return nil, fmt.Errorf("querying active resources: %w", err)
	}
	defer rows.Close()

	var resources []Resource
	for rows.Next() {
		var r Resource
		if err := rows.Scan(&r.ResourceType, &r.ResourceID, &r.ResourceURL); err != nil {
			return nil, err
		}
		resources = append(resources, r)
	}
	return resources, rows.Err()
}

func EventCursor(d *db.DB, source, resourceType, resourceID string) string {
	var cursor string
	err := d.Conn().QueryRow(`
		SELECT MAX(e.external_ts) FROM events e
		JOIN event_resources er ON er.event_id = e.id
		WHERE er.resource_type = ? AND er.resource_id = ?
		  AND e.source = ?
		  AND e.external_ts IS NOT NULL
	`, resourceType, resourceID, source).Scan(&cursor)
	if err != nil {
		return ""
	}
	return cursor
}

func IsDuplicate(d *db.DB, source, resourceType, resourceID, eventType, externalTS string) bool {
	var count int
	d.Conn().QueryRow(`
		SELECT COUNT(*) FROM events e
		JOIN event_resources er ON er.event_id = e.id
		WHERE e.source = ? AND e.type = ? AND e.external_ts = ?
		  AND er.resource_type = ? AND er.resource_id = ?
	`, source, eventType, externalTS, resourceType, resourceID).Scan(&count)
	return count > 0
}

func EmitWatcherEvent(d *db.DB, source, eventType, title string, body *string, externalTS string, author, authorType *string, resource Resource) error {
	return d.InsertEvent(db.Event{
		ID:         uuid.New().String(),
		TS:         time.Now().UTC().Format(time.RFC3339),
		ExternalTS: &externalTS,
		Source:     source,
		Type:       eventType,
		Title:      title,
		Body:       body,
		Author:     author,
		AuthorType: authorType,
	}, nil, []db.EventResource{
		{ResourceType: resource.ResourceType, ResourceID: resource.ResourceID, ResourceURL: strPtr(resource.ResourceURL)},
	})
}

func EmitWatcherError(d *db.DB, source, title string, body *string, resource Resource) error {
	return d.InsertEvent(db.Event{
		ID:     uuid.New().String(),
		TS:     time.Now().UTC().Format(time.RFC3339),
		Source: source,
		Type:   "watcher_error",
		Title:  title,
		Body:   body,
	}, nil, []db.EventResource{
		{ResourceType: resource.ResourceType, ResourceID: resource.ResourceID, ResourceURL: strPtr(resource.ResourceURL)},
	})
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func OpenLog(watcherName string) *log.Logger {
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".agent-handler", "data", "logs")
	os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, fmt.Sprintf("watcher-%s.log", watcherName))
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return log.Default()
	}
	return log.New(f, "", log.LstdFlags)
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./watcher/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add watcher/framework.go watcher/framework_test.go
git commit --signoff -m "feat: watcher framework — active resources, cursors, dedup, event emission"
```

---

## Task 5: Watcher Scheduler (launchd/cron)

**Files:**
- Create: `watcher/scheduler.go`, `watcher/scheduler_test.go`

- [ ] **Step 1: Implement scheduler.go**

```go
// watcher/scheduler.go
package watcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"
)

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.agent-handler.watcher-{{.Name}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.HandlerPath}}</string>
		<string>watcher</string>
		<string>run</string>
		<string>{{.Name}}</string>
	</array>
	<key>StartInterval</key>
	<integer>{{.IntervalSeconds}}</integer>
	<key>StandardOutPath</key>
	<string>{{.LogPath}}</string>
	<key>StandardErrorPath</key>
	<string>{{.LogPath}}</string>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>`

type SchedulerConfig struct {
	Name            string
	IntervalSeconds int
	HandlerPath     string
	LogPath         string
}

func Install(name string, intervalSeconds int) error {
	handlerPath, err := exec.LookPath("handler")
	if err != nil {
		return fmt.Errorf("handler not found on PATH: %w", err)
	}

	home, _ := os.UserHomeDir()
	logPath := filepath.Join(home, ".agent-handler", "data", "logs", fmt.Sprintf("watcher-%s.log", name))

	cfg := SchedulerConfig{
		Name:            name,
		IntervalSeconds: intervalSeconds,
		HandlerPath:     handlerPath,
		LogPath:         logPath,
	}

	if runtime.GOOS == "darwin" {
		return installLaunchd(cfg)
	}
	return installCron(cfg)
}

func Uninstall(name string) error {
	if runtime.GOOS == "darwin" {
		return uninstallLaunchd(name)
	}
	return uninstallCron(name)
}

func IsInstalled(name string) bool {
	if runtime.GOOS == "darwin" {
		plistPath := launchdPlistPath(name)
		_, err := os.Stat(plistPath)
		return err == nil
	}
	return isCronInstalled(name)
}

func launchdPlistPath(name string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", fmt.Sprintf("com.agent-handler.watcher-%s.plist", name))
}

func installLaunchd(cfg SchedulerConfig) error {
	plistPath := launchdPlistPath(cfg.Name)
	os.MkdirAll(filepath.Dir(plistPath), 0755)

	tmpl, err := template.New("plist").Parse(plistTemplate)
	if err != nil {
		return err
	}

	f, err := os.Create(plistPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := tmpl.Execute(f, cfg); err != nil {
		return err
	}

	// Unload first in case it's already loaded
	exec.Command("launchctl", "unload", plistPath).Run()
	return exec.Command("launchctl", "load", plistPath).Run()
}

func uninstallLaunchd(name string) error {
	plistPath := launchdPlistPath(name)
	exec.Command("launchctl", "unload", plistPath).Run()
	return os.Remove(plistPath)
}

func installCron(cfg SchedulerConfig) error {
	cronLine := fmt.Sprintf("*/%d * * * * %s watcher run %s >> %s 2>&1 # agent-handler-%s",
		max(cfg.IntervalSeconds/60, 1), cfg.HandlerPath, cfg.Name, cfg.LogPath, cfg.Name)

	existing, _ := exec.Command("crontab", "-l").Output()
	lines := strings.Split(string(existing), "\n")

	// Remove existing entry for this watcher
	var kept []string
	for _, line := range lines {
		if !strings.Contains(line, fmt.Sprintf("agent-handler-%s", cfg.Name)) {
			kept = append(kept, line)
		}
	}
	kept = append(kept, cronLine)

	newCrontab := strings.Join(kept, "\n")
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(newCrontab)
	return cmd.Run()
}

func uninstallCron(name string) error {
	existing, _ := exec.Command("crontab", "-l").Output()
	lines := strings.Split(string(existing), "\n")

	var kept []string
	for _, line := range lines {
		if !strings.Contains(line, fmt.Sprintf("agent-handler-%s", name)) {
			kept = append(kept, line)
		}
	}

	newCrontab := strings.Join(kept, "\n")
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(newCrontab)
	return cmd.Run()
}

func isCronInstalled(name string) bool {
	existing, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(existing), fmt.Sprintf("agent-handler-%s", name))
}

func LastRunTime(name string) *time.Time {
	home, _ := os.UserHomeDir()
	logPath := filepath.Join(home, ".agent-handler", "data", "logs", fmt.Sprintf("watcher-%s.log", name))
	info, err := os.Stat(logPath)
	if err != nil {
		return nil
	}
	t := info.ModTime()
	return &t
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
```

- [ ] **Step 2: Commit**

```bash
git add watcher/scheduler.go
git commit --signoff -m "feat: watcher scheduler — launchd plist and cron entry management"
```

---

## Task 6: Watcher CLI Commands (run, install, uninstall, list, logs)

**Files:**
- Create: `cmd/watcher/run.go`, `cmd/watcher/install.go`, `cmd/watcher/uninstall_cmd.go`, `cmd/watcher/list.go`, `cmd/watcher/logs.go`

- [ ] **Step 1: Create cmd/watcher/run.go**

```go
// cmd/watcher/run.go
package watcher

import (
	"fmt"
	"strings"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	watcherPkg "github.com/mturley/agent-handler/watcher"
	"github.com/mturley/agent-handler/watcher/github"
	"github.com/mturley/agent-handler/watcher/jira"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run <name>",
	Short: "Poll active resources for a watcher and write events",
	Args:  cobra.ExactArgs(1),
	RunE:  runWatcher,
}

func init() {
	runCmd.Flags().String("resources", "", "comma-separated list of specific resources to poll")
	WatcherCmd.AddCommand(runCmd)
}

func runWatcher(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, err := config.Read(config.DefaultPath())
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	if !cfg.IsServiceConfigured(name) {
		return fmt.Errorf("%s is not configured. Run 'handler watcher auth %s' to set up API access", name, name)
	}

	d, err := db.Open(db.DefaultPath())
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer d.Close()

	logger := watcherPkg.OpenLog(name)

	// Get resources to poll
	resourceFilter, _ := cmd.Flags().GetString("resources")
	var resources []watcherPkg.Resource

	if resourceFilter != "" {
		// Specific resources requested (catch-up mode)
		for _, r := range strings.Split(resourceFilter, ",") {
			parts := strings.SplitN(strings.TrimSpace(r), ":", 2)
			if len(parts) == 2 {
				resources = append(resources, watcherPkg.Resource{
					ResourceType: parts[0],
					ResourceID:   parts[1],
				})
			}
		}
	} else {
		// All active resources for this watcher type
		resourceType := serviceToResourceType(name)
		resources, err = watcherPkg.ActiveResources(d, resourceType)
		if err != nil {
			return fmt.Errorf("querying active resources: %w", err)
		}
	}

	if len(resources) == 0 {
		logger.Printf("No active resources to poll")
		return nil
	}

	logger.Printf("Polling %d resources", len(resources))

	switch name {
	case "github":
		return github.Poll(d, cfg, resources, logger)
	case "jira":
		return jira.Poll(d, cfg, resources, logger)
	default:
		return fmt.Errorf("unknown watcher: %s", name)
	}
}

func serviceToResourceType(service string) string {
	switch service {
	case "github":
		return "pr"
	case "jira":
		return "jira"
	default:
		return service
	}
}
```

- [ ] **Step 2: Create cmd/watcher/install.go**

```go
// cmd/watcher/install.go
package watcher

import (
	"fmt"
	"time"

	"github.com/mturley/agent-handler/config"
	watcherPkg "github.com/mturley/agent-handler/watcher"
	"github.com/spf13/cobra"
)

var defaultIntervals = map[string]time.Duration{
	"github": 3 * time.Minute,
	"jira":   5 * time.Minute,
}

var installCmd = &cobra.Command{
	Use:   "install <name>",
	Short: "Schedule a watcher to run periodically",
	Args:  cobra.ExactArgs(1),
	RunE:  runInstall,
}

func init() {
	installCmd.Flags().Duration("interval", 0, "polling interval (e.g. 3m, 5m)")
	WatcherCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, err := config.Read(config.DefaultPath())
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	if !cfg.IsServiceConfigured(name) {
		return fmt.Errorf("%s is not configured. Run 'handler watcher auth %s' first", name, name)
	}

	interval, _ := cmd.Flags().GetDuration("interval")
	if interval == 0 {
		interval = defaultIntervals[name]
		if interval == 0 {
			interval = 3 * time.Minute
		}
	}

	fmt.Printf("Installing %s watcher...\n", name)
	fmt.Printf("  Schedule: every %s\n", interval)

	if err := watcherPkg.Install(name, int(interval.Seconds())); err != nil {
		return fmt.Errorf("installing watcher: %w", err)
	}

	fmt.Printf("  ✓ Watcher installed\n\n")
	fmt.Printf("To check status: handler watcher list\n")
	fmt.Printf("To view logs:    handler watcher logs %s\n", name)
	fmt.Printf("To remove:       handler watcher uninstall %s\n", name)

	return nil
}
```

- [ ] **Step 3: Create cmd/watcher/uninstall_cmd.go, list.go, logs.go**

`uninstall_cmd.go` — calls `watcherPkg.Uninstall(name)`, prints confirmation.

`list.go` — checks `IsInstalled` for each known watcher (github, jira), shows interval and last run time.

`logs.go` — tails the log file at `~/.agent-handler/data/logs/watcher-<name>.log`.

Each follows the same pattern as the commands above. The implementing engineer should create them following the established patterns.

- [ ] **Step 4: Build and verify**

```bash
go build ./...
./bin/handler watcher --help
```

- [ ] **Step 5: Commit**

```bash
git add cmd/watcher/
git commit --signoff -m "feat: watcher CLI commands — run, install, uninstall, list, logs"
```

---

## Task 7: GitHub PR Watcher — GraphQL Client

**Files:**
- Create: `watcher/github/graphql.go`

- [ ] **Step 1: Implement the GraphQL query builder and response types**

```go
// watcher/github/graphql.go
package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const graphqlEndpoint = "https://api.github.com/graphql"

type PRData struct {
	Number    int
	Owner     string
	Repo      string
	State     string
	Title     string
	UpdatedAt string
	Reviews   []Review
	Comments  []Comment
	ReviewComments []ReviewComment
	Commits   CommitInfo
	CheckRuns []CheckRun
}

type Review struct {
	Author      string
	AuthorType  string
	State       string
	SubmittedAt string
	Body        string
}

type Comment struct {
	Author     string
	AuthorType string
	CreatedAt  string
	Body       string
}

type ReviewComment struct {
	Author     string
	AuthorType string
	CreatedAt  string
	Path       string
	Body       string
}

type CommitInfo struct {
	TotalCount int
	LatestSHA  string
}

type CheckRun struct {
	Name        string
	Conclusion  string
	CompletedAt string
}

type RateLimit struct {
	Remaining int
	Limit     int
	ResetAt   string
}

func FetchPRs(token string, prs []PRRef) ([]PRData, *RateLimit, error) {
	query := buildBatchQuery(prs)

	reqBody, _ := json.Marshal(map[string]string{"query": query})
	req, err := http.NewRequest("POST", graphqlEndpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	rateLimit := parseRateLimitHeaders(resp)

	if resp.StatusCode != http.StatusOK {
		return nil, rateLimit, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	return parseBatchResponse(body, prs)
}

type PRRef struct {
	Owner  string
	Repo   string
	Number int
}

func ParsePRResourceID(resourceID string) (PRRef, error) {
	// Format: owner/repo#123
	parts := strings.SplitN(resourceID, "#", 2)
	if len(parts) != 2 {
		return PRRef{}, fmt.Errorf("invalid PR resource ID: %s", resourceID)
	}
	repoParts := strings.SplitN(parts[0], "/", 2)
	if len(repoParts) != 2 {
		return PRRef{}, fmt.Errorf("invalid PR resource ID: %s", resourceID)
	}
	var num int
	fmt.Sscanf(parts[1], "%d", &num)
	return PRRef{Owner: repoParts[0], Repo: repoParts[1], Number: num}, nil
}

func buildBatchQuery(prs []PRRef) string {
	var fragments []string
	for i, pr := range prs {
		fragments = append(fragments, fmt.Sprintf(`
			pr%d: repository(owner: "%s", name: "%s") {
				pullRequest(number: %d) {
					number
					state
					title
					updatedAt
					reviews(last: 20) {
						nodes {
							author { login ... on Bot { login } }
							state
							submittedAt
							body
						}
					}
					comments(last: 20) {
						nodes {
							author { login ... on Bot { login } }
							createdAt
							body
						}
					}
					reviewThreads(last: 50) {
						nodes {
							comments(last: 5) {
								nodes {
									author { login ... on Bot { login } }
									createdAt
									path
									body
								}
							}
						}
					}
					commits(last: 1) {
						totalCount
						nodes { commit { oid } }
					}
					statusCheckRollup: commits(last: 1) {
						nodes {
							commit {
								checkSuites(first: 10) {
									nodes {
										checkRuns(first: 50) {
											nodes {
												name
												conclusion
												completedAt
											}
										}
									}
								}
							}
						}
					}
				}
			}
		`, i, pr.Owner, pr.Repo, pr.Number))
	}

	return fmt.Sprintf("{ %s rateLimit { remaining limit resetAt } }", strings.Join(fragments, "\n"))
}

func parseBatchResponse(body []byte, prs []PRRef) ([]PRData, *RateLimit, error) {
	// This is a simplified parser — the implementing engineer should handle
	// the full GraphQL response structure including nested nodes
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil, fmt.Errorf("parsing response: %w", err)
	}

	var dataMap map[string]json.RawMessage
	if d, ok := raw["data"]; ok {
		json.Unmarshal(d, &dataMap)
	}

	var rl *RateLimit
	if rlData, ok := dataMap["rateLimit"]; ok {
		var r RateLimit
		json.Unmarshal(rlData, &r)
		rl = &r
	}

	var results []PRData
	for i, pr := range prs {
		key := fmt.Sprintf("pr%d", i)
		if prJSON, ok := dataMap[key]; ok {
			prData, err := parsePRResponse(prJSON, pr)
			if err != nil {
				continue
			}
			results = append(results, prData)
		}
	}

	return results, rl, nil
}

func parsePRResponse(data json.RawMessage, ref PRRef) (PRData, error) {
	// The implementing engineer needs to fully implement this parser
	// based on the GraphQL response structure
	var repo struct {
		PullRequest struct {
			Number    int    `json:"number"`
			State     string `json:"state"`
			Title     string `json:"title"`
			UpdatedAt string `json:"updatedAt"`
		} `json:"pullRequest"`
	}
	if err := json.Unmarshal(data, &repo); err != nil {
		return PRData{}, err
	}

	return PRData{
		Number:    repo.PullRequest.Number,
		Owner:     ref.Owner,
		Repo:      ref.Repo,
		State:     repo.PullRequest.State,
		Title:     repo.PullRequest.Title,
		UpdatedAt: repo.PullRequest.UpdatedAt,
	}, nil
}

func parseRateLimitHeaders(resp *http.Response) *RateLimit {
	remaining := resp.Header.Get("X-RateLimit-Remaining")
	limit := resp.Header.Get("X-RateLimit-Limit")
	if remaining == "" {
		return nil
	}
	var rl RateLimit
	fmt.Sscanf(remaining, "%d", &rl.Remaining)
	fmt.Sscanf(limit, "%d", &rl.Limit)
	return &rl
}
```

- [ ] **Step 2: Commit**

```bash
git add watcher/github/graphql.go
git commit --signoff -m "feat: GitHub GraphQL client for PR data fetching"
```

---

## Task 8: GitHub PR Watcher — Poller

**Files:**
- Create: `watcher/github/poller.go`, `watcher/github/poller_test.go`

- [ ] **Step 1: Implement the poller**

```go
// watcher/github/poller.go
package github

import (
	"fmt"
	"log"
	"strings"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	watcherPkg "github.com/mturley/agent-handler/watcher"
)

func Poll(d *db.DB, cfg *config.Config, resources []watcherPkg.Resource, logger *log.Logger) error {
	if cfg.Services.GitHub == nil {
		return fmt.Errorf("GitHub not configured")
	}
	token := cfg.Services.GitHub.Token

	// Parse PR refs
	var prRefs []PRRef
	refToResource := make(map[string]watcherPkg.Resource)
	for _, r := range resources {
		ref, err := ParsePRResourceID(r.ResourceID)
		if err != nil {
			logger.Printf("Skipping invalid PR resource: %s: %v", r.ResourceID, err)
			continue
		}
		prRefs = append(prRefs, ref)
		refToResource[r.ResourceID] = r
	}

	if len(prRefs) == 0 {
		return nil
	}

	// Fetch all PRs in one GraphQL request
	prDataList, rateLimit, err := FetchPRs(token, prRefs)
	if err != nil {
		errMsg := err.Error()
		for _, r := range resources {
			watcherPkg.EmitWatcherError(d, "github", fmt.Sprintf("GitHub API error: %s", errMsg), &errMsg, r)
		}
		logger.Printf("GitHub API error: %v", err)
		return err
	}

	if rateLimit != nil {
		logger.Printf("Rate limit: %d/%d remaining", rateLimit.Remaining, rateLimit.Limit)
		if rateLimit.Remaining < 100 {
			logger.Printf("WARNING: Rate limit low!")
		}
	}

	eventsWritten := 0
	for _, pr := range prDataList {
		resourceID := fmt.Sprintf("%s/%s#%d", pr.Owner, pr.Repo, pr.Number)
		resource := refToResource[resourceID]
		cursor := watcherPkg.EventCursor(d, "github", "pr", resourceID)

		isFirstPoll := cursor == ""
		if isFirstPoll {
			// Emit watch_started summary
			summary := fmt.Sprintf("State: %s, Title: %s", pr.State, pr.Title)
			watcherPkg.EmitWatcherEvent(d, "github", "watch_started",
				fmt.Sprintf("Now watching PR %s — %s", resourceID, pr.State),
				&summary, pr.UpdatedAt, nil, nil, resource)
			eventsWritten++
			continue
		}

		// Process reviews
		for _, review := range pr.Reviews {
			if review.SubmittedAt <= cursor {
				continue
			}
			if watcherPkg.IsDuplicate(d, "github", "pr", resourceID, reviewEventType(review.State), review.SubmittedAt) {
				continue
			}
			author := review.Author
			authorType := review.AuthorType
			watcherPkg.EmitWatcherEvent(d, "github", reviewEventType(review.State),
				fmt.Sprintf("%s %s on %s", review.Author, reviewAction(review.State), resourceID),
				&review.Body, review.SubmittedAt, &author, &authorType, resource)
			eventsWritten++
		}

		// Process comments
		for _, comment := range pr.Comments {
			if comment.CreatedAt <= cursor {
				continue
			}
			if watcherPkg.IsDuplicate(d, "github", "pr", resourceID, "pr_comment", comment.CreatedAt) {
				continue
			}
			author := comment.Author
			authorType := comment.AuthorType
			bodyPreview := truncate(comment.Body, 200)
			watcherPkg.EmitWatcherEvent(d, "github", "pr_comment",
				fmt.Sprintf("Comment by %s on %s", comment.Author, resourceID),
				&bodyPreview, comment.CreatedAt, &author, &authorType, resource)
			eventsWritten++
		}

		// Process review comments
		for _, rc := range pr.ReviewComments {
			if rc.CreatedAt <= cursor {
				continue
			}
			if watcherPkg.IsDuplicate(d, "github", "pr", resourceID, "pr_review_comment", rc.CreatedAt) {
				continue
			}
			author := rc.Author
			authorType := rc.AuthorType
			bodyPreview := fmt.Sprintf("%s: %s", rc.Path, truncate(rc.Body, 150))
			watcherPkg.EmitWatcherEvent(d, "github", "pr_review_comment",
				fmt.Sprintf("Review comment by %s on %s", rc.Author, resourceID),
				&bodyPreview, rc.CreatedAt, &author, &authorType, resource)
			eventsWritten++
		}

		// Process CI check runs
		for _, check := range pr.CheckRuns {
			if check.CompletedAt == "" || check.CompletedAt <= cursor {
				continue
			}
			eventType := "ci_check_passed"
			if check.Conclusion == "FAILURE" || check.Conclusion == "ERROR" {
				eventType = "ci_check_failed"
			} else if check.Conclusion != "SUCCESS" {
				continue
			}
			if watcherPkg.IsDuplicate(d, "github", "pr", resourceID, eventType, check.CompletedAt) {
				continue
			}
			watcherPkg.EmitWatcherEvent(d, "github", eventType,
				fmt.Sprintf("CI: %s %s", check.Name, strings.ToLower(check.Conclusion)),
				nil, check.CompletedAt, nil, nil, resource)
			eventsWritten++
		}

		// Terminal state handling
		state := strings.ToUpper(pr.State)
		if state == "MERGED" || state == "CLOSED" {
			eventType := "pr_merged"
			if state == "CLOSED" {
				eventType = "pr_closed"
			}
			if !watcherPkg.IsDuplicate(d, "github", "pr", resourceID, eventType, pr.UpdatedAt) {
				watcherPkg.EmitWatcherEvent(d, "github", eventType,
					fmt.Sprintf("PR %s %s", resourceID, strings.ToLower(state)),
					nil, pr.UpdatedAt, nil, nil, resource)
				// Soft-delete subscriptions
				d.Conn().Exec(`UPDATE subscriptions SET deleted_at = datetime('now') WHERE resource_type = 'pr' AND resource_id = ? AND deleted_at IS NULL`, resourceID)
				eventsWritten++
			}
		}
	}

	logger.Printf("Wrote %d events for %d PRs", eventsWritten, len(prDataList))
	return nil
}

func reviewEventType(state string) string {
	switch strings.ToUpper(state) {
	case "APPROVED":
		return "pr_approved"
	case "CHANGES_REQUESTED":
		return "pr_review_comment"
	default:
		return "pr_comment"
	}
}

func reviewAction(state string) string {
	switch strings.ToUpper(state) {
	case "APPROVED":
		return "approved"
	case "CHANGES_REQUESTED":
		return "requested changes"
	default:
		return "reviewed"
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
```

- [ ] **Step 2: Write basic tests with mock HTTP server**

The implementing engineer should write tests that:
- Create a mock HTTP server returning sample GraphQL responses
- Verify events are written to the DB
- Verify deduplication works
- Verify terminal state handling (merge → subscription deleted)

- [ ] **Step 3: Commit**

```bash
git add watcher/github/
git commit --signoff -m "feat: GitHub PR watcher — GraphQL polling with event emission"
```

---

## Task 9: Jira Watcher — Client and Poller

**Files:**
- Create: `watcher/jira/client.go`, `watcher/jira/poller.go`, `watcher/jira/poller_test.go`

- [ ] **Step 1: Implement the Jira REST client**

```go
// watcher/jira/client.go
package jira

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Client struct {
	BaseURL string
	Email   string
	Token   string
}

type IssueData struct {
	Key         string
	Summary     string
	Status      string
	Assignee    string
	EpicKey     string
	EpicURL     string
	Labels      []string
	Comments    []IssueComment
	Changelog   []ChangelogEntry
}

type IssueComment struct {
	Author    string
	CreatedAt string
	Body      string
}

type ChangelogEntry struct {
	Author    string
	CreatedAt string
	Field     string
	From      string
	To        string
}

func (c *Client) FetchIssue(issueKey string) (*IssueData, error) {
	url := fmt.Sprintf("%s/rest/api/3/issue/%s?expand=changelog&fields=summary,status,assignee,labels,comment,parent,customfield_12311140", c.BaseURL, issueKey)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.Email, c.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Jira API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Jira API returned %d: %s", resp.StatusCode, string(body))
	}

	return parseIssueResponse(body, issueKey)
}

func parseIssueResponse(body []byte, issueKey string) (*IssueData, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	issue := &IssueData{Key: issueKey}

	// Parse fields
	var fields map[string]json.RawMessage
	json.Unmarshal(raw["fields"], &fields)

	// Summary
	json.Unmarshal(fields["summary"], &issue.Summary)

	// Status
	var status struct {
		Name string `json:"name"`
	}
	json.Unmarshal(fields["status"], &status)
	issue.Status = status.Name

	// Assignee
	var assignee struct {
		DisplayName string `json:"displayName"`
	}
	if err := json.Unmarshal(fields["assignee"], &assignee); err == nil {
		issue.Assignee = assignee.DisplayName
	}

	// Labels
	json.Unmarshal(fields["labels"], &issue.Labels)

	// Epic link (customfield varies by instance — this uses a common one)
	var epicKey string
	if epicField, ok := fields["customfield_12311140"]; ok {
		json.Unmarshal(epicField, &epicKey)
		issue.EpicKey = epicKey
	}

	// Comments
	var commentWrapper struct {
		Comments []struct {
			Author struct {
				DisplayName string `json:"displayName"`
			} `json:"author"`
			Created string `json:"created"`
			Body    json.RawMessage `json:"body"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(fields["comment"], &commentWrapper); err == nil {
		for _, c := range commentWrapper.Comments {
			issue.Comments = append(issue.Comments, IssueComment{
				Author:    c.Author.DisplayName,
				CreatedAt: c.Created,
				Body:      string(c.Body),
			})
		}
	}

	// Changelog
	var changelog struct {
		Histories []struct {
			Author struct {
				DisplayName string `json:"displayName"`
			} `json:"author"`
			Created string `json:"created"`
			Items   []struct {
				Field      string `json:"field"`
				FromString string `json:"fromString"`
				ToString   string `json:"toString"`
			} `json:"items"`
		} `json:"histories"`
	}
	if err := json.Unmarshal(raw["changelog"], &changelog); err == nil {
		for _, h := range changelog.Histories {
			for _, item := range h.Items {
				issue.Changelog = append(issue.Changelog, ChangelogEntry{
					Author:    h.Author.DisplayName,
					CreatedAt: h.Created,
					Field:     item.Field,
					From:      item.FromString,
					To:        item.ToString,
				})
			}
		}
	}

	return issue, nil
}
```

- [ ] **Step 2: Implement the Jira poller**

```go
// watcher/jira/poller.go
package jira

import (
	"fmt"
	"log"
	"strings"

	"github.com/mturley/agent-handler/config"
	"github.com/mturley/agent-handler/db"
	watcherPkg "github.com/mturley/agent-handler/watcher"
)

var terminalStatuses = map[string]bool{
	"Done": true, "Resolved": true, "Won't Fix": true, "Closed": true,
	"Won't Do": true, "Cancelled": true,
}

func Poll(d *db.DB, cfg *config.Config, resources []watcherPkg.Resource, logger *log.Logger) error {
	if cfg.Services.Jira == nil {
		return fmt.Errorf("Jira not configured")
	}

	client := &Client{
		BaseURL: cfg.Services.Jira.URL,
		Email:   cfg.Services.Jira.Email,
		Token:   cfg.Services.Jira.Token,
	}

	botUsernames := make(map[string]bool)
	for _, u := range cfg.Services.Jira.BotUsernames {
		botUsernames[u] = true
	}

	eventsWritten := 0
	for _, resource := range resources {
		issueKey := resource.ResourceID

		issue, err := client.FetchIssue(issueKey)
		if err != nil {
			errMsg := err.Error()
			watcherPkg.EmitWatcherError(d, "jira", fmt.Sprintf("Jira API error for %s", issueKey), &errMsg, resource)
			logger.Printf("Error fetching %s: %v", issueKey, err)
			continue
		}

		cursor := watcherPkg.EventCursor(d, "jira", "jira", issueKey)
		isFirstPoll := cursor == ""

		if isFirstPoll {
			summary := fmt.Sprintf("Status: %s, Assignee: %s, Summary: %s", issue.Status, issue.Assignee, issue.Summary)
			watcherPkg.EmitWatcherEvent(d, "jira", "watch_started",
				fmt.Sprintf("Now watching %s — %s", issueKey, issue.Status),
				&summary, issue.Comments[len(issue.Comments)-1].CreatedAt if len(issue.Comments) > 0 else time.Now().UTC().Format(time.RFC3339),
				nil, nil, resource)
			eventsWritten++

			// Write epic relationship if present
			if issue.EpicKey != "" {
				writeEpicRelationship(d, issueKey, issue.EpicKey, resource.ResourceURL, cfg.Services.Jira.URL)
			}
			continue
		}

		// Process new comments
		for _, comment := range issue.Comments {
			if comment.CreatedAt <= cursor {
				continue
			}
			if watcherPkg.IsDuplicate(d, "jira", "jira", issueKey, "jira_comment", comment.CreatedAt) {
				continue
			}
			author := comment.Author
			authorType := authorTypeForUser(author, botUsernames)
			bodyPreview := truncate(comment.Body, 200)
			watcherPkg.EmitWatcherEvent(d, "jira", "jira_comment",
				fmt.Sprintf("Comment by %s on %s", author, issueKey),
				&bodyPreview, comment.CreatedAt, &author, &authorType, resource)
			eventsWritten++
		}

		// Process changelog
		for _, entry := range issue.Changelog {
			if entry.CreatedAt <= cursor {
				continue
			}

			var eventType, title string
			switch entry.Field {
			case "status":
				eventType = "jira_status_change"
				title = fmt.Sprintf("%s: %s → %s", issueKey, entry.From, entry.To)
			case "assignee":
				eventType = "jira_assigned"
				title = fmt.Sprintf("%s assigned to %s", issueKey, entry.To)
			case "description":
				eventType = "jira_description_changed"
				title = fmt.Sprintf("Description updated on %s", issueKey)
			case "labels":
				eventType = "jira_labels_changed"
				title = formatLabelChange(issueKey, entry.From, entry.To)
			default:
				continue
			}

			if watcherPkg.IsDuplicate(d, "jira", "jira", issueKey, eventType, entry.CreatedAt) {
				continue
			}

			author := entry.Author
			authorType := authorTypeForUser(author, botUsernames)
			detail := fmt.Sprintf("%s: %s → %s", entry.Field, entry.From, entry.To)
			watcherPkg.EmitWatcherEvent(d, "jira", eventType, title, &detail, entry.CreatedAt, &author, &authorType, resource)
			eventsWritten++

			// Terminal state handling
			if entry.Field == "status" && terminalStatuses[entry.To] {
				d.Conn().Exec(`UPDATE subscriptions SET deleted_at = datetime('now') WHERE resource_type = 'jira' AND resource_id = ? AND deleted_at IS NULL`, issueKey)
			}
		}

		// Update epic relationship
		if issue.EpicKey != "" {
			writeEpicRelationship(d, issueKey, issue.EpicKey, resource.ResourceURL, cfg.Services.Jira.URL)
		}
	}

	logger.Printf("Wrote %d events for %d issues", eventsWritten, len(resources))
	return nil
}

func writeEpicRelationship(d *db.DB, childKey, parentKey, childURL, jiraBaseURL string) {
	// Check if relationship already exists
	var count int
	d.Conn().QueryRow(`SELECT COUNT(*) FROM resource_relationships WHERE child_type = 'jira' AND child_id = ? AND parent_type = 'jira' AND parent_id = ?`,
		childKey, parentKey).Scan(&count)
	if count > 0 {
		return
	}

	parentURL := fmt.Sprintf("%s/browse/%s", jiraBaseURL, parentKey)
	d.LinkResources(db.ResourceRelationship{
		ID:           uuid.New().String(),
		ChildType:    "jira",
		ChildID:      childKey,
		ChildURL:     &childURL,
		ParentType:   "jira",
		ParentID:     parentKey,
		ParentURL:    &parentURL,
		Relationship: "epic_child",
		Source:       "watcher",
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	})
}

func authorTypeForUser(username string, botUsernames map[string]bool) string {
	if botUsernames[username] {
		return "bot"
	}
	return "human"
}

func formatLabelChange(issueKey, from, to string) string {
	oldLabels := strings.Split(from, " ")
	newLabels := strings.Split(to, " ")

	oldSet := make(map[string]bool)
	for _, l := range oldLabels {
		if l != "" {
			oldSet[l] = true
		}
	}
	newSet := make(map[string]bool)
	for _, l := range newLabels {
		if l != "" {
			newSet[l] = true
		}
	}

	var added, removed []string
	for l := range newSet {
		if !oldSet[l] {
			added = append(added, "+"+l)
		}
	}
	for l := range oldSet {
		if !newSet[l] {
			removed = append(removed, "-"+l)
		}
	}

	changes := append(added, removed...)
	return fmt.Sprintf("Labels changed on %s: %s", issueKey, strings.Join(changes, ", "))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
```

Note: The `isFirstPoll` watch_started timestamp and the missing imports (`time`, `uuid`) need to be fixed by the implementing engineer. The Go syntax for the conditional timestamp expression is invalid and should use a proper if/else.

- [ ] **Step 3: Write tests with mock HTTP server**

Tests should verify:
- Comments after cursor are emitted, before cursor are skipped
- Status changes emit `jira_status_change` events
- Terminal statuses soft-delete subscriptions
- Epic relationships are created
- Bot username detection works
- Deduplication works

- [ ] **Step 4: Commit**

```bash
git add watcher/jira/
git commit --signoff -m "feat: Jira watcher — REST polling with changelog-based change detection"
```

---

## Task 10: Subscribe Guard

**Files:**
- Modify: `cmd/subscribe.go`

- [ ] **Step 1: Add auth guard to subscribe command**

After parsing the resource type, before subscribing:

```go
import "github.com/mturley/agent-handler/config"

// After parsing resourceType:
service := config.ResourceTypeToService(resourceType)
if service != "" {
    cfg, err := config.Read(config.DefaultPath())
    if err != nil {
        return fmt.Errorf("reading config: %w", err)
    }
    if !cfg.IsServiceConfigured(service) {
        return fmt.Errorf("%s is not configured. Run 'handler watcher auth %s' to set up API access", service, service)
    }
}
```

- [ ] **Step 2: Build and test**

```bash
go build ./...
# Test: try subscribing without auth configured
./bin/handler subscribe --resource "pr:test/repo#1" --session-id test
```

- [ ] **Step 3: Commit**

```bash
git add cmd/subscribe.go
git commit --signoff -m "feat: subscribe guard — blocks subscription to unconfigured services"
```

---

## Task 11: Catch-up on Re-registration

**Files:**
- Modify: `cmd/register.go`

- [ ] **Step 1: After registration and cursor initialization, spawn background watcher runs**

Add after the `.worktree-resources` subscription loop:

```go
// Spawn background catch-up watcher runs for subscribed resources
subs, _ := d.ListSubscriptions(regSessionID, false)
if len(subs) > 0 {
    resourcesByType := make(map[string][]string)
    for _, sub := range subs {
        service := config.ResourceTypeToService(sub.ResourceType)
        if service != "" {
            resourcesByType[service] = append(resourcesByType[service],
                sub.ResourceType+":"+sub.ResourceID)
        }
    }
    for service, resources := range resourcesByType {
        resourceList := strings.Join(resources, ",")
        go func(svc, rl string) {
            exec.Command("handler", "watcher", "run", svc, "--resources", rl).Run()
        }(service, resourceList)
    }
}
```

- [ ] **Step 2: Commit**

```bash
git add cmd/register.go
git commit --signoff -m "feat: background catch-up watcher run on session re-registration"
```

---

## Task 12: Setup and Uninstall Integration

**Files:**
- Modify: `cmd/setup.go`, `cmd/uninstall.go`

- [ ] **Step 1: Add watcher auth to setup flow**

After the permissions step in `cmd/setup.go`, add:

```go
// 8. Run watcher auth for external services
fmt.Println("\nConfiguring external service API tokens...")
watcherAuthCmd := exec.Command("handler", "watcher", "auth")
watcherAuthCmd.Stdin = os.Stdin
watcherAuthCmd.Stdout = os.Stdout
watcherAuthCmd.Stderr = os.Stderr
watcherAuthCmd.Run()
```

- [ ] **Step 2: Add watcher uninstall to uninstall flow**

In `cmd/uninstall.go`, before removing hooks, add:

```go
// Uninstall any active watchers
for _, name := range []string{"github", "jira"} {
    if watcherPkg.IsInstalled(name) {
        watcherPkg.Uninstall(name)
        fmt.Printf("  ✓ Uninstalled %s watcher\n", name)
    }
}
```

Add the uninstall summary:
```go
for _, name := range []string{"github", "jira"} {
    if watcherPkg.IsInstalled(name) {
        fmt.Printf("  Uninstall %s watcher schedule\n", name)
    }
}
```

- [ ] **Step 3: Commit**

```bash
git add cmd/setup.go cmd/uninstall.go
git commit --signoff -m "feat: integrate watcher auth into setup, watcher uninstall into uninstall"
```

---

## Task 13: Integration Smoke Test

**Files:** None created — manual verification.

- [ ] **Step 1: Build**

```bash
make build
make install
```

- [ ] **Step 2: Configure auth**

```bash
handler watcher auth github
handler watcher auth jira
handler watcher auth status
```

- [ ] **Step 3: Install watchers**

```bash
handler watcher install github
handler watcher install jira
handler watcher list
```

- [ ] **Step 4: Test GitHub watcher manually**

```bash
# Subscribe to a real PR
handler subscribe --resource "pr:mturley/agent-handler#1" --url "https://github.com/mturley/agent-handler/pull/1"

# Run watcher once
handler watcher run github

# Check events
handler tail --source github
```

- [ ] **Step 5: Test Jira watcher manually**

```bash
# Subscribe to a real Jira issue
handler subscribe --resource "jira:RHOAIENG-12345" --url "https://redhat.atlassian.net/browse/RHOAIENG-12345"

# Run watcher once
handler watcher run jira

# Check events
handler tail --source jira
```

- [ ] **Step 6: Check watcher logs**

```bash
handler watcher logs github
handler watcher logs jira
```

- [ ] **Step 7: Verify catch-up on re-registration**

```bash
# Start a new session, subscribe to a PR, end session, start new session
# The registration should trigger a background watcher run
handler status
```

- [ ] **Step 8: Commit any fixes**

```bash
git add -A
git commit --signoff -m "fix: integration test fixes from Phase 2 smoke testing"
```

---

## Task 14: Update CLAUDE.md and README

**Files:**
- Modify: `CLAUDE.md`, `README.md`

- [ ] **Step 1: Update CLAUDE.md with watcher info**

Add watcher-related conventions:
- Config file location and format
- When adding new watcher types, what to update
- The resource type to service mapping

- [ ] **Step 2: Update README.md**

Add a "Watchers" section covering:
- How to configure (`handler watcher auth`)
- How to install watchers
- How to check status and logs

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md README.md
git commit --signoff -m "docs: update CLAUDE.md and README with Phase 2 watcher documentation"
```

---

## Task 15: Statusline Watcher Info (Phase 2b)

**Files:**
- Modify: `cmd/statusline.go`, `hooks/statusline.sh`

- [ ] **Step 1: Update handler statusline to include subscription and watcher info**

After the inbox mode line, add a third line showing:
- Number of active subscriptions by type (e.g. "2 PRs, 1 Jira")
- Watcher status for configured services (e.g. "GitHub: watching, Jira: watching")
- Or "No active subscriptions" if none

Example:
```
/inbox: 2 unread (1 pr_comment, 1 ci_check_failed)
/inbox_mode: manual | on-submit | auto
← 2 PRs, 1 Jira | watchers: github ✓ jira ✓
```

- [ ] **Step 2: Test statusline output**

```bash
handler statusline --session <id>
```

- [ ] **Step 3: Commit**

```bash
git add cmd/statusline.go hooks/statusline.sh
git commit --signoff -m "feat: statusline shows active subscriptions and watcher status"
```
