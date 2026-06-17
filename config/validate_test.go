package config

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateGitHubToken(t *testing.T) {
	tests := []struct {
		name          string
		token         string
		statusCode    int
		responseBody  string
		expectedLogin string
		expectError   bool
		errorContains string
	}{
		{
			name:       "valid token",
			token:      "ghp_valid",
			statusCode: 200,
			responseBody: `{
				"data": {
					"viewer": {
						"login": "testuser"
					}
				}
			}`,
			expectedLogin: "testuser",
			expectError:   false,
		},
		{
			name:          "invalid token - 401",
			token:         "ghp_invalid",
			statusCode:    401,
			responseBody:  `{"message": "Bad credentials"}`,
			expectError:   true,
			errorContains: "invalid GitHub token",
		},
		{
			name:       "GraphQL error",
			token:      "ghp_error",
			statusCode: 200,
			responseBody: `{
				"errors": [
					{"message": "Something went wrong"}
				]
			}`,
			expectError:   true,
			errorContains: "GraphQL error",
		},
		{
			name:       "empty login",
			token:      "ghp_empty",
			statusCode: 200,
			responseBody: `{
				"data": {
					"viewer": {
						"login": ""
					}
				}
			}`,
			expectError:   true,
			errorContains: "empty login",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request method and headers
				if r.Method != "POST" {
					t.Errorf("Expected POST request, got %s", r.Method)
				}

				authHeader := r.Header.Get("Authorization")
				expectedAuth := "Bearer " + tt.token
				if authHeader != expectedAuth {
					t.Errorf("Expected Authorization header '%s', got '%s'", expectedAuth, authHeader)
				}

				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			// Call ValidateGitHubToken with mock server URL
			login, err := ValidateGitHubToken(tt.token, server.URL)

			if tt.expectError {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if login != tt.expectedLogin {
					t.Errorf("Expected login '%s', got '%s'", tt.expectedLogin, login)
				}
			}
		})
	}
}

func TestValidateJiraToken(t *testing.T) {
	tests := []struct {
		name              string
		email             string
		token             string
		statusCode        int
		responseBody      string
		expectedDisplay   string
		expectError       bool
		errorContains     string
		checkAuthHeader   bool
		expectedAuthValue string
	}{
		{
			name:       "valid credentials",
			email:      "test@example.com",
			token:      "jira_token_123",
			statusCode: 200,
			responseBody: `{
				"displayName": "Test User"
			}`,
			expectedDisplay: "Test User",
			expectError:     false,
		},
		{
			name:          "invalid credentials - 401",
			email:         "test@example.com",
			token:         "bad_token",
			statusCode:    401,
			responseBody:  `{"errorMessages": ["Invalid credentials"]}`,
			expectError:   true,
			errorContains: "invalid Jira credentials",
		},
		{
			name:          "server error - 500",
			email:         "test@example.com",
			token:         "token",
			statusCode:    500,
			responseBody:  `{"errorMessages": ["Internal server error"]}`,
			expectError:   true,
			errorContains: "Jira API error: status 500",
		},
		{
			name:       "empty displayName",
			email:      "test@example.com",
			token:      "token",
			statusCode: 200,
			responseBody: `{
				"displayName": ""
			}`,
			expectError:   true,
			errorContains: "empty displayName",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request method and path
				if r.Method != "GET" {
					t.Errorf("Expected GET request, got %s", r.Method)
				}
				if !strings.HasSuffix(r.URL.Path, "/rest/api/3/myself") {
					t.Errorf("Expected path ending with /rest/api/3/myself, got %s", r.URL.Path)
				}

				// Verify basic auth header
				authHeader := r.Header.Get("Authorization")
				if !strings.HasPrefix(authHeader, "Basic ") {
					t.Errorf("Expected Basic auth header, got '%s'", authHeader)
				}

				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			// Call ValidateJiraToken with mock server URL
			displayName, err := ValidateJiraToken(server.URL, tt.email, tt.token)

			if tt.expectError {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if displayName != tt.expectedDisplay {
					t.Errorf("Expected displayName '%s', got '%s'", tt.expectedDisplay, displayName)
				}
			}
		})
	}
}

func TestValidateGitHubTokenRequestBody(t *testing.T) {
	// Verify that the request body contains the correct GraphQL query
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}

		query, ok := body["query"].(string)
		if !ok {
			t.Fatal("Expected query field in request body")
		}

		if !strings.Contains(query, "viewer") || !strings.Contains(query, "login") {
			t.Errorf("Expected query to contain viewer and login, got: %s", query)
		}

		w.WriteHeader(200)
		w.Write([]byte(`{"data":{"viewer":{"login":"test"}}}`))
	}))
	defer server.Close()

	_, err := ValidateGitHubToken("test_token", server.URL)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}
