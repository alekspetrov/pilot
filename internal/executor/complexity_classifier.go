package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ComplexityClassification is the structured result from LLM complexity analysis.
type ComplexityClassification struct {
	// Complexity is the detected complexity level.
	Complexity Complexity `json:"complexity"`

	// ShouldDecompose indicates whether the task should be split into sub-issues.
	// True means the task contains multiple independent work items needing separate PRs.
	// False means the task is a single piece of work (even if detailed/long).
	ShouldDecompose bool `json:"should_decompose"`

	// Reason explains the classification decision.
	Reason string `json:"reason"`
}

// ComplexityClassifier uses an LLM (Haiku) to classify task complexity.
// This replaces the word-count heuristic with semantic understanding:
// a 500-word issue can be trivial, a 30-word issue can be complex.
type ComplexityClassifier struct {
	apiKey     string
	apiURL     string
	model      string
	httpClient *http.Client
}

// ComplexityClassifierConfig configures the LLM-based complexity classifier.
//
// Example YAML configuration:
//
//	executor:
//	  complexity_classifier:
//	    enabled: true
//	    model: "claude-haiku-4-5-20251001"
//	    timeout: "10s"
type ComplexityClassifierConfig struct {
	// Enabled controls whether LLM complexity classification is active.
	// When false (default), falls back to heuristic detection.
	Enabled bool `yaml:"enabled"`

	// Model is the model for complexity classification.
	// Default: "claude-haiku-4-5-20251001"
	Model string `yaml:"model,omitempty"`

	// Timeout is the HTTP request timeout for the classifier.
	// Default: "10s"
	Timeout string `yaml:"timeout,omitempty"`
}

// DefaultComplexityClassifierConfig returns default classifier configuration.
func DefaultComplexityClassifierConfig() *ComplexityClassifierConfig {
	return &ComplexityClassifierConfig{
		Enabled: false,
		Model:   "claude-haiku-4-5-20251001",
		Timeout: "10s",
	}
}

// NewComplexityClassifier creates a new LLM-based complexity classifier.
func NewComplexityClassifier(apiKey string) *ComplexityClassifier {
	return &ComplexityClassifier{
		apiKey: apiKey,
		apiURL: "https://api.anthropic.com/v1/messages",
		model:  "claude-haiku-4-5-20251001",
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// newComplexityClassifierWithURL creates a classifier with a custom API URL for testing.
func newComplexityClassifierWithURL(apiKey, url string) *ComplexityClassifier {
	c := NewComplexityClassifier(apiKey)
	c.apiURL = url
	return c
}

const complexityClassifierSystemPrompt = `You are a task complexity classifier for a software development automation system. Given a task title and description, classify the task complexity and determine if it should be decomposed into sub-issues.

Complexity levels:
- "trivial": Minimal changes — typos, log additions, renames, comment updates, formatting fixes. Single-line or few-line changes.
- "simple": Small focused changes — add a field, small bug fix, update a config value, add a test case. One file or a few lines across files.
- "medium": Standard feature work — new endpoint, new component, integration, moderate refactoring. Multiple files but a coherent single task.
- "complex": Architectural changes — major refactors, migrations, system redesigns, cross-cutting concerns. Significant multi-file changes.
- "epic": Too large for a single execution cycle — requires multiple independent sub-tasks, phases, or milestones.

Decomposition rules:
- should_decompose=true ONLY when the task contains multiple INDEPENDENT work items that would each produce a separate PR.
- should_decompose=false when the task is a single coherent piece of work, even if the description is long and detailed.
- Detailed implementation instructions (numbered steps, bullet points explaining HOW to do ONE thing) are NOT decomposition candidates.
- A list of acceptance criteria for ONE feature is NOT a decomposition candidate.
- Multiple unrelated features, separate components, or independent work streams ARE decomposition candidates.

Respond with ONLY a valid JSON object. No markdown, no explanation. Example:
{"complexity":"medium","should_decompose":false,"reason":"Single endpoint with validation and tests"}`

// maxTaskCharsDefault is the maximum combined title+description chars sent to the LLM.
const maxTaskCharsDefault = 4000

// Classify sends a task to the LLM and returns a structured complexity classification.
func (c *ComplexityClassifier) Classify(ctx context.Context, title, description string) (*ComplexityClassification, error) {
	if title == "" && description == "" {
		return &ComplexityClassification{
			Complexity:      ComplexityMedium,
			ShouldDecompose: false,
			Reason:          "empty task",
		}, nil
	}

	// Truncate to prevent token overflow
	combined := fmt.Sprintf("## Title\n%s\n\n## Description\n%s", title, description)
	if len(combined) > maxTaskCharsDefault {
		combined = combined[:maxTaskCharsDefault] + "\n...[truncated]"
	}

	reqBody := haikuRequest{
		Model:     c.model,
		MaxTokens: 256,
		System:    complexityClassifierSystemPrompt,
		Messages: []haikuMessage{
			{Role: "user", Content: combined},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var apiResp haikuResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(apiResp.Content) == 0 || apiResp.Content[0].Text == "" {
		return nil, fmt.Errorf("empty response from API")
	}

	return parseClassificationResponse(apiResp.Content[0].Text)
}

// parseClassificationResponse extracts the structured classification from the LLM response.
func parseClassificationResponse(text string) (*ComplexityClassification, error) {
	// Trim any surrounding whitespace/markdown
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var result ComplexityClassification
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("parse classification JSON: %w (response: %s)", err, truncateForError(text, 200))
	}

	// Validate complexity value
	switch result.Complexity {
	case ComplexityTrivial, ComplexitySimple, ComplexityMedium, ComplexityComplex, ComplexityEpic:
		// valid
	default:
		return nil, fmt.Errorf("invalid complexity value: %q", result.Complexity)
	}

	return &result, nil
}

// truncateForError truncates text for inclusion in error messages.
func truncateForError(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
