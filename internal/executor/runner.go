package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// StreamEvent represents a Claude Code stream-json event
type StreamEvent struct {
	Type          string           `json:"type"`
	Subtype       string           `json:"subtype,omitempty"`
	Message       *AssistantMsg    `json:"message,omitempty"`
	Result        string           `json:"result,omitempty"`
	IsError       bool             `json:"is_error,omitempty"`
	DurationMS    int              `json:"duration_ms,omitempty"`
	NumTurns      int              `json:"num_turns,omitempty"`
	ToolUseResult json.RawMessage  `json:"tool_use_result,omitempty"`
}

// AssistantMsg represents the message field in assistant events
type AssistantMsg struct {
	Content []ContentBlock `json:"content"`
}

// ContentBlock represents content in assistant messages
type ContentBlock struct {
	Type  string                 `json:"type"`
	Text  string                 `json:"text,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`
}

// ToolResultContent represents tool result in user events
type ToolResultContent struct {
	ToolUseID string `json:"tool_use_id"`
	Type      string `json:"type"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error"`
}

// progressState tracks execution phase for compact progress reporting
type progressState struct {
	phase      string // Current phase: Exploring, Implementing, Testing, Committing
	filesRead  int    // Count of files read
	filesWrite int    // Count of files written
	commands   int    // Count of bash commands
}

// Task represents a task to be executed
type Task struct {
	ID          string
	Title       string
	Description string
	Priority    int
	ProjectPath string
	Branch      string
	Verbose     bool // Stream Claude Code output to console
}

// ExecutionResult represents the result of task execution
type ExecutionResult struct {
	TaskID    string
	Success   bool
	Output    string
	Error     string
	Duration  time.Duration
	PRUrl     string
	CommitSHA string
}

// ProgressCallback is called during execution with progress updates
type ProgressCallback func(taskID string, phase string, progress int, message string)

// Runner executes tasks using Claude Code
type Runner struct {
	onProgress ProgressCallback
	mu         sync.Mutex
	running    map[string]*exec.Cmd
}

// NewRunner creates a new executor runner
func NewRunner() *Runner {
	return &Runner{
		running: make(map[string]*exec.Cmd),
	}
}

// OnProgress sets the progress callback
func (r *Runner) OnProgress(callback ProgressCallback) {
	r.onProgress = callback
}

// Execute runs a task using Claude Code
func (r *Runner) Execute(ctx context.Context, task *Task) (*ExecutionResult, error) {
	start := time.Now()

	// Build the prompt for Claude Code
	prompt := r.BuildPrompt(task)

	// Create the Claude Code command with stream-json output
	// --verbose is required for stream-json to work
	// --dangerously-skip-permissions allows autonomous execution without approval prompts
	cmd := exec.CommandContext(ctx, "claude", "-p", prompt,
		"--verbose",
		"--output-format", "stream-json",
		"--dangerously-skip-permissions",
	)
	cmd.Dir = task.ProjectPath

	// Track the running command
	r.mu.Lock()
	r.running[task.ID] = cmd
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		delete(r.running, task.ID)
		r.mu.Unlock()
	}()

	// Report start
	r.reportProgress(task.ID, "Starting", 0, "Initializing Claude Code...")

	// Create pipes for output
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start Claude Code: %w", err)
	}

	// State for tracking progress and result
	var finalResult string
	var finalError string
	state := &progressState{phase: "Starting"}
	var wg sync.WaitGroup

	// Read stdout (stream-json events)
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		// Increase buffer size for large JSON events
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			if task.Verbose {
				fmt.Printf("   %s\n", line)
			}

			// Parse JSON event
			result, errMsg := r.parseStreamEvent(task.ID, line, state)
			if result != "" {
				finalResult = result
			}
			if errMsg != "" {
				finalError = errMsg
			}
		}
	}()

	// Read stderr
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		var errorBuilder strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			errorBuilder.WriteString(line + "\n")
			if task.Verbose {
				fmt.Printf("   [err] %s\n", line)
			}
		}
		if errorBuilder.Len() > 0 && finalError == "" {
			finalError = errorBuilder.String()
		}
	}()

	// Wait for output readers
	wg.Wait()

	// Wait for command to complete
	err = cmd.Wait()
	duration := time.Since(start)

	result := &ExecutionResult{
		TaskID:   task.ID,
		Output:   finalResult,
		Error:    finalError,
		Duration: duration,
	}

	if err != nil {
		result.Success = false
		if result.Error == "" {
			result.Error = err.Error()
		}
		r.reportProgress(task.ID, "Failed", 100, result.Error)
	} else {
		result.Success = true
		r.reportProgress(task.ID, "Completed", 100, "Task completed successfully")
	}

	return result, nil
}

