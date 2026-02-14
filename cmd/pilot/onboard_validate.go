package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/alekspetrov/pilot/internal/adapters/asana"
	"github.com/alekspetrov/pilot/internal/adapters/azuredevops"
	"github.com/alekspetrov/pilot/internal/adapters/github"
	"github.com/alekspetrov/pilot/internal/adapters/gitlab"
	"github.com/alekspetrov/pilot/internal/adapters/jira"
	"github.com/alekspetrov/pilot/internal/adapters/linear"
)

const validationTimeout = 5 * time.Second

// validateGitHubConn tests GitHub token by fetching repository info.
// Returns authenticated username on success.
func validateGitHubConn(ctx context.Context, token, owner, repo string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, validationTimeout)
	defer cancel()

	client := github.NewClient(token)
	repository, err := client.GetRepository(ctx, owner, repo)
	if err != nil {
		return "", formatConnectionError(err)
	}

	return repository.Owner.Login, nil
}

// validateLinearConn tests Linear API key by querying the viewer.
// Returns the viewer name on success.
func validateLinearConn(ctx context.Context, apiKey string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, validationTimeout)
	defer cancel()

	client := linear.NewClient(apiKey)

	query := `query { viewer { id name } }`
	var result struct {
		Viewer struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"viewer"`
	}

	if err := client.Execute(ctx, query, nil, &result); err != nil {
		return "", formatConnectionError(err)
	}

	if result.Viewer.Name == "" {
		return result.Viewer.ID, nil
	}
	return result.Viewer.Name, nil
}

// validateSlackConn tests Slack bot token via auth.test API.
// Returns bot name on success.
func validateSlackConn(ctx context.Context, botToken string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, validationTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://slack.com/api/auth.test", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+botToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: validationTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", formatConnectionError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		User  string `json:"user"`
		BotID string `json:"bot_id"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if !result.OK {
		return "", fmt.Errorf("authentication failed: %s", result.Error)
	}

	if result.User != "" {
		return result.User, nil
	}
	return result.BotID, nil
}

// validateJiraConn tests Jira credentials by fetching a project.
// projectKey is used to validate access; if empty, validation only checks auth.
func validateJiraConn(ctx context.Context, baseURL, username, apiToken, platform, projectKey string) error {
	ctx, cancel := context.WithTimeout(ctx, validationTimeout)
	defer cancel()

	client := jira.NewClient(baseURL, username, apiToken, platform)

	// If a project key is provided, try to fetch it to validate access
	if projectKey != "" {
		_, err := client.GetProject(ctx, projectKey)
		if err != nil {
			return formatConnectionError(err)
		}
		return nil
	}

	// Without a project key, search for any accessible issue to validate auth
	issues, err := client.SearchIssues(ctx, "order by created DESC", 1)
	if err != nil {
		return formatConnectionError(err)
	}

	// No error means auth succeeded (even if no issues found)
	_ = issues
	return nil
}

// validateGitLabConn tests GitLab token by fetching project info.
func validateGitLabConn(ctx context.Context, token, baseURL, project string) error {
	ctx, cancel := context.WithTimeout(ctx, validationTimeout)
	defer cancel()

	var client *gitlab.Client
	if baseURL != "" && baseURL != "https://gitlab.com" {
		client = gitlab.NewClientWithBaseURL(token, project, baseURL)
	} else {
		client = gitlab.NewClient(token, project)
	}

	_, err := client.GetProject(ctx)
	if err != nil {
		return formatConnectionError(err)
	}
	return nil
}

// validateAzureDevOpsConn tests Azure DevOps PAT by fetching repository info.
func validateAzureDevOpsConn(ctx context.Context, pat, org, project string) error {
	ctx, cancel := context.WithTimeout(ctx, validationTimeout)
	defer cancel()

	client := azuredevops.NewClient(pat, org, project)

	_, err := client.GetDefaultBranch(ctx)
	if err != nil {
		return formatConnectionError(err)
	}
	return nil
}

// validateAsanaConn tests Asana access token by pinging the workspace.
func validateAsanaConn(ctx context.Context, accessToken, workspaceID string) error {
	ctx, cancel := context.WithTimeout(ctx, validationTimeout)
	defer cancel()

	client := asana.NewClient(accessToken, workspaceID)

	if err := client.Ping(ctx); err != nil {
		return formatConnectionError(err)
	}
	return nil
}

// validateTelegramConn tests Telegram bot token via getMe API.
// Returns bot username on success.
func validateTelegramConn(ctx context.Context, botToken string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, validationTimeout)
	defer cancel()

	url := fmt.Sprintf("https://api.telegram.org/bot%s/getMe", botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: validationTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", formatConnectionError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		OK     bool   `json:"ok"`
		Result struct {
			Username string `json:"username"`
		} `json:"result"`
		Description string `json:"description"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if !result.OK {
		return "", fmt.Errorf("authentication failed: %s", result.Description)
	}

	return "@" + result.Result.Username, nil
}

// formatConnectionError converts adapter errors to human-friendly messages.
func formatConnectionError(err error) error {
	if err == nil {
		return nil
	}

	errStr := err.Error()

	// Check for common HTTP status codes
	if strings.Contains(errStr, "status 401") {
		return fmt.Errorf("authentication failed (401): invalid or expired credentials")
	}
	if strings.Contains(errStr, "status 403") {
		return fmt.Errorf("access denied (403): insufficient permissions")
	}
	if strings.Contains(errStr, "status 404") {
		return fmt.Errorf("not found (404): resource does not exist or no access")
	}

	// Check for network errors
	if strings.Contains(errStr, "context deadline exceeded") {
		return fmt.Errorf("connection timeout: server did not respond in time")
	}
	if strings.Contains(errStr, "no such host") {
		return fmt.Errorf("network error: could not resolve hostname")
	}
	if strings.Contains(errStr, "connection refused") {
		return fmt.Errorf("network error: connection refused")
	}

	return err
}
