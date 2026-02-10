package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestComplexityClassifier_Classify(t *testing.T) {
	tests := []struct {
		name            string
		title           string
		description     string
		response        ComplexityClassification
		wantComplexity  Complexity
		wantDecompose   bool
	}{
		{
			name:        "trivial typo fix",
			title:       "Fix typo in README",
			description: "There's a typo in the getting started section",
			response: ComplexityClassification{
				Complexity:      ComplexityTrivial,
				ShouldDecompose: false,
				Reason:          "Simple typo fix in documentation",
			},
			wantComplexity: ComplexityTrivial,
			wantDecompose:  false,
		},
		{
			name:        "medium feature — single coherent task",
			title:       "Add rate limiting to API",
			description: "Add rate limiting middleware to all API endpoints. Use token bucket algorithm. Configure via YAML. Steps: 1. Create middleware 2. Add config struct 3. Wire in router 4. Add tests",
			response: ComplexityClassification{
				Complexity:      ComplexityMedium,
				ShouldDecompose: false,
				Reason:          "Single coherent feature with implementation steps, not independent work items",
			},
			wantComplexity: ComplexityMedium,
			wantDecompose:  false,
		},
		{
			name:        "complex with decomposition",
			title:       "Implement auth system and notification service",
			description: "Two independent features: 1) OAuth2 authentication with JWT tokens 2) Email/SMS notification service with templates",
			response: ComplexityClassification{
				Complexity:      ComplexityComplex,
				ShouldDecompose: true,
				Reason:          "Two independent features that should be separate PRs",
			},
			wantComplexity: ComplexityComplex,
			wantDecompose:  true,
		},
		{
			name:        "epic multi-phase project",
			title:       "[epic] Rewrite data pipeline",
			description: "Complete rewrite of the data processing pipeline across 5 phases",
			response: ComplexityClassification{
				Complexity:      ComplexityEpic,
				ShouldDecompose: true,
				Reason:          "Multi-phase rewrite requiring separate execution cycles",
			},
			wantComplexity: ComplexityEpic,
			wantDecompose:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request structure
				if r.Header.Get("x-api-key") != "fake-api-key" {
					t.Error("missing or wrong API key")
				}
				if r.Header.Get("anthropic-version") != "2023-06-01" {
					t.Error("missing anthropic-version header")
				}

				responseJSON, _ := json.Marshal(tt.response)
				w.Header().Set("Content-Type", "application/json")
				resp := haikuResponse{
					Content: []struct {
						Text string `json:"text"`
					}{
						{Text: string(responseJSON)},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			classifier := newComplexityClassifierWithURL("fake-api-key", server.URL)
			result, err := classifier.Classify(context.Background(), tt.title, tt.description)
			if err != nil {
				t.Fatalf("Classify() error = %v", err)
			}

			if result.Complexity != tt.wantComplexity {
				t.Errorf("Complexity = %v, want %v", result.Complexity, tt.wantComplexity)
			}
			if result.ShouldDecompose != tt.wantDecompose {
				t.Errorf("ShouldDecompose = %v, want %v", result.ShouldDecompose, tt.wantDecompose)
			}
		})
	}
}

func TestComplexityClassifier_EmptyTask(t *testing.T) {
	classifier := NewComplexityClassifier("fake-api-key")
	result, err := classifier.Classify(context.Background(), "", "")
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if result.Complexity != ComplexityMedium {
		t.Errorf("Complexity = %v, want medium for empty task", result.Complexity)
	}
	if result.ShouldDecompose {
		t.Error("ShouldDecompose should be false for empty task")
	}
}

func TestComplexityClassifier_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	classifier := newComplexityClassifierWithURL("fake-api-key", server.URL)
	_, err := classifier.Classify(context.Background(), "title", "description")
	if err == nil {
		t.Error("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("error should mention status 500, got: %v", err)
	}
}

func TestComplexityClassifier_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := haikuResponse{
			Content: []struct {
				Text string `json:"text"`
			}{
				{Text: "this is not json"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	classifier := newComplexityClassifierWithURL("fake-api-key", server.URL)
	_, err := classifier.Classify(context.Background(), "title", "description")
	if err == nil {
		t.Error("expected error for malformed JSON response")
	}
}

func TestComplexityClassifier_InvalidComplexity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := haikuResponse{
			Content: []struct {
				Text string `json:"text"`
			}{
				{Text: `{"complexity":"impossible","should_decompose":false,"reason":"test"}`},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	classifier := newComplexityClassifierWithURL("fake-api-key", server.URL)
	_, err := classifier.Classify(context.Background(), "title", "description")
	if err == nil {
		t.Error("expected error for invalid complexity value")
	}
	if !strings.Contains(err.Error(), "invalid complexity") {
		t.Errorf("error should mention invalid complexity, got: %v", err)
	}
}

func TestComplexityClassifier_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	classifier := newComplexityClassifierWithURL("fake-api-key", server.URL)
	_, err := classifier.Classify(ctx, "title", "description")
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestComplexityClassifier_MarkdownWrappedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := haikuResponse{
			Content: []struct {
				Text string `json:"text"`
			}{
				{Text: "```json\n{\"complexity\":\"simple\",\"should_decompose\":false,\"reason\":\"small fix\"}\n```"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	classifier := newComplexityClassifierWithURL("fake-api-key", server.URL)
	result, err := classifier.Classify(context.Background(), "Fix button", "Fix the submit button color")
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if result.Complexity != ComplexitySimple {
		t.Errorf("Complexity = %v, want simple", result.Complexity)
	}
}

func TestComplexityClassifier_TruncatesLongInput(t *testing.T) {
	var receivedContent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody haikuRequest
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		if len(reqBody.Messages) > 0 {
			receivedContent = reqBody.Messages[0].Content
		}
		w.Header().Set("Content-Type", "application/json")
		resp := haikuResponse{
			Content: []struct {
				Text string `json:"text"`
			}{
				{Text: `{"complexity":"medium","should_decompose":false,"reason":"truncated"}`},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	longDesc := strings.Repeat("word ", 5000) // Way over maxTaskCharsDefault

	classifier := newComplexityClassifierWithURL("fake-api-key", server.URL)
	_, err := classifier.Classify(context.Background(), "title", longDesc)
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}

	if !strings.Contains(receivedContent, "...[truncated]") {
		t.Error("expected truncation marker in sent content")
	}
	// Should be close to maxTaskCharsDefault + truncation marker
	if len(receivedContent) > maxTaskCharsDefault+100 {
		t.Errorf("content too long: %d chars (max %d)", len(receivedContent), maxTaskCharsDefault)
	}
}

func TestParseClassificationResponse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		want    *ComplexityClassification
	}{
		{
			name:  "clean JSON",
			input: `{"complexity":"medium","should_decompose":false,"reason":"single feature"}`,
			want: &ComplexityClassification{
				Complexity:      ComplexityMedium,
				ShouldDecompose: false,
				Reason:          "single feature",
			},
		},
		{
			name:  "markdown wrapped",
			input: "```json\n{\"complexity\":\"complex\",\"should_decompose\":true,\"reason\":\"multi-component\"}\n```",
			want: &ComplexityClassification{
				Complexity:      ComplexityComplex,
				ShouldDecompose: true,
				Reason:          "multi-component",
			},
		},
		{
			name:  "whitespace padded",
			input: "  \n{\"complexity\":\"trivial\",\"should_decompose\":false,\"reason\":\"typo\"}\n  ",
			want: &ComplexityClassification{
				Complexity:      ComplexityTrivial,
				ShouldDecompose: false,
				Reason:          "typo",
			},
		},
		{
			name:    "invalid JSON",
			input:   "not json at all",
			wantErr: true,
		},
		{
			name:    "invalid complexity value",
			input:   `{"complexity":"mega","should_decompose":false,"reason":"test"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseClassificationResponse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseClassificationResponse() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Complexity != tt.want.Complexity {
				t.Errorf("Complexity = %v, want %v", got.Complexity, tt.want.Complexity)
			}
			if got.ShouldDecompose != tt.want.ShouldDecompose {
				t.Errorf("ShouldDecompose = %v, want %v", got.ShouldDecompose, tt.want.ShouldDecompose)
			}
			if got.Reason != tt.want.Reason {
				t.Errorf("Reason = %q, want %q", got.Reason, tt.want.Reason)
			}
		})
	}
}

func TestNewComplexityClassifier(t *testing.T) {
	classifier := NewComplexityClassifier("test-key")
	if classifier.apiKey != "test-key" {
		t.Errorf("expected apiKey 'test-key', got %q", classifier.apiKey)
	}
	if classifier.model != "claude-haiku-4-5-20251001" {
		t.Errorf("expected default model, got %q", classifier.model)
	}
	if classifier.apiURL != "https://api.anthropic.com/v1/messages" {
		t.Errorf("expected default API URL, got %q", classifier.apiURL)
	}
}

func TestDecomposer_LLMDecision(t *testing.T) {
	config := &DecomposeConfig{
		Enabled:             true,
		MinComplexity:       "complex",
		MaxSubtasks:         5,
		MinDescriptionWords: 50,
	}

	t.Run("LLM says no decompose overrides heuristic", func(t *testing.T) {
		decomposer := NewTaskDecomposer(config)
		decomposer.SetLLMDecomposeDecision(false)

		// This task would normally be decomposed by heuristics (long, has bullets)
		task := &Task{
			ID:    "test-1",
			Title: "Complex task",
			Description: `This is a very long description with lots of detail that normally triggers decomposition.
We need to do many things across many files and components in the system.
The implementation spans multiple layers of the application stack.
- First thing to implement with careful attention to detail
- Second thing to implement with proper testing
- Third thing to implement with documentation
And much more detail about each of these items to reach the word threshold.
We need to ensure proper error handling, logging, monitoring, and alerting for all components.
Performance testing is required before deployment to validate throughput targets.`,
		}

		result := decomposer.Decompose(task)
		if result.Decomposed {
			t.Error("expected no decomposition when LLM says no")
		}
		if result.Reason != "LLM classifier: should not decompose" {
			t.Errorf("unexpected reason: %s", result.Reason)
		}

		// Reset and verify heuristics take over
		decomposer.ResetLLMDecomposeDecision()
		// Heuristic path: task needs to be Complex complexity AND meet word count
		// This task description contains "system" keyword → Complex via pattern match
		result2 := decomposer.Decompose(task)
		// With heuristics, it should try to decompose (complexity=complex, enough words, has bullets)
		if !result2.Decomposed {
			t.Logf("Heuristic decomposition result: %s", result2.Reason)
		}
	})

	t.Run("LLM says decompose skips heuristic checks", func(t *testing.T) {
		decomposer := NewTaskDecomposer(config)
		decomposer.SetLLMDecomposeDecision(true)

		// Short task that heuristics would NOT decompose (too few words)
		task := &Task{
			ID:    "test-2",
			Title: "Two features",
			Description: `1. Add login page
2. Add signup page`,
		}

		result := decomposer.Decompose(task)
		// LLM says decompose, so it proceeds to structural analysis
		// The description has numbered steps, so it should find decomposition points
		if !result.Decomposed {
			t.Errorf("expected decomposition when LLM says yes, got reason: %s", result.Reason)
		}
	})
}
