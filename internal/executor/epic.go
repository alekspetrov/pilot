package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// EpicPlan represents the result of planning an epic task.
// Contains the parent task and the subtasks derived from Claude Code's planning output.
type EpicPlan struct {
	// ParentTask is the original epic task that was planned
	ParentTask *Task

	// Subtasks are the sequential subtasks derived from the planning phase
	Subtasks []PlannedSubtask

	// TotalEffort is the estimated total effort (if provided by the planner)
	TotalEffort string

	// PlanOutput is the raw Claude Code output for reference
	PlanOutput string
}

// PlannedSubtask represents a single subtask derived from epic planning.
type PlannedSubtask struct {
	// Title is the short title of the subtask
	Title string

	// Description is the detailed description of what needs to be done
	Description string

	// Order is the execution order (1-indexed)
	Order int

	// DependsOn contains the orders of subtasks this depends on
	DependsOn []int
}

// CreatedIssue represents a GitHub issue created from a planned subtask.
type CreatedIssue struct {
	// Number is the GitHub issue number
	Number int

	// URL is the full GitHub issue URL
	URL string

	// Subtask is the planned subtask this issue was created from
	Subtask PlannedSubtask
}

// numberedListRegex matches numbered patterns: "1. ", "1) ", "Step 1:", "Phase 1:", "**1.", etc.
// Allows optional markdown bold markers (**) before the number (GH-490 fix).
// Also handles markdown heading prefixes (### 1.), dash/asterisk bullets (- 1., * 1.),
// and combinations like "- **1. Title**" or "### Step 1: Title" (GH-542 fix).
// Used by parseSubtasks as the regex fallback in the parsing pipeline:
//
//	PlanEpic → parseSubtasksWithFallback → SubtaskParser (Haiku API) → parseSubtasks (regex)
var numberedListRegex = regexp.MustCompile(`(?mi)^(?:\s*)(?:#{1,6}\s+)?(?:[-*]\s+)?(?:\*{0,2})(?:step|phase|task)?\s*(\d+)[.):]\s*(.+)`)

// PlanEpic runs Claude Code in planning mode to break an epic into subtasks.
// Returns an EpicPlan with 3-5 sequential subtasks.
func (r *Runner) PlanEpic(ctx context.Context, task *Task) (*EpicPlan, error) {
	// Build planning prompt
	prompt := buildPlanningPrompt(task)

	// Get claude command from config or use default
	claudeCmd := "claude"
	if r.config != nil && r.config.ClaudeCode != nil && r.config.ClaudeCode.Command != "" {
		claudeCmd = r.config.ClaudeCode.Command
	}

	// Run Claude Code with --print flag for planning
	args := []string{"--print", "-p", prompt}

	cmd := exec.CommandContext(ctx, claudeCmd, args...)

	// Set working directory if specified
	if task.ProjectPath != "" {
		cmd.Dir = task.ProjectPath
	}

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	r.log.Debug("Running Claude Code planning",
		"task_id", task.ID,
		"command", claudeCmd,
		"args", args,
	)

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("claude planning failed: %w (stderr: %s)", err, stderr.String())
	}

	output := stdout.String()
	if output == "" {
		return nil, fmt.Errorf("claude planning returned empty output")
	}

	// Parse subtasks: tries Haiku structured extraction first, falls back to regex.
	// See parseSubtasksWithFallback in subtask_parser.go for the fallback chain.
	subtasks := parseSubtasksWithFallback(r.subtaskParser, output)
	if len(subtasks) == 0 {
		return nil, fmt.Errorf("no subtasks found in planning output")
	}

	return &EpicPlan{
		ParentTask: task,
		Subtasks:   subtasks,
		PlanOutput: output,
	}, nil
}

