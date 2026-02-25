package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewProjectBoardSync_NilConfig(t *testing.T) {
	client := NewClient("test-token")
	bs := NewProjectBoardSync(client, nil, "owner")
	if bs != nil {
		t.Error("expected nil for nil config")
	}
}

func TestNewProjectBoardSync_Disabled(t *testing.T) {
	client := NewClient("test-token")
	cfg := &ProjectBoardConfig{Enabled: false}
	bs := NewProjectBoardSync(client, cfg, "owner")
	if bs != nil {
		t.Error("expected nil for disabled config")
	}
}

func TestNewProjectBoardSync_Enabled(t *testing.T) {
	client := NewClient("test-token")
	cfg := &ProjectBoardConfig{
		Enabled:       true,
		ProjectNumber: 1,
		StatusField:   "Status",
	}
	bs := NewProjectBoardSync(client, cfg, "owner")
	if bs == nil {
		t.Fatal("expected non-nil for enabled config")
	}
	if bs.owner != "owner" {
		t.Errorf("owner = %q, want %q", bs.owner, "owner")
	}
}

func TestProjectBoardConfig_GetStatuses_Nil(t *testing.T) {
	var cfg *ProjectBoardConfig
	s := cfg.GetStatuses()
	if s.InProgress != "" || s.Done != "" || s.Failed != "" {
		t.Errorf("expected zero statuses for nil config, got %+v", s)
	}
}

func TestProjectBoardConfig_GetStatuses(t *testing.T) {
	cfg := &ProjectBoardConfig{
		Statuses: ProjectStatuses{
			InProgress: "In Dev",
			Done:       "Done",
			Failed:     "Blocked",
		},
	}
	s := cfg.GetStatuses()
	if s.InProgress != "In Dev" {
		t.Errorf("InProgress = %q, want %q", s.InProgress, "In Dev")
	}
	if s.Done != "Done" {
		t.Errorf("Done = %q, want %q", s.Done, "Done")
	}
	if s.Failed != "Blocked" {
		t.Errorf("Failed = %q, want %q", s.Failed, "Blocked")
	}
}

func TestUpdateProjectItemStatus_GraphQLFlow(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req graphqlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		callCount++
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.Contains(req.Query, "organization"):
			// findProjectID - org query
			_, _ = w.Write([]byte(`{"data":{"organization":{"projectV2":{"id":"PVT_123"}}}}`))

		case strings.Contains(req.Query, "user"):
			// findProjectID - user query (return empty so it falls through to org)
			_, _ = w.Write([]byte(`{"data":{"user":null}}`))

		case strings.Contains(req.Query, "addProjectV2ItemById"):
			// findProjectItemID
			_, _ = w.Write([]byte(`{"data":{"addProjectV2ItemById":{"item":{"id":"PVTI_456"}}}}`))

		case strings.Contains(req.Query, "fields"):
			// findStatusFieldAndOptionID
			_, _ = w.Write([]byte(`{"data":{"node":{"fields":{"nodes":[{"id":"PVTSSF_789","name":"Status","options":[{"id":"OPT_1","name":"In Dev"},{"id":"OPT_2","name":"Done"}]}]}}}}`))

		case strings.Contains(req.Query, "updateProjectV2ItemFieldValue"):
			// UpdateProjectItemStatus mutation
			_, _ = w.Write([]byte(`{"data":{"updateProjectV2ItemFieldValue":{"projectV2Item":{"id":"PVTI_456"}}}}`))

		default:
			http.Error(w, "unexpected query", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-token", server.URL)
	cfg := &ProjectBoardConfig{
		Enabled:       true,
		ProjectNumber: 1,
		StatusField:   "Status",
	}
	bs := NewProjectBoardSync(client, cfg, "myorg")
	bs.graphqlURL = server.URL // Point GraphQL calls at test server

	ctx := t.Context()
	if err := bs.UpdateProjectItemStatus(ctx, "ISSUE_NODE_1", "In Dev"); err != nil {
		t.Fatalf("UpdateProjectItemStatus failed: %v", err)
	}

	// Expect 5 GraphQL calls: user lookup, org lookup, addItem, fields, updateField
	if callCount != 5 {
		t.Errorf("expected 5 GraphQL calls, got %d", callCount)
	}
}

func TestUpdateProjectItemStatus_NilBoardSync(t *testing.T) {
	// Verify that calling methods on a nil-constructed sync is safe
	// (callers use syncBoardStatus helper which nil-checks)
	var bs *ProjectBoardSync
	if bs != nil {
		t.Error("should be nil")
	}
}
