package executor

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Task represents a task to be executed
type Task struct {
	ID          string
	Title       string
	Description string
	Priority    int
	ProjectPath string
	Branch      string
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
	prompt := r.buildPrompt(task)

	// Create the Claude Code command
	cmd := exec.CommandContext(ctx, "claude", "-p", prompt)
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

	// Collect output
	var outputBuilder strings.Builder
	var errorBuilder strings.Builder
	var wg sync.WaitGroup

	// Read stdout
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			outputBuilder.WriteString(line + "\n")
			r.parseProgressFromOutput(task.ID, line)
		}
	}()

	// Read stderr
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			errorBuilder.WriteString(scanner.Text() + "\n")
		}
	}()

	// Wait for output readers
	wg.Wait()

	// Wait for command to complete
	err = cmd.Wait()
	duration := time.Since(start)

	result := &ExecutionResult{
		TaskID:   task.ID,
		Output:   outputBuilder.String(),
		Error:    errorBuilder.String(),
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

// buildPrompt builds the prompt for Claude Code
func (r *Runner) buildPrompt(task *Task) string {
	var branchInstr string
	if task.Branch != "" {
		branchInstr = fmt.Sprintf("1. Create a new git branch: %s\n", task.Branch)
	} else {
		branchInstr = "1. Work on the current branch (no new branch)\n"
	}

	prompt := fmt.Sprintf(`Implement the following task:

Task ID: %s

Description:
%s

Instructions:
%s2. Implement the feature following project patterns
3. Write tests if appropriate
4. Commit changes with conventional commit format: type(scope): description

Work autonomously - implement and commit without prompting for confirmation.
`, task.ID, task.Description, branchInstr)

	return prompt
}

// parseProgressFromOutput attempts to parse progress from Claude Code output
func (r *Runner) parseProgressFromOutput(taskID, line string) {
	// Look for Navigator status blocks or phase transitions
	if strings.Contains(line, "PHASE:") {
		parts := strings.Split(line, "PHASE:")
		if len(parts) > 1 {
			phase := strings.TrimSpace(parts[1])
			r.reportProgress(taskID, phase, r.phaseToProgress(phase), "")
		}
	} else if strings.Contains(line, "Progress:") {
		// Parse progress percentage
		// Format: "Progress: 75%"
		parts := strings.Split(line, "Progress:")
		if len(parts) > 1 {
			progressStr := strings.TrimSpace(parts[1])
			progressStr = strings.TrimSuffix(progressStr, "%")
			var progress int
			fmt.Sscanf(progressStr, "%d", &progress)
			r.reportProgress(taskID, "Working", progress, "")
		}
	}
}

// phaseToProgress converts a phase name to approximate progress percentage
func (r *Runner) phaseToProgress(phase string) int {
	switch strings.ToUpper(phase) {
	case "INIT", "RESEARCH":
		return 10
	case "PLAN":
		return 25
	case "IMPL", "IMPLEMENTATION":
		return 50
	case "VERIFY", "TEST":
		return 75
	case "COMPLETE", "DONE":
		return 100
	default:
		return 50
	}
}

// reportProgress sends a progress update
func (r *Runner) reportProgress(taskID, phase string, progress int, message string) {
	if r.onProgress != nil {
		r.onProgress(taskID, phase, progress, message)
	}
}
