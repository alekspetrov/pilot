package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/alekspetrov/pilot/internal/logging"
)

// MergeWaitResult contains the result of waiting for a PR merge
type MergeWaitResult struct {
	Merged      bool
	Closed      bool
	Conflict    bool
	TimedOut    bool
	PRNumber    int
	MergeCommit string
	Error       error
}

// MergeWaiterConfig configures the PR merge waiter
type MergeWaiterConfig struct {
	// PollInterval is how often to check PR status (default: 30s)
	PollInterval time.Duration
	// Timeout is max time to wait for merge (0 = no timeout)
	Timeout time.Duration
	// OnStatusChange is called when PR status changes
	OnStatusChange func(status *PRStatus)
}

// DefaultMergeWaiterConfig returns default merge waiter settings
func DefaultMergeWaiterConfig() *MergeWaiterConfig {
	return &MergeWaiterConfig{
		PollInterval: 30 * time.Second,
		Timeout:      1 * time.Hour,
	}
}

// MergeWaiter polls a PR until it's merged, closed, or times out
type MergeWaiter struct {
	client *Client
	owner  string
	repo   string
	config *MergeWaiterConfig
	logger *slog.Logger
}

// NewMergeWaiter creates a new PR merge waiter
func NewMergeWaiter(client *Client, owner, repo string, config *MergeWaiterConfig) *MergeWaiter {
	if config == nil {
		config = DefaultMergeWaiterConfig()
	}
	return &MergeWaiter{
		client: client,
		owner:  owner,
		repo:   repo,
		config: config,
		logger: logging.WithComponent("merge-waiter"),
	}
}

// ErrPRClosed is returned when PR is closed without merge
var ErrPRClosed = errors.New("PR was closed without merge")

// ErrPRConflict is returned when PR has merge conflicts
var ErrPRConflict = errors.New("PR has merge conflicts")

// ErrPRTimeout is returned when PR merge times out
var ErrPRTimeout = errors.New("PR merge timed out")

// WaitForMerge waits for a PR to be merged
// Returns nil on successful merge, or an error if closed/conflict/timeout
func (w *MergeWaiter) WaitForMerge(ctx context.Context, prNumber int) (*MergeWaitResult, error) {
	w.logger.Info("Waiting for PR merge",
		slog.Int("pr", prNumber),
		slog.String("repo", w.owner+"/"+w.repo),
		slog.Duration("poll_interval", w.config.PollInterval),
		slog.Duration("timeout", w.config.Timeout),
	)

	result := &MergeWaitResult{PRNumber: prNumber}

	// Set up timeout if configured
	var cancel context.CancelFunc
	if w.config.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, w.config.Timeout)
		defer cancel()
	}

	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	// Check immediately before first tick
	status, err := w.checkStatus(ctx, prNumber)
	if err != nil {
		result.Error = err
		return result, err
	}
	if done, err := w.handleStatus(status, result); done {
		return result, err
	}

	for {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				w.logger.Warn("PR merge wait timed out",
					slog.Int("pr", prNumber),
					slog.Duration("timeout", w.config.Timeout),
				)
				result.TimedOut = true
				result.Error = ErrPRTimeout
				return result, ErrPRTimeout
			}
			result.Error = ctx.Err()
			return result, ctx.Err()

		case <-ticker.C:
			status, err := w.checkStatus(ctx, prNumber)
			if err != nil {
				// Log but continue polling - transient errors are OK
				w.logger.Warn("Failed to check PR status",
					slog.Int("pr", prNumber),
					slog.Any("error", err),
				)
				continue
			}

			if done, err := w.handleStatus(status, result); done {
				return result, err
			}
		}
	}
}

// checkStatus fetches current PR status
func (w *MergeWaiter) checkStatus(ctx context.Context, prNumber int) (*PRStatus, error) {
	return w.client.GetPRStatus(ctx, w.owner, w.repo, prNumber)
}

