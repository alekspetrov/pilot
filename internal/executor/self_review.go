package executor

import (
	"context"
	"strings"
	"time"

	"github.com/alekspetrov/pilot/internal/quality"
)

// runSelfReview executes a self-review phase where Claude examines its changes.
// This catches issues like unwired config, undefined methods, or incomplete implementations.
// Returns nil if review passes or is skipped, error only for critical failures.
func (r *Runner) runSelfReview(ctx context.Context, task *Task, state *progressState) error {
	// Skip self-review if disabled in config
	if r.config != nil && r.config.SkipSelfReview {
		r.log.Debug("Self-review skipped (disabled in config)", "task_id", task.ID)
		return nil
	}

	// Skip for trivial tasks - they don't need self-review
	complexity := DetectComplexity(task)
	if complexity.ShouldSkipNavigator() {
		r.log.Debug("Self-review skipped (trivial task)", "task_id", task.ID)
		return nil
	}

	r.log.Info("Running self-review phase", "task_id", task.ID)
	r.reportProgress(task.ID, "Self-Review", 95, "Reviewing changes...")

	reviewPrompt := r.buildSelfReviewPrompt(task)

	// Execute self-review with shorter timeout (2 minutes)
	reviewCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// Select model and effort (use same routing as main execution)
	selectedModel := r.modelRouter.SelectModel(task)
	selectedEffort := r.modelRouter.SelectEffort(task)

	result, err := r.backend.Execute(reviewCtx, ExecuteOptions{
		Prompt:      reviewPrompt,
		ProjectPath: task.ProjectPath,
		Verbose:     task.Verbose,
		Model:       selectedModel,
		Effort:      selectedEffort,
		EventHandler: func(event BackendEvent) {
			// Track tokens from self-review
			state.tokensInput += event.TokensInput
			state.tokensOutput += event.TokensOutput
			// Extract any new commit SHAs from self-review fixes
			if event.Type == EventTypeToolResult && event.ToolResult != "" {
				extractCommitSHA(event.ToolResult, state)
			}
		},
	})

	if err != nil {
		// Self-review failure is not fatal - log and continue
		r.log.Warn("Self-review execution failed",
			"task_id", task.ID,
			"error", err,
		)
		return nil
	}

	// Check if review found and fixed issues
	if strings.Contains(result.Output, "REVIEW_FIXED:") {
		r.log.Info("Self-review fixed issues",
			"task_id", task.ID,
		)
		r.reportProgress(task.ID, "Self-Review", 97, "Issues fixed during review")
	} else if strings.Contains(result.Output, "REVIEW_PASSED") {
		r.log.Info("Self-review passed",
			"task_id", task.ID,
		)
		r.reportProgress(task.ID, "Self-Review", 97, "Review passed")
	} else {
		r.log.Debug("Self-review completed (no explicit signal)",
			"task_id", task.ID,
		)
	}

	return nil
}

// buildQualityGatesResult converts QualityOutcome to QualityGatesResult for ExecutionResult (GH-209)
func (r *Runner) buildQualityGatesResult(outcome *QualityOutcome, totalRetries int) *QualityGatesResult {
	if outcome == nil {
		return nil
	}

	qgResult := &QualityGatesResult{
		Enabled:       true,
		AllPassed:     outcome.Passed,
		TotalDuration: outcome.TotalDuration,
		TotalRetries:  totalRetries,
		Gates:         make([]QualityGateResult, len(outcome.GateDetails)),
	}

	for i, detail := range outcome.GateDetails {
		qgResult.Gates[i] = QualityGateResult(detail)
	}

	return qgResult
}

// simpleQualityChecker is a minimal quality checker for auto-enabled build gates (GH-363).
// Used when quality gates aren't explicitly configured but we still want basic build verification.
type simpleQualityChecker struct {
	config      *quality.Config
	projectPath string
	taskID      string
}

// Check runs the build gate and returns the outcome.
func (c *simpleQualityChecker) Check(ctx context.Context) (*QualityOutcome, error) {
	runner := quality.NewRunner(c.config, c.projectPath)

	results, err := runner.RunAll(ctx, c.taskID)
	if err != nil {
		return nil, err
	}

	// Convert to QualityOutcome
	outcome := &QualityOutcome{
		Passed:        results.AllPassed,
		ShouldRetry:   !results.AllPassed && c.config.OnFailure.Action == quality.ActionRetry,
		TotalDuration: results.TotalTime,
		GateDetails:   make([]QualityGateDetail, 0, len(results.Results)),
	}

	// Build retry feedback if failed
	if !results.AllPassed {
		outcome.RetryFeedback = quality.FormatErrorFeedback(results)
	}

	for _, r := range results.Results {
		outcome.GateDetails = append(outcome.GateDetails, QualityGateDetail{
			Name:       r.GateName,
			Passed:     r.Status == quality.StatusPassed,
			Duration:   r.Duration,
			RetryCount: r.RetryCount,
			Error:      r.Error,
		})
	}

	return outcome, nil
}
