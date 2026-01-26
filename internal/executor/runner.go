package executor

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
			if task.Verbose {
				fmt.Printf("   %s\n", line)
			}
			r.parseProgressFromOutput(task.ID, line)
		}
	}()

	// Read stderr
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			errorBuilder.WriteString(line + "\n")
			if task.Verbose {
				fmt.Printf("   [err] %s\n", line)
			}
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
		// Non-Navigator project: explicit instructions needed
		sb.WriteString(fmt.Sprintf("## Task: %s\n\n", task.ID))
		sb.WriteString(fmt.Sprintf("%s\n\n", task.Description))
		sb.WriteString("## Instructions\n\n")

		if task.Branch != "" {
			sb.WriteString(fmt.Sprintf("1. Create git branch: `%s`\n", task.Branch))
		} else {
			sb.WriteString("1. Work on current branch (no new branch)\n")
		}

		sb.WriteString("2. Implement exactly what is requested - nothing more\n")
		sb.WriteString("3. Write tests if appropriate\n")
		sb.WriteString("4. Commit with format: `type(scope): description`\n")
		sb.WriteString("\nWork autonomously. Do not ask for confirmation.\n")
	}

	return sb.String()
}

// parseProgressFromOutput attempts to parse progress from Claude Code output
func (r *Runner) parseProgressFromOutput(taskID, line string) {
	// Navigator WORKFLOW CHECK block
	if strings.Contains(line, "Mode:") && (strings.Contains(line, "LOOP") || strings.Contains(line, "TASK") || strings.Contains(line, "DIRECT")) {
		if strings.Contains(line, "LOOP") {
			r.reportProgress(taskID, "Loop Mode", 10, "Navigator loop mode active")
		} else if strings.Contains(line, "TASK") {
			r.reportProgress(taskID, "Task Mode", 10, "Navigator task mode active")
		}
	}

	// Navigator NAVIGATOR_STATUS block phase detection
	if strings.Contains(line, "Phase:") {
		parts := strings.Split(line, "Phase:")
		if len(parts) > 1 {
			phase := strings.TrimSpace(parts[1])
			// Handle "RESEARCH → PLAN" format
			if strings.Contains(phase, "→") {
				phaseParts := strings.Split(phase, "→")
				if len(phaseParts) > 1 {
					phase = strings.TrimSpace(phaseParts[1])
				}
			}
			r.reportProgress(taskID, phase, r.phaseToProgress(phase), "")
		}
	}

	// Navigator Progress percentage
	if strings.Contains(line, "Progress:") {
		parts := strings.Split(line, "Progress:")
		if len(parts) > 1 {
			progressStr := strings.TrimSpace(parts[1])
			progressStr = strings.TrimSuffix(progressStr, "%")
			var progress int
			fmt.Sscanf(progressStr, "%d", &progress)
			if progress > 0 {
				r.reportProgress(taskID, "Working", progress, "")
			}
		}
	}

	// Navigator EXIT_SIGNAL detection
	if strings.Contains(line, "EXIT_SIGNAL: true") {
		r.reportProgress(taskID, "Completing", 95, "Navigator exit signal received")
	}

	// Commit detection
	if strings.Contains(line, "git commit") || strings.Contains(line, "committed") {
		r.reportProgress(taskID, "Committed", 90, "Changes committed")
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
