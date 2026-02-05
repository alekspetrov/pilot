package executor

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestNewSubtaskParser_NoAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	parser := NewSubtaskParser(slog.Default())
	if parser != nil {
		t.Error("expected nil parser when ANTHROPIC_API_KEY is not set")
	}
}

func TestNewSubtaskParser_WithAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-api-key")
	parser := NewSubtaskParser(slog.Default())
	if parser == nil {
		t.Fatal("expected non-nil parser when ANTHROPIC_API_KEY is set")
	}
	if parser.apiKey != "test-api-key" {
		t.Errorf("apiKey = %q, want %q", parser.apiKey, "test-api-key")
	}
	if parser.model != "claude-haiku-4-5-20251001" {
		t.Errorf("model = %q, want %q", parser.model, "claude-haiku-4-5-20251001")
	}
}

func TestSubtaskParser_Parse_Success(t *testing.T) {
	// Mock Anthropic API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		if r.Header.Get("x-api-key") != "test-api-key" {
			t.Errorf("missing or wrong x-api-key header")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("missing or wrong anthropic-version header")
		}

		// Return mock response
		resp := map[string]interface{}{
			"content": []map[string]string{
				{
					"text": `{"subtasks": [{"title": "Set up database", "description": "Create tables for users", "order": 1}, {"title": "Add auth service", "description": "JWT-based authentication", "order": 2}, {"title": "Create endpoints", "description": "REST API for login/logout", "order": 3}]}`,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	parser := &SubtaskParser{
		apiKey:     "test-api-key",
		httpClient: server.Client(),
		model:      "claude-haiku-4-5-20251001",
		log:        slog.Default(),
	}
	// Override URL by replacing the HTTP client transport
	parser.httpClient = &http.Client{
		Transport: &rewriteTransport{
			base: server.Client().Transport,
			url:  server.URL,
		},
	}

	subtasks, err := parser.Parse(context.Background(), "1. Set up database\n2. Add auth\n3. Create endpoints")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(subtasks) != 3 {
		t.Fatalf("expected 3 subtasks, got %d", len(subtasks))
	}

	expected := []PlannedSubtask{
		{Title: "Set up database", Description: "Create tables for users", Order: 1},
		{Title: "Add auth service", Description: "JWT-based authentication", Order: 2},
		{Title: "Create endpoints", Description: "REST API for login/logout", Order: 3},
	}

	for i, exp := range expected {
		if subtasks[i].Title != exp.Title {
			t.Errorf("subtask %d: title = %q, want %q", i, subtasks[i].Title, exp.Title)
		}
		if subtasks[i].Description != exp.Description {
			t.Errorf("subtask %d: description = %q, want %q", i, subtasks[i].Description, exp.Description)
		}
		if subtasks[i].Order != exp.Order {
			t.Errorf("subtask %d: order = %d, want %d", i, subtasks[i].Order, exp.Order)
		}
	}
}

func TestSubtaskParser_Parse_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	parser := &SubtaskParser{
		apiKey:     "test-api-key",
		httpClient: &http.Client{Transport: &rewriteTransport{base: server.Client().Transport, url: server.URL}},
		model:      "claude-haiku-4-5-20251001",
		log:        slog.Default(),
	}

	_, err := parser.Parse(context.Background(), "some output")
	if err == nil {
		t.Fatal("expected error for API 500 response")
	}
}

func TestSubtaskParser_Parse_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"content": []map[string]string{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	parser := &SubtaskParser{
		apiKey:     "test-api-key",
		httpClient: &http.Client{Transport: &rewriteTransport{base: server.Client().Transport, url: server.URL}},
		model:      "claude-haiku-4-5-20251001",
		log:        slog.Default(),
	}

	_, err := parser.Parse(context.Background(), "some output")
	if err == nil {
		t.Fatal("expected error for empty API response")
	}
}

func TestSubtaskParser_Parse_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"content": []map[string]string{
				{"text": "not valid json"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	parser := &SubtaskParser{
		apiKey:     "test-api-key",
		httpClient: &http.Client{Transport: &rewriteTransport{base: server.Client().Transport, url: server.URL}},
		model:      "claude-haiku-4-5-20251001",
		log:        slog.Default(),
	}

	_, err := parser.Parse(context.Background(), "some output")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestSubtaskParser_Parse_NoSubtasks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"content": []map[string]string{
				{"text": `{"subtasks": []}`},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	parser := &SubtaskParser{
		apiKey:     "test-api-key",
		httpClient: &http.Client{Transport: &rewriteTransport{base: server.Client().Transport, url: server.URL}},
		model:      "claude-haiku-4-5-20251001",
		log:        slog.Default(),
	}

	_, err := parser.Parse(context.Background(), "some output")
	if err == nil {
		t.Fatal("expected error for empty subtasks")
	}
}

// rewriteTransport redirects all requests to the test server URL.
type rewriteTransport struct {
	base http.RoundTripper
	url  string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = t.url[len("http://"):]
	if t.base != nil {
		return t.base.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func TestSubtaskParser_Parse_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow response - should be cancelled
		select {
		case <-r.Context().Done():
			return
		}
	}))
	defer server.Close()

	parser := &SubtaskParser{
		apiKey:     "test-api-key",
		httpClient: &http.Client{Transport: &rewriteTransport{base: server.Client().Transport, url: server.URL}},
		model:      "claude-haiku-4-5-20251001",
		log:        slog.Default(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := parser.Parse(ctx, "some output")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// TestSubtaskParser_EnvIntegration verifies the env var lookup is used.
func TestSubtaskParser_EnvIntegration(t *testing.T) {
	// Save and restore
	orig := os.Getenv("ANTHROPIC_API_KEY")
	defer func() { _ = os.Setenv("ANTHROPIC_API_KEY", orig) }()

	t.Setenv("ANTHROPIC_API_KEY", "")
	if p := NewSubtaskParser(slog.Default()); p != nil {
		t.Error("expected nil when env var is empty")
	}

	t.Setenv("ANTHROPIC_API_KEY", "test-key-123")
	if p := NewSubtaskParser(slog.Default()); p == nil {
		t.Error("expected non-nil when env var is set")
	}
}
