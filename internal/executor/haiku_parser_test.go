package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// newTestSubtaskParser creates a SubtaskParser pointed at a test server URL.
func newTestSubtaskParser(serverURL string) *SubtaskParser {
	return &SubtaskParser{
		apiKey: "test-api-key",
		apiURL: serverURL,
		model:  haikuModel,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// makeHaikuAPIResponse builds an Anthropic Messages API response body.
func makeHaikuAPIResponse(text string) []byte {
	resp := haikuResponse{
		Content: []haikuContentBlock{
			{Type: "text", Text: text},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

func TestSubtaskParser_Parse_ValidResponse(t *testing.T) {
	tests := []struct {
		name     string
		response string
		expected []PlannedSubtask
	}{
		{
			name:     "three subtasks",
			response: `[{"order":1,"title":"Create schema","description":"Add migration for users table","depends_on":[]},{"order":2,"title":"Add endpoints","description":"REST API for CRUD operations","depends_on":[1]},{"order":3,"title":"Write tests","description":"Unit and integration tests","depends_on":[1,2]}]`,
			expected: []PlannedSubtask{
				{Order: 1, Title: "Create schema", Description: "Add migration for users table", DependsOn: []int{}},
				{Order: 2, Title: "Add endpoints", Description: "REST API for CRUD operations", DependsOn: []int{1}},
				{Order: 3, Title: "Write tests", Description: "Unit and integration tests", DependsOn: []int{1, 2}},
			},
		},
		{
			name:     "single subtask",
			response: `[{"order":1,"title":"Fix bug","description":"Patch the nil pointer in handler","depends_on":[]}]`,
			expected: []PlannedSubtask{
				{Order: 1, Title: "Fix bug", Description: "Patch the nil pointer in handler", DependsOn: []int{}},
			},
		},
		{
			name:     "five subtasks",
			response: `[{"order":1,"title":"A","description":"first","depends_on":[]},{"order":2,"title":"B","description":"second","depends_on":[]},{"order":3,"title":"C","description":"third","depends_on":[]},{"order":4,"title":"D","description":"fourth","depends_on":[]},{"order":5,"title":"E","description":"fifth","depends_on":[]}]`,
			expected: []PlannedSubtask{
				{Order: 1, Title: "A", Description: "first", DependsOn: []int{}},
				{Order: 2, Title: "B", Description: "second", DependsOn: []int{}},
				{Order: 3, Title: "C", Description: "third", DependsOn: []int{}},
				{Order: 4, Title: "D", Description: "fourth", DependsOn: []int{}},
				{Order: 5, Title: "E", Description: "fifth", DependsOn: []int{}},
			},
		},
		{
			name:     "subtasks with no depends_on field",
			response: `[{"order":1,"title":"Setup","description":"Initialize project"},{"order":2,"title":"Implement","description":"Write the code"}]`,
			expected: []PlannedSubtask{
				{Order: 1, Title: "Setup", Description: "Initialize project"},
				{Order: 2, Title: "Implement", Description: "Write the code"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(makeHaikuAPIResponse(tt.response))
			}))
			defer server.Close()

			parser := newTestSubtaskParser(server.URL)
			subtasks, err := parser.Parse(context.Background(), "planning output text")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(subtasks) != len(tt.expected) {
				t.Fatalf("got %d subtasks, want %d", len(subtasks), len(tt.expected))
			}

			for i, want := range tt.expected {
				got := subtasks[i]
				if got.Order != want.Order {
					t.Errorf("subtask[%d].Order = %d, want %d", i, got.Order, want.Order)
				}
				if got.Title != want.Title {
					t.Errorf("subtask[%d].Title = %q, want %q", i, got.Title, want.Title)
				}
				if got.Description != want.Description {
					t.Errorf("subtask[%d].Description = %q, want %q", i, got.Description, want.Description)
				}
				if len(got.DependsOn) != len(want.DependsOn) {
					t.Errorf("subtask[%d].DependsOn length = %d, want %d", i, len(got.DependsOn), len(want.DependsOn))
				} else {
					for j, dep := range want.DependsOn {
						if got.DependsOn[j] != dep {
							t.Errorf("subtask[%d].DependsOn[%d] = %d, want %d", i, j, got.DependsOn[j], dep)
						}
					}
				}
			}
		})
	}
}

func TestSubtaskParser_Parse_MalformedJSON(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{
			name:     "plain text not JSON",
			response: `not valid json at all`,
		},
		{
			name:     "JSON object instead of array",
			response: `{"order":1,"title":"Only one","description":"Not an array"}`,
		},
		{
			name:     "truncated JSON",
			response: `[{"order":1,"title":"Incomplete`,
		},
		{
			name:     "HTML instead of JSON",
			response: `<html><body>Error</body></html>`,
		},
		{
			name:     "empty string",
			response: ``,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(makeHaikuAPIResponse(tt.response))
			}))
			defer server.Close()

			parser := newTestSubtaskParser(server.URL)
			_, err := parser.Parse(context.Background(), "some planning output")
			if err == nil {
				t.Error("expected error for malformed JSON, got nil")
			}
		})
	}
}

func TestSubtaskParser_Parse_HTTPTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than client timeout
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	parser := &SubtaskParser{
		apiKey: "test-api-key",
		apiURL: server.URL,
		model:  haikuModel,
		httpClient: &http.Client{
			Timeout: 50 * time.Millisecond, // Very short timeout
		},
	}

	_, err := parser.Parse(context.Background(), "some planning output")
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}