// buildPlanningPrompt creates the prompt for epic planning.
func buildPlanningPrompt(task *Task) string {
	var sb strings.Builder

	sb.WriteString("You are a software architect planning an implementation.\n\n")
	sb.WriteString("Break down this epic task into 3-5 sequential subtasks that can each be completed independently.\n")
	sb.WriteString("Each subtask should be a concrete, implementable unit of work.\n\n")

	sb.WriteString("## Task to Plan\n\n")
	sb.WriteString(fmt.Sprintf("**Title:** %s\n\n", task.Title))
	if task.Description != "" {
		sb.WriteString(fmt.Sprintf("**Description:**\n%s\n\n", task.Description))
	}

	sb.WriteString("## Output Format\n\n")
	sb.WriteString("List each subtask with a number, title, and description:\n\n")
	sb.WriteString("1. **Subtask title** - Description of what needs to be done\n")
	sb.WriteString("2. **Next subtask** - Its description\n")
	sb.WriteString("...\n\n")

	sb.WriteString("Focus on:\n")
	sb.WriteString("- Clear boundaries between subtasks\n")
	sb.WriteString("- Logical ordering (dependencies flow naturally)\n")
	sb.WriteString("- Each subtask should be testable/verifiable\n")
	sb.WriteString("- Include any setup/infrastructure subtasks first\n")

	return sb.String()
}

// parseSubtasks extracts subtasks from Claude's planning output using regex.
// This is the fallback parser when Haiku API is unavailable (see subtask_parser.go).
// Looks for numbered patterns: "1. Title - Description", "Step 1: Title", "**1. Title**"
func parseSubtasks(output string) []PlannedSubtask {
	var subtasks []PlannedSubtask
	seenOrders := make(map[int]bool)

	scanner := bufio.NewScanner(strings.NewReader(output))
	var currentSubtask *PlannedSubtask
	var descriptionLines []string

	for scanner.Scan() {
		line := scanner.Text()

		// Try to match numbered list patterns
		matches := numberedListRegex.FindStringSubmatch(line)
		if len(matches) >= 3 {
			// Save previous subtask if exists
			if currentSubtask != nil {
				finalizeSubtask(currentSubtask, descriptionLines)
				if currentSubtask.Title != "" && !seenOrders[currentSubtask.Order] {
					subtasks = append(subtasks, *currentSubtask)
					seenOrders[currentSubtask.Order] = true
				}
			}

			order := 0
			_, _ = fmt.Sscanf(matches[1], "%d", &order)

			// Extract title and possibly inline description
			titleAndDesc := strings.TrimSpace(matches[2])
			title, desc := splitTitleDescription(titleAndDesc)

			currentSubtask = &PlannedSubtask{
				Title:       title,
				Description: desc,
				Order:       order,
			}
			descriptionLines = nil
			continue
		}

		// Accumulate description lines for current subtask
		if currentSubtask != nil && strings.TrimSpace(line) != "" {
			// Skip markdown headers that might be formatting
			if !strings.HasPrefix(strings.TrimSpace(line), "#") {
				descriptionLines = append(descriptionLines, strings.TrimSpace(line))
			}
		}
	}

	// Save last subtask
	if currentSubtask != nil {
		finalizeSubtask(currentSubtask, descriptionLines)
		if currentSubtask.Title != "" && !seenOrders[currentSubtask.Order] {
			subtasks = append(subtasks, *currentSubtask)
		}
	}

	return subtasks
}

// splitTitleDescription splits "**Title** - Description" or "Title: Description" patterns.
func splitTitleDescription(s string) (title, description string) {
	// Remove markdown bold markers
	s = strings.ReplaceAll(s, "**", "")

	// Try common separators
	separators := []string{" - ", ": ", " – "}
	for _, sep := range separators {
		if idx := strings.Index(s, sep); idx > 0 {
			return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+len(sep):])
		}
	}

	// No separator found, entire string is title
	return strings.TrimSpace(s), ""
}

