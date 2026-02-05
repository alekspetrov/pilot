package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewSubtaskParser(t *testing.T) {
	tests := []struct {
		name        string
		configKey   string
		envKey      string
		wantErr     bool
		wantAPIKey  string
	}{
		{
			name:       "config key used",
			configKey:  "test-config-key",
			wantErr:    false,
			wantAPIKey: "test-config-key",
		},
		{
			name:       "env var fallback",
			configKey:  "",
			envKey:     "test-env-key",
			wantErr:    false,
			wantAPIKey: "test-env-key",
		},
		{
			name:       "config key takes precedence over env",
			configKey:  "config-key",
			envKey:     "env-key",
			wantErr:    false,
			wantAPIKey: "config-key",
		},
		{
			name:    "no key returns error",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envKey != "" {
				t.Setenv("ANTHROPIC_API_KEY", tt.envKey)
			} else {
				t.Setenv("ANTHROPIC_API_KEY", "")
			}

			parser, err := NewSubtaskParser(tt.configKey)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if parser.apiKey != tt.wantAPIKey {
				t.Errorf("apiKey = %q, want %q", parser.apiKey, tt.wantAPIKey)
			}
			if parser.httpClient == nil {
				t.Error("httpClient is nil")
			}
			if parser.httpClient.Timeout != 10*time.Second {
				t.Errorf("timeout = %v, want %v", parser.httpClient.Timeout, 10*time.Second)
			}
		})
	}
}

func TestSubtaskParserParseEmptyInput(t *testing.T) {
	parser := &SubtaskParser{
		apiKey:     "test-api-key",
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}

	_, err := parser.Parse(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty input, got nil")
	}
	if !containsStr(err.Error(), "empty planning output") {
		t.Errorf("error = %q, want to contain 'empty planning output'", err.Error())
	}
}

func TestSubtaskParserParse(t *testing.T) {
	subtasksJSON := `[
		{"title": "Add config struct", "description": "Create config", "order": 1, "depends_on": []},
		{"title": "Implement handler", "description": "Wire handler", "order": 2, "depends_on": [1]}
	]`

	tests := []struct {
		name         string
		response     string
		statusCode   int
		wantCount    int
		wantErr      bool
		wantErrMsg   string
	}{
		{
			name:       "successful parse",
			response:   `{"content": [{"type": "text", "text": ` + jsonEscape(subtasksJSON) + `}]}`,
			statusCode: http.StatusOK,
			wantCount:  2,
		},
		{
			name:       "API error status",
			response:   `{"error": "unauthorized"}`,
			statusCode: http.StatusUnauthorized,
			wantErr:    true,
			wantErrMsg: "API returned status 401",
		},
		{
			name:       "empty content array",
			response:   `{"content": []}`,
			statusCode: http.StatusOK,
			wantErr:    true,
			wantErrMsg: "empty response",
		},
		{
			name:       "no text block",
			response:   `{"content": [{"type": "tool_use", "text": ""}]}`,
			statusCode: http.StatusOK,
			wantErr:    true,
			wantErrMsg: "no text content",
		},
		{
			name:       "invalid JSON in text",
			response:   `{"content": [{"type": "text", "text": "not json"}]}`,
			statusCode: http.StatusOK,
			wantErr:    true,
			wantErrMsg: "parse subtasks JSON",
		},
		{
			name:       "empty array",
			response:   `{"content": [{"type": "text", "text": "[]"}]}`,
			statusCode: http.StatusOK,
			wantErr:    true,
			wantErrMsg: "empty subtask array",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify headers
				if got := r.Header.Get("Content-Type"); got != "application/json" {
					t.Errorf("Content-Type = %q, want application/json", got)
				}
				if got := r.Header.Get("x-api-key"); got != "test-api-key" {
					t.Errorf("x-api-key = %q, want test-api-key", got)
				}
				if got := r.Header.Get("anthropic-version"); got != anthropicAPIVersion {
					t.Errorf("anthropic-version = %q, want %q", got, anthropicAPIVersion)
				}

				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			parser := &SubtaskParser{
				apiKey: "test-api-key",
				httpClient: &http.Client{
					Timeout:   10 * time.Second,
					Transport: &testTransport{serverURL: server.URL},
				},
			}

			subtasks, err := parser.Parse(context.Background(), "Plan output here")

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantErrMsg != "" {
					if !containsStr(err.Error(), tt.wantErrMsg) {
						t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErrMsg)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(subtasks) != tt.wantCount {
				t.Fatalf("subtask count = %d, want %d", len(subtasks), tt.wantCount)
			}
		})
	}
}

