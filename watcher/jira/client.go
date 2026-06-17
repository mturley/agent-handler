package jira

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is a REST client for Jira API v3.
type Client struct {
	BaseURL string
	Email   string
	Token   string
}

// IssueData represents Jira issue data with changelog and comments.
type IssueData struct {
	Key       string
	Summary   string
	Status    string
	Assignee  *string
	EpicKey   *string
	Labels    []string
	Comments  []IssueComment
	Changelog []ChangelogEntry
}

// IssueComment represents a Jira issue comment.
type IssueComment struct {
	Author    string
	CreatedAt string
	Body      string // Summary text, not full ADF
}

// ChangelogEntry represents a single Jira changelog item.
type ChangelogEntry struct {
	Author    string
	CreatedAt string
	Field     string
	From      string
	To        string
}

// FetchIssue fetches issue data from Jira API v3.
func (c *Client) FetchIssue(issueKey string) (*IssueData, error) {
	url := fmt.Sprintf("%s/rest/api/3/issue/%s?expand=changelog&fields=summary,status,assignee,labels,comment,customfield_12311140", c.BaseURL, issueKey)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(c.Email, c.Token)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var raw struct {
		Key    string `json:"key"`
		Fields struct {
			Summary  string `json:"summary"`
			Status   struct {
				Name string `json:"name"`
			} `json:"status"`
			Assignee *struct {
				DisplayName string `json:"displayName"`
			} `json:"assignee"`
			Labels    []string `json:"labels"`
			EpicLink  *string  `json:"customfield_12311140"` // Red Hat's epic link field
			Comment   struct {
				Comments []struct {
					Author struct {
						DisplayName string `json:"displayName"`
					} `json:"author"`
					Created string `json:"created"`
					Body    interface{} `json:"body"` // ADF JSON
				} `json:"comments"`
			} `json:"comment"`
		} `json:"fields"`
		Changelog struct {
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
		} `json:"changelog"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Build IssueData
	issue := &IssueData{
		Key:     raw.Key,
		Summary: raw.Fields.Summary,
		Status:  raw.Fields.Status.Name,
		Labels:  raw.Fields.Labels,
	}

	if raw.Fields.Assignee != nil {
		issue.Assignee = &raw.Fields.Assignee.DisplayName
	}

	if raw.Fields.EpicLink != nil {
		issue.EpicKey = raw.Fields.EpicLink
	}

	// Parse comments
	for _, c := range raw.Fields.Comment.Comments {
		// For ADF bodies, just use a summary
		body := fmt.Sprintf("Comment by %s on %s", c.Author.DisplayName, c.Created[:10])
		issue.Comments = append(issue.Comments, IssueComment{
			Author:    c.Author.DisplayName,
			CreatedAt: c.Created,
			Body:      body,
		})
	}

	// Parse changelog
	for _, history := range raw.Changelog.Histories {
		for _, item := range history.Items {
			issue.Changelog = append(issue.Changelog, ChangelogEntry{
				Author:    history.Author.DisplayName,
				CreatedAt: history.Created,
				Field:     item.Field,
				From:      item.FromString,
				To:        item.ToString,
			})
		}
	}

	return issue, nil
}