// Cancel cancels a running task
func (r *Runner) Cancel(taskID string) error {
	r.mu.Lock()
	cmd, ok := r.running[taskID]
	r.mu.Unlock()

	if !ok {
		return fmt.Errorf("task %s is not running", taskID)
	}

	return cmd.Process.Kill()
}

// IsRunning checks if a task is running
func (r *Runner) IsRunning(taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.running[taskID]
	return ok
}

// BuildPrompt builds the prompt for Claude Code (exported for dry-run preview)
func (r *Runner) BuildPrompt(task *Task) string {
	var sb strings.Builder

	// Check if project has Navigator initialized
	agentDir := filepath.Join(task.ProjectPath, ".agent")
	hasNavigator := false
	if _, err := os.Stat(agentDir); err == nil {
		hasNavigator = true
	}

	// Navigator-aware prompt structure
	if hasNavigator {
		// Navigator handles workflow, autonomous completion, and documentation
		sb.WriteString("Start my Navigator session.\n\n")
		sb.WriteString(fmt.Sprintf("## Task: %s\n\n", task.ID))
		sb.WriteString(fmt.Sprintf("%s\n\n", task.Description))

		if task.Branch != "" {
			sb.WriteString(fmt.Sprintf("Create branch `%s` before starting.\n\n", task.Branch))
		}

		// Navigator will handle: workflow check, complexity detection, autonomous completion
		sb.WriteString("Run until done. Use Navigator's autonomous completion protocol.\n")
	} else {
		// Non-Navigator project: explicit instructions with strict constraints
		sb.WriteString(fmt.Sprintf("## Task: %s\n\n", task.ID))
		sb.WriteString(fmt.Sprintf("%s\n\n", task.Description))

		sb.WriteString("## Constraints\n\n")
		sb.WriteString("- ONLY create files explicitly mentioned in the task\n")
		sb.WriteString("- Do NOT create additional files, tests, configs, or dependencies\n")
		sb.WriteString("- Do NOT modify existing files unless explicitly requested\n")
		sb.WriteString("- If task specifies a file type (e.g., .py), use ONLY that type\n")
		sb.WriteString("- Do NOT add package.json, requirements.txt, or build configs\n")
		sb.WriteString("- Keep implementation minimal and focused\n\n")

		sb.WriteString("## Instructions\n\n")

		if task.Branch != "" {
			sb.WriteString(fmt.Sprintf("1. Create git branch: `%s`\n", task.Branch))
		} else {
			sb.WriteString("1. Work on current branch (no new branch)\n")
		}

		sb.WriteString("2. Implement EXACTLY what is requested - nothing more, nothing less\n")
		sb.WriteString("3. Commit with format: `type(scope): description`\n")
		sb.WriteString("\nWork autonomously. Do not ask for confirmation.\n")
	}

	return sb.String()
}

// parseStreamEvent parses a stream-json event and reports progress
// Returns (finalResult, errorMessage) - non-empty when task completes
func (r *Runner) parseStreamEvent(taskID, line string, state *progressState) (string, string) {
	var event StreamEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		// Not valid JSON, skip
		return "", ""
	}

	switch event.Type {
	case "system":
		if event.Subtype == "init" {
			r.reportProgress(taskID, "🚀 Started", 5, "Claude Code initialized")
		}

	case "assistant":
		if event.Message != nil {
			for _, block := range event.Message.Content {
				if block.Type == "tool_use" {
					r.handleToolUse(taskID, block.Name, block.Input, state)
				}
				// Skip "Thinking" messages - too noisy
			}
		}

	case "user":
		// Tool results - skip error reporting (too noisy)
		// Errors will show in final result if task fails

	case "result":
		if event.IsError {
			return "", event.Result
		}
		return event.Result, ""
	}

	return "", ""
}

