package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const (
	// haikuModel is the Claude Haiku model used for fast JSON extraction.
	haikuModel = "claude-haiku-4-5-20251001"

	// anthropicMessagesURL is the Anthropic Messages API endpoint.
	anthropicMessagesURL = "https://api.anthropic.com/v1/messages"

	// anthropicAPIVersion is the API version header value.
	anthropicAPIVersion = "2023-06-01"

	// haikuParserTimeout is the HTTP client timeout for Haiku calls.
	haikuParserTimeout = 10 * time.Second

	// haikuMaxTokens is the max output tokens for the Haiku response.
	haikuMaxTokens = 1000
)

// haikuSystemPrompt instructs Haiku to extract subtasks as a JSON array.
const haikuSystemPrompt = `You are a JSON extraction assistant. Given planning output that describes subtasks for a software project, extract the subtasks into a JSON array.

Each subtask must have these fields:
- "title": short title of the subtask (string)
- "description": detailed description of what needs to be done (string)
- "order": execution order, 1-indexed (integer)
- "depends_on": array of order numbers this subtask depends on (integer array, empty if none)

Return ONLY a valid JSON array, no other text. Example:
[
  {"title": "Add config struct", "description": "Create the configuration struct with YAML tags", "order": 1, "depends_on": []},
  {"title": "Implement handler", "description": "Wire the handler to the config", "order": 2, "depends_on": [1]}
]`

// SubtaskParser uses Claude Haiku to parse planning output into structured subtasks.
// It holds an API key and HTTP client, and exposes a single Parse method.
type SubtaskParser struct {
	apiKey     string
	httpClient *http.Client
}

// NewSubtaskParser creates a SubtaskParser. The API key is resolved from the provided
// config value first, falling back to the ANTHROPIC_API_KEY environment variable.
func NewSubtaskParser(configAPIKey string) (*SubtaskParser, error) {
	apiKey := configAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("no Anthropic API key: set config field or ANTHROPIC_API_KEY env var")
	}

	return &SubtaskParser{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: haikuParserTimeout,
		},
	}, nil
}

// haikuRequest is the Anthropic Messages API request body.
type haikuRequest struct {
	Model        string            `json:"model"`
	MaxTokens    int               `json:"max_tokens"`
	System       string            `json:"system"`
	Messages     []haikuMessage    `json:"messages"`
	OutputConfig haikuOutputConfig `json:"output_config"`
}

// haikuMessage is a single message in the Anthropic API request.
type haikuMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// haikuOutputConfig controls output effort level.
type haikuOutputConfig struct {
	Effort string `json:"effort"`
}

// haikuResponse is the Anthropic Messages API response body.
type haikuResponse struct {
	Content []haikuContentBlock `json:"content"`
}

// haikuContentBlock is a single content block in the API response.
type haikuContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// haikuSubtask is the JSON schema returned by Haiku for each subtask.
type haikuSubtask struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Order       int    `json:"order"`
	DependsOn   []int  `json:"depends_on"`
}

// Parse sends the planning output to Claude Haiku and returns structured subtasks.
// Returns an error if the input is empty, the API call fails, or the response
// cannot be parsed into valid subtasks.
func (p *SubtaskParser) Parse(ctx context.Context, planningOutput string) ([]PlannedSubtask, error) {
	if planningOutput == "" {
		return nil, fmt.Errorf("empty planning output")
	}

	reqBody := haikuRequest{
		Model:     haikuModel,
		MaxTokens: haikuMaxTokens,
		System:    haikuSystemPrompt,
		Messages: []haikuMessage{
			{Role: "user", Content: planningOutput},
		},
		OutputConfig: haikuOutputConfig{Effort: "low"},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", anthropicMessagesURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := p.httpClient.Do(req)
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

	if len(apiResp.Content) == 0 {
		return nil, fmt.Errorf("empty response from Haiku API")
	}

	// Find the first text block
	var text string
	for _, block := range apiResp.Content {
		if block.Type == "text" {
			text = block.Text
			break
		}
	}
	if text == "" {
		return nil, fmt.Errorf("no text content in Haiku response")
	}

	// Parse the JSON array
	var raw []haikuSubtask
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("parse subtasks JSON: %w (response: %s)", err, text)
	}

	if len(raw) == 0 {
		return nil, fmt.Errorf("Haiku returned empty subtask array")
	}

	// Convert to PlannedSubtask
	subtasks := make([]PlannedSubtask, len(raw))
	for i, r := range raw {
		subtasks[i] = PlannedSubtask{
			Title:       r.Title,
			Description: r.Description,
			Order:       r.Order,
			DependsOn:   r.DependsOn,
		}
	}

	return subtasks, nil
}
