package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// SubtaskParser uses Claude Haiku via the Anthropic API to parse
// planning output into structured subtasks. It replaces regex-based
// parsing with LLM-based extraction for better accuracy.
type SubtaskParser struct {
	apiKey     string
	httpClient *http.Client
	model      string
	log        *slog.Logger
}

// subtaskParserResponse is the expected JSON structure from Haiku.
type subtaskParserResponse struct {
	Subtasks []struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Order       int    `json:"order"`
	} `json:"subtasks"`
}

// NewSubtaskParser creates a SubtaskParser that calls the Anthropic API.
// Returns nil if ANTHROPIC_API_KEY is not set.
func NewSubtaskParser(log *slog.Logger) *SubtaskParser {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil
	}
	return &SubtaskParser{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		model: "claude-haiku-4-5-20251001",
		log:   log,
	}
}

// Parse sends the raw planning output to Haiku and returns structured subtasks.
func (p *SubtaskParser) Parse(ctx context.Context, output string) ([]PlannedSubtask, error) {
	systemPrompt := `You are a structured data extractor. Parse the following planning output into a JSON array of subtasks.

Return ONLY valid JSON in this exact format:
{"subtasks": [{"title": "...", "description": "...", "order": 1}, ...]}

Rules:
- Extract the numbered subtasks from the planning output
- "title" is the short task name (no markdown, no bold markers)
- "description" is the detailed explanation of what needs to be done
- "order" is the sequential number (1-indexed)
- Preserve the original ordering
- Do NOT add subtasks that aren't in the input`

	messages := []map[string]string{
		{
			"role":    "user",
			"content": fmt.Sprintf("Parse this planning output into structured subtasks:\n\n%s", output),
		},
	}

	requestBody := map[string]interface{}{
		"model":      p.model,
		"max_tokens": 2048,
		"system":     systemPrompt,
		"messages":   messages,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var apiResp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(apiResp.Content) == 0 {
		return nil, fmt.Errorf("empty response from API")
	}

	var parsed subtaskParserResponse
	if err := json.Unmarshal([]byte(apiResp.Content[0].Text), &parsed); err != nil {
		return nil, fmt.Errorf("parse subtask JSON: %w (raw: %s)", err, apiResp.Content[0].Text)
	}

	if len(parsed.Subtasks) == 0 {
		return nil, fmt.Errorf("no subtasks in parsed response")
	}

	subtasks := make([]PlannedSubtask, len(parsed.Subtasks))
	for i, s := range parsed.Subtasks {
		subtasks[i] = PlannedSubtask{
			Title:       s.Title,
			Description: s.Description,
			Order:       s.Order,
		}
	}

	return subtasks, nil
}
