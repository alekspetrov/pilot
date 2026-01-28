package github

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alekspetrov/pilot/internal/logging"
)

// ExecutionMode determines how tasks are executed
type ExecutionMode string

const (
	// ExecutionModeSequential processes one issue at a time, waiting for PR merge
	ExecutionModeSequential ExecutionMode = "sequential"
	// ExecutionModeParallel processes issues as they arrive (legacy behavior)
	ExecutionModeParallel ExecutionMode = "parallel"
)

// SequentialConfig configures sequential execution mode
type SequentialConfig struct {
	// WaitForMerge waits for PR to be merged before next task
	WaitForMerge bool
	// PollInterval is how often to check PR status
	PollInterval time.Duration
	// PRTimeout is max time to wait for PR merge (0 = no timeout)
	PRTimeout time.Duration
}

// Poller polls GitHub for issues with a specific label
type Poller struct {
	client    *Client
	owner     string
	repo      string
	label     string
	interval  time.Duration
	processed map[int]bool
	mu        sync.RWMutex
	onIssue   func(ctx context.Context, issue *Issue) error
	logger    *slog.Logger

	// Sequential mode settings
	mode             ExecutionMode
	sequentialConfig *SequentialConfig
	onPRStatus       func(prNumber int, status *PRStatus)
}

// PollerOption configures a Poller
type PollerOption func(*Poller)

// WithPollerLogger sets the logger for the poller
func WithPollerLogger(logger *slog.Logger) PollerOption {
	return func(p *Poller) {
		p.logger = logger
	}
}

// WithOnIssue sets the callback for new issues
func WithOnIssue(fn func(ctx context.Context, issue *Issue) error) PollerOption {
	return func(p *Poller) {
		p.onIssue = fn
	}
}

// WithExecutionMode sets the execution mode (sequential or parallel)
func WithExecutionMode(mode ExecutionMode) PollerOption {
	return func(p *Poller) {
		p.mode = mode
	}
}

// WithSequentialConfig sets the sequential mode configuration
func WithSequentialConfig(config *SequentialConfig) PollerOption {
	return func(p *Poller) {
		p.sequentialConfig = config
		if config != nil {
			p.mode = ExecutionModeSequential
		}
	}
}

// WithOnPRStatus sets the callback for PR status updates during sequential mode
func WithOnPRStatus(fn func(prNumber int, status *PRStatus)) PollerOption {
	return func(p *Poller) {
		p.onPRStatus = fn
	}
}

// NewPoller creates a new GitHub issue poller
func NewPoller(client *Client, repo string, label string, interval time.Duration, opts ...PollerOption) (*Poller, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo format, expected owner/repo: %s", repo)
	}

	p := &Poller{
		client:    client,
		owner:     parts[0],
		repo:      parts[1],
		label:     label,
		interval:  interval,
		processed: make(map[int]bool),
		logger:    logging.WithComponent("github-poller"),
		mode:      ExecutionModeSequential, // Default to sequential
	}

	for _, opt := range opts {
		opt(p)
	}

	return p, nil
}

// Start begins polling for issues
func (p *Poller) Start(ctx context.Context) {
	p.logger.Info("Starting GitHub poller",
		slog.String("repo", p.owner+"/"+p.repo),
		slog.String("label", p.label),
		slog.Duration("interval", p.interval),
		slog.String("mode", string(p.mode)),
	)

	if p.mode == ExecutionModeSequential {
		p.startSequential(ctx)
	} else {
		p.startParallel(ctx)
	}
}

// startSequential runs in sequential mode - one issue at a time, waiting for PR merge
func (p *Poller) startSequential(ctx context.Context) {
	p.logger.Info("Running in sequential mode - will wait for PR merge before next issue")

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("GitHub poller stopped")
			return
		default:
			// Find oldest unprocessed issue
			issue := p.findOldestUnprocessedIssue(ctx)
			if issue == nil {
				// No issues to process, wait and try again
				select {
				case <-ctx.Done():
					return
				case <-time.After(p.interval):
					continue
				}
			}

			// Process the issue (task execution + PR creation)
			p.logger.Info("Processing issue in sequential mode",
				slog.Int("number", issue.Number),
				slog.String("title", issue.Title),
			)

			prNumber, err := p.processIssueSequential(ctx, issue)
			if err != nil {
				p.logger.Error("Failed to process issue",
					slog.Int("number", issue.Number),
					slog.Any("error", err),
				)
				// Mark as processed to avoid retry loop
				p.markProcessed(issue.Number)
				continue
			}

			// If PR was created and we should wait for merge
			if prNumber > 0 && p.sequentialConfig != nil && p.sequentialConfig.WaitForMerge {
				p.logger.Info("Waiting for PR merge before next issue",
					slog.Int("pr", prNumber),
					slog.Int("issue", issue.Number),
				)

				waiter := NewMergeWaiter(p.client, p.owner, p.repo, &MergeWaiterConfig{
					PollInterval: p.sequentialConfig.PollInterval,
					Timeout:      p.sequentialConfig.PRTimeout,
					OnStatusChange: func(status *PRStatus) {
						if p.onPRStatus != nil {
							p.onPRStatus(prNumber, status)
						}
					},
				})

				result, err := waiter.WaitForMerge(ctx, prNumber)
				if err != nil {
					p.logger.Warn("PR merge wait ended with error",
						slog.Int("pr", prNumber),
						slog.Any("error", err),
					)
					// Handle different error cases
					switch {
					case result != nil && result.Closed:
						p.logger.Warn("PR was closed without merge - marking issue for re-review",
							slog.Int("pr", prNumber),
							slog.Int("issue", issue.Number),
						)
					case result != nil && result.Conflict:
						p.logger.Warn("PR has merge conflicts - needs manual resolution",
							slog.Int("pr", prNumber),
							slog.Int("issue", issue.Number),
						)
					case result != nil && result.TimedOut:
						p.logger.Warn("PR merge wait timed out - continuing with next issue",
							slog.Int("pr", prNumber),
							slog.Int("issue", issue.Number),
						)
					}
				} else {
					p.logger.Info("PR merged successfully, proceeding to next issue",
						slog.Int("pr", prNumber),
						slog.Int("issue", issue.Number),
					)
				}
			}

			p.markProcessed(issue.Number)
		}
	}
}

