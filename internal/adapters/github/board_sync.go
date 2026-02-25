package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

const graphqlURL = "https://api.github.com/graphql"

// ProjectBoardSync moves issues across GitHub Projects V2 board columns
// by calling the GraphQL API. All operations are best-effort: errors are
// logged but never propagated to callers.
type ProjectBoardSync struct {
	client     *Client
	cfg        *ProjectBoardConfig
	owner      string
	graphqlURL string // overridable for testing; defaults to graphqlURL constant
}

// NewProjectBoardSync creates a new board sync instance.
// Returns nil if cfg is nil or not enabled — callers should nil-check.
func NewProjectBoardSync(client *Client, cfg *ProjectBoardConfig, owner string) *ProjectBoardSync {
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	return &ProjectBoardSync{
		client:     client,
		cfg:        cfg,
		owner:      owner,
		graphqlURL: graphqlURL,
	}
}

// graphqlRequest is the generic GraphQL request envelope.
type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// graphqlResponse is the generic GraphQL response envelope.
type graphqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// doGraphQL executes a GraphQL query against the GitHub API.
func (bs *ProjectBoardSync) doGraphQL(ctx context.Context, req graphqlRequest) (*graphqlResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal graphql request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, bs.graphqlURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create graphql request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+bs.client.token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := bs.client.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("graphql request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read graphql response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("graphql HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var gqlResp graphqlResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return nil, fmt.Errorf("unmarshal graphql response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", gqlResp.Errors[0].Message)
	}

	return &gqlResp, nil
}

// findProjectID resolves the Projects V2 node ID from owner + project number.
func (bs *ProjectBoardSync) findProjectID(ctx context.Context) (string, error) {
	query := `query($owner: String!, $number: Int!) {
		user(login: $owner) {
			projectV2(number: $number) { id }
		}
	}`
	// Try user first, fall back to organization
	resp, err := bs.doGraphQL(ctx, graphqlRequest{
		Query: query,
		Variables: map[string]any{
			"owner":  bs.owner,
			"number": bs.cfg.ProjectNumber,
		},
	})
	if err == nil && resp != nil {
		var data struct {
			User *struct {
				ProjectV2 *struct {
					ID string `json:"id"`
				} `json:"projectV2"`
			} `json:"user"`
		}
		if json.Unmarshal(resp.Data, &data) == nil && data.User != nil && data.User.ProjectV2 != nil {
			return data.User.ProjectV2.ID, nil
		}
	}

	// Try organization
	orgQuery := `query($owner: String!, $number: Int!) {
		organization(login: $owner) {
			projectV2(number: $number) { id }
		}
	}`
	resp, err = bs.doGraphQL(ctx, graphqlRequest{
		Query: orgQuery,
		Variables: map[string]any{
			"owner":  bs.owner,
			"number": bs.cfg.ProjectNumber,
		},
	})
	if err != nil {
		return "", fmt.Errorf("find project: %w", err)
	}

	var orgData struct {
		Organization *struct {
			ProjectV2 *struct {
				ID string `json:"id"`
			} `json:"projectV2"`
		} `json:"organization"`
	}
	if err := json.Unmarshal(resp.Data, &orgData); err != nil {
		return "", fmt.Errorf("unmarshal org project: %w", err)
	}
	if orgData.Organization == nil || orgData.Organization.ProjectV2 == nil {
		return "", fmt.Errorf("project #%d not found for owner %q", bs.cfg.ProjectNumber, bs.owner)
	}
	return orgData.Organization.ProjectV2.ID, nil
}