func TestHaikuParserParseSubtaskFields(t *testing.T) {
	subtasksJSON := `[
		{"title": "Setup database", "description": "Create tables and indexes", "order": 1, "depends_on": []},
		{"title": "Add API endpoints", "description": "REST handlers for CRUD", "order": 2, "depends_on": [1]},
		{"title": "Write tests", "description": "Unit and integration tests", "order": 3, "depends_on": [1, 2]}
	]`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		resp := `{"content": [{"type": "text", "text": ` + jsonEscape(subtasksJSON) + `}]}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	parser := &SubtaskParser{
		apiKey: "test-api-key",
		httpClient: &http.Client{
			Timeout:   10 * time.Second,
			Transport: &testTransport{serverURL: server.URL},
		},
	}

	subtasks, err := parser.Parse(context.Background(), "Plan output")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(subtasks) != 3 {
		t.Fatalf("count = %d, want 3", len(subtasks))
	}

	// Verify first subtask
	if subtasks[0].Title != "Setup database" {
		t.Errorf("subtasks[0].Title = %q, want %q", subtasks[0].Title, "Setup database")
	}
	if subtasks[0].Description != "Create tables and indexes" {
		t.Errorf("subtasks[0].Description = %q, want %q", subtasks[0].Description, "Create tables and indexes")
	}
	if subtasks[0].Order != 1 {
		t.Errorf("subtasks[0].Order = %d, want 1", subtasks[0].Order)
	}
	if len(subtasks[0].DependsOn) != 0 {
		t.Errorf("subtasks[0].DependsOn = %v, want empty", subtasks[0].DependsOn)
	}

	// Verify third subtask dependencies
	if len(subtasks[2].DependsOn) != 2 {
		t.Fatalf("subtasks[2].DependsOn length = %d, want 2", len(subtasks[2].DependsOn))
	}
	if subtasks[2].DependsOn[0] != 1 || subtasks[2].DependsOn[1] != 2 {
		t.Errorf("subtasks[2].DependsOn = %v, want [1 2]", subtasks[2].DependsOn)
	}
}

func TestHaikuParserRequestBody(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content": [{"type": "text", "text": "[{\"title\":\"T\",\"description\":\"D\",\"order\":1,\"depends_on\":[]}]"}]}`))
	}))
	defer server.Close()

	parser := &SubtaskParser{
		apiKey: "test-api-key",
		httpClient: &http.Client{
			Timeout:   10 * time.Second,
			Transport: &testTransport{serverURL: server.URL},
		},
	}

	_, err := parser.Parse(context.Background(), "Planning output text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify model
	if receivedBody["model"] != haikuModel {
		t.Errorf("model = %v, want %v", receivedBody["model"], haikuModel)
	}

	// Verify max_tokens
	if receivedBody["max_tokens"] != float64(haikuMaxTokens) {
		t.Errorf("max_tokens = %v, want %v", receivedBody["max_tokens"], haikuMaxTokens)
	}

	// Verify system prompt
	if receivedBody["system"] == nil || receivedBody["system"] == "" {
		t.Error("system prompt is missing")
	}

	// Verify output_config is NOT sent (not supported on Haiku)
	if _, ok := receivedBody["output_config"]; ok {
		t.Error("output_config should not be sent for Haiku model")
	}

	// Verify messages
	messages, ok := receivedBody["messages"].([]interface{})
	if !ok {
		t.Fatal("messages not an array")
	}
	if len(messages) != 1 {
		t.Errorf("messages length = %d, want 1", len(messages))
	}

	msg := messages[0].(map[string]interface{})
	if msg["role"] != "user" {
		t.Errorf("message role = %v, want user", msg["role"])
	}
	if msg["content"] != "Planning output text" {
		t.Errorf("message content = %v, want 'Planning output text'", msg["content"])
	}
}

// testTransport redirects all requests to a test server.
type testTransport struct {
	serverURL string
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newReq, err := http.NewRequest(req.Method, t.serverURL, req.Body)
	if err != nil {
		return nil, err
	}
	newReq.Header = req.Header
	return http.DefaultTransport.RoundTrip(newReq)
}

// jsonEscape marshals a string as a JSON string value.
func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// containsStr is defined in progress_test.go in the same package.