// startParallel runs in parallel mode (legacy behavior)
func (p *Poller) startParallel(ctx context.Context) {
	// Do an initial check immediately
	p.checkForNewIssues(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("GitHub poller stopped")
			return
		case <-ticker.C:
			p.checkForNewIssues(ctx)
		}
	}
}

// findOldestUnprocessedIssue finds the oldest issue with the pilot label that hasn't been processed
func (p *Poller) findOldestUnprocessedIssue(ctx context.Context) *Issue {
	issues, err := p.client.ListIssues(ctx, p.owner, p.repo, &ListIssuesOptions{
		Labels: []string{p.label},
		State:  StateOpen,
		Sort:   "created", // Sort by creation date
	})
	if err != nil {
		p.logger.Warn("Failed to fetch issues", slog.Any("error", err))
		return nil
	}

	// Sort by creation date (oldest first)
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].CreatedAt.Before(issues[j].CreatedAt)
	})

	for _, issue := range issues {
		// Skip if already processed
		p.mu.RLock()
		processed := p.processed[issue.Number]
		p.mu.RUnlock()

		if processed {
			continue
		}

		// Skip if already in progress or done
		if HasLabel(issue, LabelInProgress) || HasLabel(issue, LabelDone) {
			p.markProcessed(issue.Number)
			continue
		}

		return issue
	}

	return nil
}

// processIssueSequential processes an issue and returns the PR number if one was created
func (p *Poller) processIssueSequential(ctx context.Context, issue *Issue) (int, error) {
	if p.onIssue == nil {
		return 0, nil
	}

	// Call the issue handler
	if err := p.onIssue(ctx, issue); err != nil {
		return 0, err
	}

	// Try to find the PR that was created for this issue
	// Convention: branch name is pilot/GH-<issue_number>
	branchName := fmt.Sprintf("pilot/GH-%d", issue.Number)
	pr, err := p.client.FindPRByBranch(ctx, p.owner, p.repo, branchName)
	if err != nil {
		p.logger.Debug("Could not find PR for branch",
			slog.String("branch", branchName),
			slog.Any("error", err),
		)
		return 0, nil // Not an error - PR might not have been created
	}

	if pr != nil {
		return pr.Number, nil
	}

	return 0, nil
}

// checkForNewIssues fetches issues and processes new ones
func (p *Poller) checkForNewIssues(ctx context.Context) {
	issues, err := p.client.ListIssues(ctx, p.owner, p.repo, &ListIssuesOptions{
		Labels: []string{p.label},
		State:  StateOpen,
		Sort:   "updated",
	})
	if err != nil {
		p.logger.Warn("Failed to fetch issues", slog.Any("error", err))
		return
	}

	for _, issue := range issues {
		// Skip if already processed
		p.mu.RLock()
		processed := p.processed[issue.Number]
		p.mu.RUnlock()

		if processed {
			continue
		}

		// Skip if already in progress or done
		if HasLabel(issue, LabelInProgress) || HasLabel(issue, LabelDone) {
			p.markProcessed(issue.Number)
			continue
		}

		// Process the issue
		p.logger.Info("Found new issue",
			slog.Int("number", issue.Number),
			slog.String("title", issue.Title),
		)

		if p.onIssue != nil {
			if err := p.onIssue(ctx, issue); err != nil {
				p.logger.Error("Failed to process issue",
					slog.Int("number", issue.Number),
					slog.Any("error", err),
				)
				// Don't mark as processed so we can retry
				continue
			}
		}

		p.markProcessed(issue.Number)
	}
}

// markProcessed marks an issue as processed
func (p *Poller) markProcessed(number int) {
	p.mu.Lock()
	p.processed[number] = true
	p.mu.Unlock()
}

// IsProcessed checks if an issue has been processed
func (p *Poller) IsProcessed(number int) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.processed[number]
}

// ProcessedCount returns the number of processed issues
func (p *Poller) ProcessedCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.processed)
}

// Reset clears the processed issues map
func (p *Poller) Reset() {
	p.mu.Lock()
	p.processed = make(map[int]bool)
	p.mu.Unlock()
}