// handleStatus processes PR status and returns (done, error)
func (w *MergeWaiter) handleStatus(status *PRStatus, result *MergeWaitResult) (bool, error) {
	// Notify callback if configured
	if w.config.OnStatusChange != nil {
		w.config.OnStatusChange(status)
	}

	switch status.State {
	case PRStateMerged:
		w.logger.Info("PR merged",
			slog.Int("pr", status.Number),
		)
		result.Merged = true
		result.MergeCommit = status.MergeCommit
		return true, nil

	case PRStateClosed:
		w.logger.Warn("PR closed without merge",
			slog.Int("pr", status.Number),
		)
		result.Closed = true
		result.Error = ErrPRClosed
		return true, ErrPRClosed

	case PRStateConflict:
		w.logger.Warn("PR has merge conflicts",
			slog.Int("pr", status.Number),
		)
		result.Conflict = true
		result.Error = ErrPRConflict
		return true, ErrPRConflict

	case PRStateOpen:
		w.logger.Debug("PR still open, waiting...",
			slog.Int("pr", status.Number),
		)
		return false, nil

	default:
		w.logger.Debug("PR in unknown state, waiting...",
			slog.Int("pr", status.Number),
			slog.String("state", string(status.State)),
		)
		return false, nil
	}
}

// WaitForMergeByBranch finds PR by branch and waits for merge
func (w *MergeWaiter) WaitForMergeByBranch(ctx context.Context, branch string) (*MergeWaitResult, error) {
	// Find PR by branch
	pr, err := w.client.FindPRByBranch(ctx, w.owner, w.repo, branch)
	if err != nil {
		return nil, fmt.Errorf("failed to find PR for branch %s: %w", branch, err)
	}
	if pr == nil {
		return nil, fmt.Errorf("no PR found for branch %s", branch)
	}

	return w.WaitForMerge(ctx, pr.Number)
}

// PRWatcherStatus represents the final status of a PR watch
type PRWatcherStatus string

const (
	PRStatusMerged   PRWatcherStatus = "merged"
	PRStatusClosed   PRWatcherStatus = "closed"
	PRStatusConflict PRWatcherStatus = "conflict"
	PRStatusTimeout  PRWatcherStatus = "timeout"
	PRStatusError    PRWatcherStatus = "error"
)

// PRWatchResult is the result of watching a PR for merge
type PRWatchResult struct {
	Status       PRWatcherStatus
	WaitDuration time.Duration
	Error        error
}

// PRWatcher watches a PR for merge/close/conflict
type PRWatcher struct {
	client       *Client
	owner        string
	repo         string
	pollInterval time.Duration
	timeout      time.Duration
	logger       *slog.Logger
}

// NewPRWatcher creates a new PR watcher
func NewPRWatcher(client *Client, owner, repo string, pollInterval, timeout time.Duration) *PRWatcher {
	if pollInterval == 0 {
		pollInterval = 30 * time.Second
	}
	if timeout == 0 {
		timeout = 1 * time.Hour
	}
	return &PRWatcher{
		client:       client,
		owner:        owner,
		repo:         repo,
		pollInterval: pollInterval,
		timeout:      timeout,
		logger:       logging.WithComponent("pr-watcher"),
	}
}

// WaitForMerge waits for a PR to be merged, closed, or timeout
func (w *PRWatcher) WaitForMerge(ctx context.Context, prNumber int) *PRWatchResult {
	startTime := time.Now()

	waiter := NewMergeWaiter(w.client, w.owner, w.repo, &MergeWaiterConfig{
		PollInterval: w.pollInterval,
		Timeout:      w.timeout,
	})

	result, err := waiter.WaitForMerge(ctx, prNumber)
	duration := time.Since(startTime)

	if err != nil {
		switch {
		case result != nil && result.Merged:
			return &PRWatchResult{Status: PRStatusMerged, WaitDuration: duration}
		case result != nil && result.Closed:
			return &PRWatchResult{Status: PRStatusClosed, WaitDuration: duration}
		case result != nil && result.Conflict:
			return &PRWatchResult{Status: PRStatusConflict, WaitDuration: duration}
		case result != nil && result.TimedOut:
			return &PRWatchResult{Status: PRStatusTimeout, WaitDuration: duration, Error: err}
		default:
			return &PRWatchResult{Status: PRStatusError, WaitDuration: duration, Error: err}
		}
	}

	if result.Merged {
		return &PRWatchResult{Status: PRStatusMerged, WaitDuration: duration}
	}

	return &PRWatchResult{Status: PRStatusError, WaitDuration: duration}
}
