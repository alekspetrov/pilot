package asana

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/alekspetrov/pilot/internal/logging"
)

// TaskResult is returned by the task handler
type TaskResult struct {
	Success  bool
	PRNumber int
	PRURL    string
	Error    error
}

// Poller polls Asana for tasks with a specific tag
type Poller struct {
	client    *Client
	config    *Config
	interval  time.Duration
	processed map[string]bool // Asana uses string GIDs
	mu        sync.RWMutex
	onTask    func(ctx context.Context, task *Task) (*TaskResult, error)
	logger    *slog.Logger

	// Tag GID cache
	pilotTagGID      string
	inProgressTagGID string
	doneTagGID       string
	failedTagGID     string
}

// PollerOption configures a Poller
type PollerOption func(*Poller)

// WithOnAsanaTask sets the callback for new tasks
func WithOnAsanaTask(fn func(ctx context.Context, task *Task) (*TaskResult, error)) PollerOption {
	return func(p *Poller) {
		p.onTask = fn
	}
}

// WithAsanaPollerLogger sets the logger for the poller
func WithAsanaPollerLogger(logger *slog.Logger) PollerOption {
	return func(p *Poller) {
		p.logger = logger
	}
}

// NewPoller creates a new Asana task poller
func NewPoller(client *Client, config *Config, interval time.Duration, opts ...PollerOption) *Poller {
	p := &Poller{
		client:    client,
		config:    config,
		interval:  interval,
		processed: make(map[string]bool),
		logger:    logging.WithComponent("asana-poller"),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Start begins polling for tasks
func (p *Poller) Start(ctx context.Context) error {
	// Cache tag GIDs on startup
	if err := p.cacheTagGIDs(ctx); err != nil {
		return fmt.Errorf("failed to cache tag GIDs: %w", err)
	}

	p.logger.Info("Starting Asana poller",
		slog.String("workspace", p.client.workspaceID),
		slog.String("tag", p.config.PilotTag),
		slog.Duration("interval", p.interval),
	)

	// Initial check
	p.checkForNewTasks(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Asana poller stopped")
			return nil
		case <-ticker.C:
			p.checkForNewTasks(ctx)
		}
	}
}

func (p *Poller) cacheTagGIDs(ctx context.Context) error {
	// Find or create pilot tag
	pilotTag, err := p.client.FindTagByName(ctx, p.config.PilotTag)
	if err != nil {
		return fmt.Errorf("pilot tag lookup: %w", err)
	}
	if pilotTag == nil {
		return fmt.Errorf("pilot tag %q not found in workspace", p.config.PilotTag)
	}
	p.pilotTagGID = pilotTag.GID

	// Status tags are optional - create if needed or skip
	if tag, _ := p.client.FindTagByName(ctx, "pilot-in-progress"); tag != nil {
		p.inProgressTagGID = tag.GID
	}
	if tag, _ := p.client.FindTagByName(ctx, "pilot-done"); tag != nil {
		p.doneTagGID = tag.GID
	}
	if tag, _ := p.client.FindTagByName(ctx, "pilot-failed"); tag != nil {
		p.failedTagGID = tag.GID
	}

	return nil
}

func (p *Poller) checkForNewTasks(ctx context.Context) {
	tasks, err := p.client.GetActiveTasksByTag(ctx, p.pilotTagGID)
	if err != nil {
		p.logger.Warn("Failed to fetch tasks", slog.Any("error", err))
		return
	}

	// Sort by creation date (oldest first)
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})

	for _, task := range tasks {
		// Skip if already processed
		p.mu.RLock()
		processed := p.processed[task.GID]
		p.mu.RUnlock()

		if processed {
			continue
		}

		// Skip if has in-progress, done, or failed tag
		if p.hasStatusTag(&task) {
			p.markProcessed(task.GID)
			continue
		}

		// Process the task
		p.logger.Info("Found new Asana task",
			slog.String("gid", task.GID),
			slog.String("name", task.Name),
		)

		if p.onTask != nil {
			// Add in-progress tag
			if p.inProgressTagGID != "" {
				_ = p.client.AddTag(ctx, task.GID, p.inProgressTagGID)
			}

			result, err := p.onTask(ctx, &task)
			if err != nil {
				p.logger.Error("Failed to process task",
					slog.String("gid", task.GID),
					slog.Any("error", err),
				)
				// Remove in-progress tag, add failed tag
				if p.inProgressTagGID != "" {
					_ = p.client.RemoveTag(ctx, task.GID, p.inProgressTagGID)
				}
				if p.failedTagGID != "" {
					_ = p.client.AddTag(ctx, task.GID, p.failedTagGID)
				}
				p.markProcessed(task.GID)
				continue
			}

			// Remove in-progress tag
			if p.inProgressTagGID != "" {
				_ = p.client.RemoveTag(ctx, task.GID, p.inProgressTagGID)
			}

			// Add done tag on success
			if result != nil && result.Success && p.doneTagGID != "" {
				_ = p.client.AddTag(ctx, task.GID, p.doneTagGID)
			}
		}

		p.markProcessed(task.GID)
	}
}

func (p *Poller) hasStatusTag(task *Task) bool {
	for _, tag := range task.Tags {
		switch tag.Name {
		case "pilot-in-progress", "pilot-done", "pilot-failed":
			return true
		}
	}
	return false
}

func (p *Poller) markProcessed(gid string) {
	p.mu.Lock()
	p.processed[gid] = true
	p.mu.Unlock()
}

// IsProcessed checks if a task has been processed
func (p *Poller) IsProcessed(gid string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.processed[gid]
}

// ProcessedCount returns the number of processed tasks
func (p *Poller) ProcessedCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.processed)
}

// Reset clears the processed tasks map
func (p *Poller) Reset() {
	p.mu.Lock()
	p.processed = make(map[string]bool)
	p.mu.Unlock()
}
