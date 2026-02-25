package plane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a Plane.so API client
type Client struct {
	baseURL       string
	apiKey        string
	workspaceSlug string
	httpClient    *http.Client
}

// NewClient creates a new Plane client
func NewClient(baseURL, apiKey, workspaceSlug string) *Client {
	return &Client{
		baseURL:       strings.TrimSuffix(baseURL, "/"),
		apiKey:        apiKey,
		workspaceSlug: workspaceSlug,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewClientWithHTTP creates a new Plane client with a custom HTTP client (for testing)
func NewClientWithHTTP(baseURL, apiKey, workspaceSlug string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:       strings.TrimSuffix(baseURL, "/"),
		apiKey:        apiKey,
		workspaceSlug: workspaceSlug,
		httpClient:    httpClient,
	}
}

// doRequest performs an HTTP request to the Plane API
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

// projectPath returns the API path prefix for a project
func (c *Client) projectPath(projectID string) string {
	return fmt.Sprintf("/api/v1/workspaces/%s/projects/%s", c.workspaceSlug, projectID)
}

// ListLabels lists all labels for a project
func (c *Client) ListLabels(ctx context.Context, projectID string) ([]Label, error) {
	path := c.projectPath(projectID) + "/labels/"
	var resp ListResponse[Label]
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// ListWorkItems lists work items for a project with optional label filter
func (c *Client) ListWorkItems(ctx context.Context, projectID string, labelID string) ([]WorkItem, error) {
	path := c.projectPath(projectID) + "/work-items/"
	if labelID != "" {
		path += "?label=" + labelID
	}
	var resp ListResponse[WorkItem]
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// GetWorkItem fetches a single work item
func (c *Client) GetWorkItem(ctx context.Context, projectID, workItemID string) (*WorkItem, error) {
	path := c.projectPath(projectID) + "/work-items/" + workItemID + "/"
	var item WorkItem
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// UpdateWorkItem updates a work item's fields
func (c *Client) UpdateWorkItem(ctx context.Context, projectID, workItemID string, fields map[string]interface{}) error {
	path := c.projectPath(projectID) + "/work-items/" + workItemID + "/"
	return c.doRequest(ctx, http.MethodPatch, path, fields, nil)
}

// AddLabel adds a label UUID to a work item's labels array.
// Plane stores labels as a UUID array on the work item — there is no separate label-issue endpoint.
func (c *Client) AddLabel(ctx context.Context, projectID, workItemID, labelID string) error {
	item, err := c.GetWorkItem(ctx, projectID, workItemID)
	if err != nil {
		return fmt.Errorf("failed to get work item for label add: %w", err)
	}

	// Check if label already present
	for _, l := range item.Labels {
		if l == labelID {
			return nil // already has the label
		}
	}

	labels := append(item.Labels, labelID)
	return c.UpdateWorkItem(ctx, projectID, workItemID, map[string]interface{}{
		"labels": labels,
	})
}

// RemoveLabel removes a label UUID from a work item's labels array.
func (c *Client) RemoveLabel(ctx context.Context, projectID, workItemID, labelID string) error {
	item, err := c.GetWorkItem(ctx, projectID, workItemID)
	if err != nil {
		return fmt.Errorf("failed to get work item for label remove: %w", err)
	}

	filtered := make([]string, 0, len(item.Labels))
	for _, l := range item.Labels {
		if l != labelID {
			filtered = append(filtered, l)
		}
	}

	if len(filtered) == len(item.Labels) {
		return nil // label not present
	}

	return c.UpdateWorkItem(ctx, projectID, workItemID, map[string]interface{}{
		"labels": filtered,
	})
}

// HasLabelID checks if a work item has a specific label UUID
func HasLabelID(item *WorkItem, labelID string) bool {
	for _, l := range item.Labels {
		if l == labelID {
			return true
		}
	}
	return false
}

// AddComment adds a comment to a work item
func (c *Client) AddComment(ctx context.Context, projectID, workItemID, body string) error {
	path := c.projectPath(projectID) + "/work-items/" + workItemID + "/comments/"
	reqBody := map[string]string{
		"comment_html": body,
	}
	return c.doRequest(ctx, http.MethodPost, path, reqBody, nil)
}