func TestSubtaskParser_Parse_NonOKStatus(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{name: "unauthorized", statusCode: http.StatusUnauthorized},
		{name: "rate limited", statusCode: http.StatusTooManyRequests},
		{name: "server error", statusCode: http.StatusInternalServerError},
		{name: "bad gateway", statusCode: http.StatusBadGateway},
		{name: "service unavailable", statusCode: http.StatusServiceUnavailable},
		{name: "forbidden", statusCode: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(`{"error":"test error"}`))
			}))
			defer server.Close()

			parser := newTestSubtaskParser(server.URL)
			_, err := parser.Parse(context.Background(), "some planning output")
			if err == nil {
				t.Errorf("expected error for HTTP %d, got nil", tt.statusCode)
			}
		})
	}
}

func TestSubtaskParser_Parse_RequestHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify method
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}

		// Verify Content-Type
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}

		// Verify API key
		if got := r.Header.Get("x-api-key"); got != "test-api-key" {
			t.Errorf("x-api-key = %q, want %q", got, "test-api-key")
		}

		// Verify API version
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Errorf("anthropic-version = %q, want %q", got, "2023-06-01")
		}

		// Verify request body
		var reqBody haikuRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		if reqBody.Model != haikuModel {
			t.Errorf("model = %q, want %q", reqBody.Model, haikuModel)
		}
		if reqBody.MaxTokens != haikuMaxTokens {
			t.Errorf("max_tokens = %d, want %d", reqBody.MaxTokens, haikuMaxTokens)
		}
		if reqBody.System != haikuSystemPrompt {
			t.Errorf("system prompt mismatch")
		}
		if len(reqBody.Messages) != 1 {
			t.Errorf("messages length = %d, want 1", len(reqBody.Messages))
		} else {
			if reqBody.Messages[0].Role != "user" {
				t.Errorf("message role = %q, want %q", reqBody.Messages[0].Role, "user")
			}
			if reqBody.Messages[0].Content != "test planning output" {
				t.Errorf("message content = %q, want %q", reqBody.Messages[0].Content, "test planning output")
			}
		}
		if reqBody.OutputConfig.Effort != "low" {
			t.Errorf("effort = %q, want %q", reqBody.OutputConfig.Effort, "low")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeHaikuAPIResponse(`[{"order":1,"title":"Task","description":"Desc","depends_on":[]}]`))
	}))
	defer server.Close()

	parser := newTestSubtaskParser(server.URL)
	_, err := parser.Parse(context.Background(), "test planning output")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSubtaskParser_Parse_EmptyInput(t *testing.T) {
	parser := newTestSubtaskParser("http://should-not-be-called")
	_, err := parser.Parse(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty input, got nil")
	}
}

func TestSubtaskParser_Parse_EmptyAPIResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":[]}`))
	}))
	defer server.Close()

	parser := newTestSubtaskParser(server.URL)
	_, err := parser.Parse(context.Background(), "some planning output")
	if err == nil {
		t.Error("expected error for empty API response, got nil")
	}
}

func TestSubtaskParser_Parse_EmptySubtaskArray(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeHaikuAPIResponse(`[]`))
	}))
	defer server.Close()

	parser := newTestSubtaskParser(server.URL)
	_, err := parser.Parse(context.Background(), "some planning output")
	if err == nil {
		t.Error("expected error for empty subtask array, got nil")
	}
}

func TestSubtaskParser_Parse_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	parser := newTestSubtaskParser(server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := parser.Parse(ctx, "some planning output")
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

func TestSubtaskParser_Parse_NoTextBlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Response has content but no "text" type block
		_, _ = w.Write([]byte(`{"content":[{"type":"image","text":""}]}`))
	}))
	defer server.Close()

	parser := newTestSubtaskParser(server.URL)
	_, err := parser.Parse(context.Background(), "some planning output")
	if err == nil {
		t.Error("expected error for no text block, got nil")
	}
}

func TestNewSubtaskParser_WithConfigKey(t *testing.T) {
	parser, err := NewSubtaskParser("test-config-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parser.apiKey != "test-config-key" {
		t.Errorf("apiKey = %q, want %q", parser.apiKey, "test-config-key")
	}
	if parser.model != haikuModel {
		t.Errorf("model = %q, want %q", parser.model, haikuModel)
	}
	if parser.apiURL != defaultAnthropicURL {
		t.Errorf("apiURL = %q, want %q", parser.apiURL, defaultAnthropicURL)
	}
	if parser.httpClient == nil {
		t.Error("httpClient is nil")
	}
}

func TestNewSubtaskParser_WithEnvVar(t *testing.T) {
	origKey := os.Getenv("ANTHROPIC_API_KEY")
	t.Setenv("ANTHROPIC_API_KEY", "test-env-key")
	defer func() {
		if origKey != "" {
			_ = os.Setenv("ANTHROPIC_API_KEY", origKey)
		} else {
			_ = os.Unsetenv("ANTHROPIC_API_KEY")
		}
	}()

	parser, err := NewSubtaskParser("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parser.apiKey != "test-env-key" {
		t.Errorf("apiKey = %q, want %q", parser.apiKey, "test-env-key")
	}
}

func TestNewSubtaskParser_NoKey(t *testing.T) {
	origKey := os.Getenv("ANTHROPIC_API_KEY")
	_ = os.Unsetenv("ANTHROPIC_API_KEY")
	defer func() {
		if origKey != "" {
			_ = os.Setenv("ANTHROPIC_API_KEY", origKey)
		}
	}()

	_, err := NewSubtaskParser("")
	if err == nil {
		t.Error("expected error when no API key available, got nil")
	}
}