// handleToolUse processes tool usage and updates phase-based progress
func (r *Runner) handleToolUse(taskID, toolName string, input map[string]interface{}, state *progressState) {
	var newPhase string
	var progress int
	var message string

	switch toolName {
	case "Read", "Glob", "Grep":
		state.filesRead++
		if state.phase != "Exploring" {
			newPhase = "Exploring"
			progress = 15
			message = "Analyzing codebase..."
		}

	case "Write", "Edit":
		state.filesWrite++
		if state.phase != "Implementing" || state.filesWrite == 1 {
			newPhase = "Implementing"
			progress = 40 + min(state.filesWrite*5, 30)
			if fp, ok := input["file_path"].(string); ok {
				message = fmt.Sprintf("Creating %s", filepath.Base(fp))
			} else {
				message = "Writing files..."
			}
		}

	case "Bash":
		state.commands++
		if cmd, ok := input["command"].(string); ok {
			cmdLower := strings.ToLower(cmd)

			// Detect phase from command (order matters - check specific patterns first)
			if strings.Contains(cmdLower, "git commit") {
				if state.phase != "Committing" {
					newPhase = "Committing"
					progress = 90
					message = "Committing changes..."
				}
			} else if strings.Contains(cmdLower, "git checkout") || strings.Contains(cmdLower, "git branch") {
				if state.phase != "Branching" {
					newPhase = "Branching"
					progress = 10
					message = "Setting up branch..."
				}
			} else if strings.Contains(cmdLower, "pytest") || strings.Contains(cmdLower, "jest") ||
				strings.Contains(cmdLower, "go test") || strings.Contains(cmdLower, "npm test") ||
				strings.Contains(cmdLower, "make test") {
				if state.phase != "Testing" {
					newPhase = "Testing"
					progress = 75
					message = "Running tests..."
				}
			} else if strings.Contains(cmdLower, "npm install") || strings.Contains(cmdLower, "pip install") ||
				strings.Contains(cmdLower, "go mod") {
				if state.phase != "Installing" {
					newPhase = "Installing"
					progress = 30
					message = "Installing dependencies..."
				}
			}
			// Skip other bash commands - too noisy
		}

	case "Task":
		// Sub-agent spawned
		if state.phase != "Delegating" {
			newPhase = "Delegating"
			progress = 50
			if desc, ok := input["description"].(string); ok {
				message = fmt.Sprintf("Spawning agent: %s", truncateText(desc, 40))
			} else {
				message = "Running sub-task..."
			}
		}
	}

	// Only report if phase changed
	if newPhase != "" && newPhase != state.phase {
		state.phase = newPhase
		r.reportProgress(taskID, newPhase, progress, message)
	}
}

// formatToolMessage creates a human-readable message for tool usage
func formatToolMessage(toolName string, input map[string]interface{}) string {
	switch toolName {
	case "Write":
		if fp, ok := input["file_path"].(string); ok {
			return fmt.Sprintf("Writing %s", filepath.Base(fp))
		}
	case "Edit":
		if fp, ok := input["file_path"].(string); ok {
			return fmt.Sprintf("Editing %s", filepath.Base(fp))
		}
	case "Read":
		if fp, ok := input["file_path"].(string); ok {
			return fmt.Sprintf("Reading %s", filepath.Base(fp))
		}
	case "Bash":
		if cmd, ok := input["command"].(string); ok {
			return fmt.Sprintf("Running: %s", truncateText(cmd, 40))
		}
	case "Glob":
		if pattern, ok := input["pattern"].(string); ok {
			return fmt.Sprintf("Searching: %s", pattern)
		}
	case "Grep":
		if pattern, ok := input["pattern"].(string); ok {
			return fmt.Sprintf("Grep: %s", truncateText(pattern, 30))
		}
	case "Task":
		if desc, ok := input["description"].(string); ok {
			return fmt.Sprintf("Spawning: %s", truncateText(desc, 40))
		}
	}
	return fmt.Sprintf("Using %s", toolName)
}

// truncateText truncates text to maxLen and adds ellipsis
func truncateText(text string, maxLen int) string {
	// Remove newlines for display
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.TrimSpace(text)
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen-3] + "..."
}

// min returns the smaller of two ints
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}


// reportProgress sends a progress update
func (r *Runner) reportProgress(taskID, phase string, progress int, message string) {
	if r.onProgress != nil {
		r.onProgress(taskID, phase, progress, message)
	}
}