// finalizeSubtask combines inline description with accumulated description lines.
func finalizeSubtask(subtask *PlannedSubtask, lines []string) {
	if len(lines) == 0 {
		return
	}

	accumulated := strings.TrimSpace(strings.Join(lines, "\n"))
	if subtask.Description == "" {
		subtask.Description = accumulated
	} else {
		// Prepend inline description to accumulated lines
		subtask.Description = subtask.Description + "\n" + accumulated
	}
}

// issueNumberRegex extracts the issue number from a GitHub issue URL.
// Matches patterns like: https://github.com/owner/repo/issues/123
var issueNumberRegex = regexp.MustCompile(`/issues/(\d+)`)


// parseIssueNumber extracts the issue number from a GitHub issue URL.
// Returns 0 if no issue number is found.
func parseIssueNumber(url string) int {
	matches := issueNumberRegex.FindStringSubmatch(url)
	if len(matches) < 2 {
		return 0
	}
	var num int
	_, _ = fmt.Sscanf(matches[1], "%d", &num)
	return num
}

// parsePRNumberFromURL extracts a PR number from a GitHub PR URL.
// Returns 0 if the URL doesn't contain a valid PR number.
func parsePRNumberFromURL(url string) int {
	// Match /pull/123 at the end of the URL
	idx := strings.LastIndex(url, "/pull/")
	if idx < 0 {
		return 0
	}
	numStr := strings.TrimSpace(url[idx+len("/pull/"):])
	// Strip any trailing path segments
	if slashIdx := strings.Index(numStr, "/"); slashIdx >= 0 {
		numStr = numStr[:slashIdx]
	}
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0
	}
	return n
}
// CreateSubIssues creates GitHub issues from the planned subtasks.
// Returns a slice of CreatedIssue with the issue numbers and URLs.
func (r *Runner) CreateSubIssues(ctx context.Context, plan *EpicPlan) ([]CreatedIssue, error) {
	if plan == nil || len(plan.Subtasks) == 0 {
		return nil, fmt.Errorf("plan has no subtasks to create issues from")
	}

	var created []CreatedIssue

	for _, subtask := range plan.Subtasks {
		// Build the issue body
		body := subtask.Description
		if plan.ParentTask != nil && plan.ParentTask.ID != "" {
			body = fmt.Sprintf("Parent: %s\n\n%s", plan.ParentTask.ID, body)
		}

		// Create issue using gh CLI
		args := []string{
			"issue", "create",
			"--title", subtask.Title,
			"--body", body,
			"--label", "pilot",
		}

		cmd := exec.CommandContext(ctx, "gh", args...)

		// Set working directory if parent task has a project path
		if plan.ParentTask != nil && plan.ParentTask.ProjectPath != "" {
			cmd.Dir = plan.ParentTask.ProjectPath
		}

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		r.log.Debug("Creating GitHub issue",
			"subtask_order", subtask.Order,
			"title", subtask.Title,
		)

		if err := cmd.Run(); err != nil {
			return created, fmt.Errorf("failed to create issue for subtask %d: %w (stderr: %s)",
				subtask.Order, err, stderr.String())
		}

		// gh issue create outputs the issue URL on success
		issueURL := strings.TrimSpace(stdout.String())
		issueNumber := parseIssueNumber(issueURL)

		created = append(created, CreatedIssue{
			Number:  issueNumber,
			URL:     issueURL,
			Subtask: subtask,
		})

		r.log.Info("Created GitHub issue",
			"subtask_order", subtask.Order,
			"issue_number", issueNumber,
			"url", issueURL,
		)
	}

	return created, nil
}

// UpdateIssueProgress adds a progress comment to an issue.
func (r *Runner) UpdateIssueProgress(ctx context.Context, projectPath string, issueID string, message string) error {
	args := []string{"issue", "comment", issueID, "--body", message}
	cmd := exec.CommandContext(ctx, "gh", args...)
	if projectPath != "" {
		cmd.Dir = projectPath
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to comment on issue %s: %w (stderr: %s)", issueID, err, stderr.String())
	}
	return nil
}