// findProjectItemID finds the project item ID for an issue node ID within the project.
func (bs *ProjectBoardSync) findProjectItemID(ctx context.Context, projectID, issueNodeID string) (string, error) {
	// Use the addProjectV2ItemById mutation to ensure the issue is on the board,
	// then use the returned item ID. This is idempotent — if already added, returns existing item.
	query := `mutation($projectID: ID!, $contentID: ID!) {
		addProjectV2ItemById(input: {projectId: $projectID, contentId: $contentID}) {
			item { id }
		}
	}`
	resp, err := bs.doGraphQL(ctx, graphqlRequest{
		Query: query,
		Variables: map[string]any{
			"projectID": projectID,
			"contentID": issueNodeID,
		},
	})
	if err != nil {
		return "", fmt.Errorf("add item to project: %w", err)
	}

	var data struct {
		AddProjectV2ItemByID struct {
			Item struct {
				ID string `json:"id"`
			} `json:"item"`
		} `json:"addProjectV2ItemById"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return "", fmt.Errorf("unmarshal item: %w", err)
	}
	return data.AddProjectV2ItemByID.Item.ID, nil
}

// findStatusFieldAndOptionID resolves the status field ID and the option ID for a given status name.
func (bs *ProjectBoardSync) findStatusFieldAndOptionID(ctx context.Context, projectID, statusName string) (fieldID string, optionID string, err error) {
	query := `query($projectID: ID!) {
		node(id: $projectID) {
			... on ProjectV2 {
				fields(first: 20) {
					nodes {
						... on ProjectV2SingleSelectField {
							id
							name
							options { id name }
						}
					}
				}
			}
		}
	}`
	resp, err := bs.doGraphQL(ctx, graphqlRequest{
		Query: query,
		Variables: map[string]any{
			"projectID": projectID,
		},
	})
	if err != nil {
		return "", "", fmt.Errorf("query project fields: %w", err)
	}

	var data struct {
		Node struct {
			Fields struct {
				Nodes []struct {
					ID      string `json:"id"`
					Name    string `json:"name"`
					Options []struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"options"`
				} `json:"nodes"`
			} `json:"fields"`
		} `json:"node"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return "", "", fmt.Errorf("unmarshal fields: %w", err)
	}

	fieldName := bs.cfg.StatusField
	if fieldName == "" {
		fieldName = "Status"
	}

	for _, f := range data.Node.Fields.Nodes {
		if f.Name == fieldName {
			for _, opt := range f.Options {
				if opt.Name == statusName {
					return f.ID, opt.ID, nil
				}
			}
			return "", "", fmt.Errorf("status option %q not found in field %q", statusName, fieldName)
		}
	}
	return "", "", fmt.Errorf("field %q not found in project", fieldName)
}

// UpdateProjectItemStatus moves an issue to a new status column on the project board.
// issueNodeID is the GraphQL node ID of the issue (Issue.NodeID).
// status is the column name (e.g. "In Dev", "Done").
func (bs *ProjectBoardSync) UpdateProjectItemStatus(ctx context.Context, issueNodeID, status string) error {
	projectID, err := bs.findProjectID(ctx)
	if err != nil {
		return err
	}

	itemID, err := bs.findProjectItemID(ctx, projectID, issueNodeID)
	if err != nil {
		return err
	}

	fieldID, optionID, err := bs.findStatusFieldAndOptionID(ctx, projectID, status)
	if err != nil {
		return err
	}

	mutation := `mutation($projectID: ID!, $itemID: ID!, $fieldID: ID!, $optionID: String!) {
		updateProjectV2ItemFieldValue(input: {
			projectId: $projectID
			itemId: $itemID
			fieldId: $fieldID
			value: { singleSelectOptionId: $optionID }
		}) {
			projectV2Item { id }
		}
	}`
	_, err = bs.doGraphQL(ctx, graphqlRequest{
		Query: mutation,
		Variables: map[string]any{
			"projectID": projectID,
			"itemID":    itemID,
			"fieldID":   fieldID,
			"optionID":  optionID,
		},
	})
	if err != nil {
		return fmt.Errorf("update status to %q: %w", status, err)
	}

	slog.Info("board sync: updated project item status",
		slog.String("status", status),
		slog.String("issue_node_id", issueNodeID),
	)
	return nil
}
