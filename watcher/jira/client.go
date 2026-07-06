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
func (c *Client) FetchIssue(issueKey string, customFieldIDs map[string]string) (*IssueData, error) {
	fields := "summary,status,assignee,labels,comment,priority,issuetype,created,updated"
	for _, fieldID := range customFieldIDs {
		fields += "," + fieldID
	}
	url := fmt.Sprintf("%s/rest/api/3/issue/%s?expand=changelog&fields=%s", c.BaseURL, issueKey, fields)

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

	// Buffer response body for dual decode (typed struct + raw map)
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

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
			Labels  []string `json:"labels"`
			Created string   `json:"created"`
			Updated string   `json:"updated"`
			Comment struct {
				Comments []struct {
					Author struct {
						DisplayName string `json:"displayName"`
					} `json:"author"`
					Created string      `json:"created"`
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

	// Build IssueData
	issue := &IssueData{
		Key:          raw.Key,
		Summary:      raw.Fields.Summary,
		Status:       raw.Fields.Status.Name,
		Priority:     raw.Fields.Priority.Name,
		IssueType:    raw.Fields.IssueType.Name,
		Labels:       raw.Fields.Labels,
		CreatedAt:    raw.Fields.Created,
		UpdatedAt:    raw.Fields.Updated,
		CustomFields: make(map[string]interface{}),
	}

	if raw.Fields.Assignee != nil {
		issue.Assignee = &raw.Fields.Assignee.DisplayName
	}

	// Extract custom fields
	for displayName, fieldID := range customFieldIDs {
		if rawVal, ok := fieldsMap[fieldID]; ok {
			issue.CustomFields[displayName] = extractFieldValue(rawVal)
		}
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