// CloseIssueWithComment closes an issue with a completion comment.
func (r *Runner) CloseIssueWithComment(ctx context.Context, projectPath string, issueID string, comment string) error {
	args := []string{"issue", "close", issueID, "--comment", comment}
	cmd := exec.CommandContext(ctx, "gh", args...)
	if projectPath != "" {
		cmd.Dir = projectPath
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to close issue %s: %w (stderr: %s)", issueID, err, stderr.String())
	}
	return nil
}

// SubIssueResult tracks the outcome of a single sub-issue execution.
type SubIssueResult struct {
	IssueNumber int    // GitHub issue number
	PRNumber    int    // PR number (0 if no PR created)
	PRUrl       string // PR URL
	Success     bool   // Whether execution + merge succeeded
	Merged      bool   // Whether PR was merged (false if no PR or merge failed/timed out)
	Error       string // Error message if failed
}

// EpicExecutionResult contains the aggregate results of executing all sub-issues.
type EpicExecutionResult struct {
	Total     int               // Total sub-issues
	Succeeded int               // Sub-issues that executed + merged successfully
	Failed    int               // Sub-issues that failed execution or merge
	Results   []SubIssueResult  // Individual results
}

// ExecuteSubIssues executes created sub-issues sequentially with merge-then-next flow.
//
// For each sub-issue:
//  1. Execute the sub-issue as a task
//  2. Create PR via existing flow
//  3. Call onSubIssuePRCreated callback
//  4. Call mergeWaiter callback with configured timeout (default 30m)
//  5. On merge success: switch to default branch, pull latest, proceed to next
//  6. On merge failure/timeout: log error, skip to next sub-issue (don't abort epic)
//
// Sub-issues are only closed after merge confirms. The parent issue is updated with progress
// and closed when all sub-issues complete (regardless of individual failures).
//
// Returns EpicExecutionResult with per-sub-issue outcomes.
func (r *Runner) ExecuteSubIssues(ctx context.Context, parent *Task, issues []CreatedIssue) (*EpicExecutionResult, error) {
	if len(issues) == 0 {
		return nil, fmt.Errorf("no sub-issues to execute")
	}

	total := len(issues)
	projectPath := ""
	if parent != nil {
		projectPath = parent.ProjectPath
	}

	// Determine merge timeout from config
	mergeTimeout := 30 * time.Minute
	if r.config != nil && r.config.Epic != nil && r.config.Epic.SubIssueMergeTimeout > 0 {
		mergeTimeout = r.config.Epic.SubIssueMergeTimeout
	}

	r.log.Info("Starting sequential sub-issue execution with merge-then-next",
		"parent_id", parent.ID,
		"total_issues", total,
		"merge_timeout", mergeTimeout,
	)

	// Update parent with start message
	startMsg := fmt.Sprintf("🚀 Starting sequential execution of %d sub-issues (merge-then-next)", total)
	if err := r.UpdateIssueProgress(ctx, projectPath, parent.ID, startMsg); err != nil {
		r.log.Warn("Failed to update parent progress", "error", err)
	}

	result := &EpicExecutionResult{
		Total:   total,
		Results: make([]SubIssueResult, 0, total),
	}

	// Create git operations for branch switching
	git := NewGitOperations(projectPath)

	for i, issue := range issues {
		subResult := SubIssueResult{
			IssueNumber: issue.Number,
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			subResult.Error = fmt.Sprintf("context cancelled: %v", ctx.Err())
			result.Results = append(result.Results, subResult)
			result.Failed++
			return result, fmt.Errorf("execution cancelled: %w", ctx.Err())
		default:
		}

		// Update parent with current progress
		progressMsg := fmt.Sprintf("⏳ Progress: %d/%d - Starting: **%s** (#%d)",
			i+1, total, issue.Subtask.Title, issue.Number)
		if err := r.UpdateIssueProgress(ctx, projectPath, parent.ID, progressMsg); err != nil {
			r.log.Warn("Failed to update parent progress", "error", err)
		}

		// Create task from sub-issue
		subTask := &Task{
			ID:          fmt.Sprintf("GH-%d", issue.Number),
			Title:       issue.Subtask.Title,
			Description: issue.Subtask.Description,
			ProjectPath: projectPath,
			Branch:      fmt.Sprintf("pilot/GH-%d", issue.Number),
			CreatePR:    true,
		}

		r.log.Info("Executing sub-issue",
			"parent_id", parent.ID,
			"sub_issue", issue.Number,
			"order", i+1,
			"total", total,
		)

		// Execute the sub-task (use override if set, for testing)
		execFn := r.Execute
		if r.executeFunc != nil {
			execFn = r.executeFunc
		}
		execResult, err := execFn(ctx, subTask)
		if err != nil {
			subResult.Error = fmt.Sprintf("execution error: %v", err)
			result.Results = append(result.Results, subResult)
			result.Failed++

			failMsg := fmt.Sprintf("❌ Failed on %d/%d: %s - Error: %v",
				i+1, total, issue.Subtask.Title, err)
			_ = r.UpdateIssueProgress(ctx, projectPath, parent.ID, failMsg)

			r.log.Warn("Sub-issue execution failed, skipping to next",
				"sub_issue", issue.Number,
				"error", err,
			)
			continue // Skip to next sub-issue instead of aborting
		}

		if !execResult.Success {
			subResult.Error = fmt.Sprintf("execution failed: %s", execResult.Error)
			result.Results = append(result.Results, subResult)
			result.Failed++

			failMsg := fmt.Sprintf("❌ Failed on %d/%d: %s - %s",
				i+1, total, issue.Subtask.Title, execResult.Error)
			_ = r.UpdateIssueProgress(ctx, projectPath, parent.ID, failMsg)

			r.log.Warn("Sub-issue execution failed, skipping to next",
				"sub_issue", issue.Number,
				"error", execResult.Error,
			)
			continue // Skip to next sub-issue instead of aborting
		}

		subResult.PRUrl = execResult.PRUrl

		// Register sub-issue PR with autopilot controller (GH-596)
		prNum := 0
		if execResult.PRUrl != "" {
			prNum = parsePRNumberFromURL(execResult.PRUrl)
			subResult.PRNumber = prNum
			if prNum > 0 && r.onSubIssuePRCreated != nil {
				r.onSubIssuePRCreated(prNum, execResult.PRUrl, issue.Number, execResult.CommitSHA, subTask.Branch)
			} else if prNum == 0 {
				r.log.Warn("Failed to extract PR number from sub-issue PR URL",
					"pr_url", execResult.PRUrl)
			}
		}

		// Wait for PR merge if mergeWaiter is configured (GH-743)
		if prNum > 0 && r.mergeWaiter != nil {
			r.log.Info("Waiting for sub-issue PR to merge",
				"sub_issue", issue.Number,
				"pr_number", prNum,
				"timeout", mergeTimeout,
			)

			progressMsg := fmt.Sprintf("⏳ Progress: %d/%d - Waiting for PR #%d to merge...",
				i+1, total, prNum)
			_ = r.UpdateIssueProgress(ctx, projectPath, parent.ID, progressMsg)

			merged, mergeErr := r.mergeWaiter(ctx, prNum, mergeTimeout)
			if mergeErr != nil {
				subResult.Error = fmt.Sprintf("merge wait error: %v", mergeErr)
				result.Results = append(result.Results, subResult)
				result.Failed++

				failMsg := fmt.Sprintf("❌ PR #%d merge failed on %d/%d: %s - %v",
					prNum, i+1, total, issue.Subtask.Title, mergeErr)
				_ = r.UpdateIssueProgress(ctx, projectPath, parent.ID, failMsg)

				r.log.Warn("Sub-issue PR merge failed/timed out, skipping to next",
					"sub_issue", issue.Number,
					"pr_number", prNum,
					"error", mergeErr,
				)
				continue // Skip to next sub-issue instead of aborting
			}

			if !merged {
				subResult.Error = "PR closed without merge"
				result.Results = append(result.Results, subResult)
				result.Failed++

				failMsg := fmt.Sprintf("❌ PR #%d was closed without merge on %d/%d: %s",
					prNum, i+1, total, issue.Subtask.Title)
				_ = r.UpdateIssueProgress(ctx, projectPath, parent.ID, failMsg)

				r.log.Warn("Sub-issue PR closed without merge, skipping to next",
					"sub_issue", issue.Number,
					"pr_number", prNum,
				)
				continue // Skip to next sub-issue instead of aborting
			}

			subResult.Merged = true

			r.log.Info("Sub-issue PR merged successfully",
				"sub_issue", issue.Number,
				"pr_number", prNum,
			)

			// Switch to default branch and pull to get merged changes (GH-743)
			defaultBranch, err := git.SwitchToDefaultBranchAndPull(ctx)
			if err != nil {
				r.log.Warn("Failed to switch to default branch after merge, continuing",
					"sub_issue", issue.Number,
					"error", err,
				)
			} else {
				r.log.Debug("Switched to default branch after merge",
					"branch", defaultBranch,
				)
			}
		} else if prNum > 0 && r.mergeWaiter == nil {
			// No mergeWaiter configured - mark as success without merge confirmation
			r.log.Debug("No mergeWaiter configured, skipping merge wait",
				"sub_issue", issue.Number,
				"pr_number", prNum,
			)
		}

		// Close completed sub-issue only after merge confirms (or if no mergeWaiter)
		subResult.Success = true
		result.Succeeded++

		closeComment := fmt.Sprintf("✅ Completed as part of %s", parent.ID)
		if execResult.PRUrl != "" {
			if subResult.Merged {
				closeComment = fmt.Sprintf("✅ Completed and merged as part of %s\nPR: %s", parent.ID, execResult.PRUrl)
			} else {
				closeComment = fmt.Sprintf("✅ Completed as part of %s\nPR: %s", parent.ID, execResult.PRUrl)
			}
		}
		if err := r.CloseIssueWithComment(ctx, projectPath, fmt.Sprintf("%d", issue.Number), closeComment); err != nil {
			r.log.Warn("Failed to close sub-issue", "issue", issue.Number, "error", err)
		}

		result.Results = append(result.Results, subResult)

		r.log.Info("Sub-issue completed",
			"parent_id", parent.ID,
			"sub_issue", issue.Number,
			"pr_url", execResult.PRUrl,
			"merged", subResult.Merged,
		)
	}

	// All done - update and close parent
	var completeMsg string
	if result.Failed == 0 {
		completeMsg = fmt.Sprintf("✅ Completed: %d/%d sub-issues done\n\nAll sub-tasks executed and merged successfully.", result.Succeeded, total)
	} else {
		completeMsg = fmt.Sprintf("⚠️ Completed with issues: %d/%d succeeded, %d failed\n\nSee individual sub-issues for details.", result.Succeeded, total, result.Failed)
	}
	_ = r.UpdateIssueProgress(ctx, projectPath, parent.ID, completeMsg)

	closeComment := "Epic execution completed."
	if result.Failed > 0 {
		closeComment = fmt.Sprintf("Epic execution completed with %d/%d sub-issues successful.", result.Succeeded, total)
	}
	if err := r.CloseIssueWithComment(ctx, projectPath, parent.ID, closeComment); err != nil {
		r.log.Warn("Failed to close parent issue", "error", err)
	}

	r.log.Info("Epic execution completed",
		"parent_id", parent.ID,
		"total", total,
		"succeeded", result.Succeeded,
		"failed", result.Failed,
	)

	return result, nil
}
