package config

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// ValidateGitHubToken validates a GitHub personal access token
// Returns the authenticated user's login on success
func ValidateGitHubToken(token string, apiURL ...string) (username string, err error) {
	endpoint := "https://api.github.com/graphql"
	if len(apiURL) > 0 && apiURL[0] != "" {
		endpoint = apiURL[0]
	}

	// GraphQL query to get viewer login
	query := map[string]interface{}{
		"query": "{ viewer { login } }",
	}

	body, err := json.Marshal(query)
	if err != nil {
		return "", fmt.Errorf("failed to marshal query: %w", err)
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return "", fmt.Errorf("invalid GitHub token: authentication failed")
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub API error: status %d", resp.StatusCode)
	}

	// Parse response
	var result struct {
		Data struct {
			Viewer struct {
				Login string `json:"login"`
			} `json:"viewer"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Errors) > 0 {
		return "", fmt.Errorf("GraphQL error: %s", result.Errors[0].Message)
	}

	if result.Data.Viewer.Login == "" {
		return "", fmt.Errorf("empty login in response")
	}

	return result.Data.Viewer.Login, nil
}

// ValidateJiraToken validates Jira API credentials
// Returns the authenticated user's display name on success
func ValidateJiraToken(baseURL, email, token string) (displayName string, err error) {
	endpoint := baseURL + "/rest/api/3/myself"

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Basic auth with email:token
	auth := email + ":" + token
	encodedAuth := base64.StdEncoding.EncodeToString([]byte(auth))
	req.Header.Set("Authorization", "Basic "+encodedAuth)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return "", fmt.Errorf("invalid Jira credentials: authentication failed")
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Jira API error: status %d", resp.StatusCode)
	}

	// Parse response
	var result struct {
		DisplayName string `json:"displayName"`
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if result.DisplayName == "" {
		return "", fmt.Errorf("empty displayName in response")
	}

	return result.DisplayName, nil
}
