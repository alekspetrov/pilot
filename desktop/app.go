package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/alekspetrov/pilot/internal/config"
	"github.com/alekspetrov/pilot/internal/memory"
)

// App holds state for the Wails application.
type App struct {
	ctx    context.Context
	store  *memory.Store
	cfg    *config.Config
	client *http.Client
}

// NewApp creates a new App instance.
func NewApp() *App {
	return &App{
		client: &http.Client{Timeout: 2 * time.Second},
	}
}

// startup is called when the app starts. Opens the SQLite store and loads config.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		cfg = config.DefaultConfig()
	}
	a.cfg = cfg

	dataPath := cfg.Memory.Path
	store, err := memory.NewStore(dataPath)
	if err != nil {
		// Non-fatal: desktop app degrades gracefully without SQLite
		return
	}
	a.store = store
}

// shutdown is called when the app exits.
func (a *App) shutdown(_ context.Context) {
	if a.store != nil {
		_ = a.store.Close()
	}
}

// GetMetrics returns aggregated lifetime metrics and 7-day sparkline history.
func (a *App) GetMetrics() (*MetricsData, error) {
	if a.store == nil {
		return &MetricsData{}, nil
	}

	lt, err := a.store.GetLifetimeTokens()
	if err != nil {
		return nil, fmt.Errorf("get lifetime tokens: %w", err)
	}

	tc, err := a.store.GetLifetimeTaskCounts()
	if err != nil {
		return nil, fmt.Errorf("get lifetime task counts: %w", err)
	}

	// 7-day daily history for sparklines
	now := time.Now()
	query := memory.MetricsQuery{
		Start: now.AddDate(0, 0, -7).Truncate(24 * time.Hour),
		End:   now.Add(24 * time.Hour).Truncate(24 * time.Hour),
	}
	daily, err := a.store.GetDailyMetrics(query)
	if err != nil {
		daily = nil // degrade gracefully
	}

	// Build 7-slot arrays (newest last)
	tokenHistory := make([]int64, 7)
	costHistory := make([]float64, 7)
	taskHistory := make([]int, 7)

	for _, d := range daily {
		daysAgo := int(now.Truncate(24 * time.Hour).Sub(d.Date.Truncate(24 * time.Hour)).Hours() / 24)
		if daysAgo >= 0 && daysAgo < 7 {
			idx := 6 - daysAgo
			tokenHistory[idx] = d.TotalTokens
			costHistory[idx] = d.TotalCostUSD
			taskHistory[idx] = d.ExecutionCount
		}
	}

	var costPerTask float64
	if tc.Total > 0 {
		costPerTask = lt.TotalCostUSD / float64(tc.Total)
	}

	return &MetricsData{
		TotalTokens:  lt.TotalTokens,
		InputTokens:  lt.InputTokens,
		OutputTokens: lt.OutputTokens,
		TotalCostUSD: lt.TotalCostUSD,
		CostPerTask:  costPerTask,
		TotalTasks:   tc.Total,
		Succeeded:    tc.Succeeded,
		Failed:       tc.Failed,
		TokenHistory: tokenHistory,
		CostHistory:  costHistory,
		TaskHistory:  taskHistory,
	}, nil
}

// GetHistory returns the most recent executions for the history panel.
func (a *App) GetHistory(limit int) ([]*HistoryEntry, error) {
	if a.store == nil {
		return nil, nil
	}

	executions, err := a.store.GetRecentExecutions(limit)
	if err != nil {
		return nil, fmt.Errorf("get recent executions: %w", err)
	}

	entries := make([]*HistoryEntry, 0, len(executions))
	for _, exec := range executions {
		duration := ""
		if exec.DurationMs > 0 {
			d := time.Duration(exec.DurationMs) * time.Millisecond
			if d >= time.Hour {
				duration = fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
			} else if d >= time.Minute {
				duration = fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
			} else {
				duration = fmt.Sprintf("%ds", int(d.Seconds()))
			}
		}

		completedAt := ""
		if exec.CompletedAt != nil {
			completedAt = exec.CompletedAt.UTC().Format(time.RFC3339)
		}

		title := exec.TaskTitle
		if title == "" {
			title = exec.TaskID
		}

		entries = append(entries, &HistoryEntry{
			ID:          exec.TaskID,
			Title:       title,
			Status:      exec.Status,
			Duration:    duration,
			CompletedAt: completedAt,
		})
	}

	return entries, nil
}

// GetVersion returns the app version string (injected via LDFLAGS).
func (a *App) GetVersion() string {
	return version
}

// GetConfig returns a non-sensitive summary of the current config.
func (a *App) GetConfig() (*ConfigSummary, error) {
	if a.cfg == nil {
		return &ConfigSummary{GatewayPort: 9090}, nil
	}

	port := 9090
	if a.cfg.Gateway != nil {
		port = a.cfg.Gateway.Port
	}

	autopilotEnv := ""
	if a.cfg.Orchestrator != nil && a.cfg.Orchestrator.Autopilot != nil {
		autopilotEnv = string(a.cfg.Orchestrator.Autopilot.Environment)
	}

	adapters := []string{}
	if a.cfg.Adapters != nil {
		if a.cfg.Adapters.GitHub != nil && a.cfg.Adapters.GitHub.Token != "" {
			adapters = append(adapters, "github")
		}
		if a.cfg.Adapters.Slack != nil && a.cfg.Adapters.Slack.BotToken != "" {
			adapters = append(adapters, "slack")
		}
		if a.cfg.Adapters.Telegram != nil && a.cfg.Adapters.Telegram.BotToken != "" {
			adapters = append(adapters, "telegram")
		}
		if a.cfg.Adapters.Linear != nil && a.cfg.Adapters.Linear.APIKey != "" {
			adapters = append(adapters, "linear")
		}
	}

	return &ConfigSummary{
		GatewayPort: port,
		Autopilot:   autopilotEnv,
		Adapters:    adapters,
	}, nil
}

// OpenInBrowser opens the given URL in the system default browser.
func (a *App) OpenInBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start() //nolint:gosec
	default:
		return exec.Command("xdg-open", url).Start() //nolint:gosec
	}
}

// GetQueueTasks fetches live queue data from the running gateway.
// Returns an empty list gracefully if the gateway is not running.
func (a *App) GetQueueTasks() ([]*TaskDisplay, error) {
	port := 9090
	if a.cfg != nil && a.cfg.Gateway != nil {
		port = a.cfg.Gateway.Port
	}

	url := fmt.Sprintf("http://localhost:%d/api/v1/tasks", port)
	resp, err := a.client.Get(url) //nolint:noctx
	if err != nil {
		return []*TaskDisplay{}, nil // gateway not running
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return []*TaskDisplay{}, nil
	}

	var tasks []*TaskDisplay
	if err := json.Unmarshal(body, &tasks); err != nil {
		return []*TaskDisplay{}, nil
	}

	return tasks, nil
}

// GetServerStatus checks if the pilot gateway is running.
func (a *App) GetServerStatus() (*ServerStatus, error) {
	port := 9090
	if a.cfg != nil && a.cfg.Gateway != nil {
		port = a.cfg.Gateway.Port
	}

	url := fmt.Sprintf("http://localhost:%d/api/v1/status", port)
	resp, err := a.client.Get(url) //nolint:noctx
	if err != nil {
		return &ServerStatus{Running: false}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	var status ServerStatus
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &ServerStatus{Running: false}, nil
	}
	if err := json.Unmarshal(body, &status); err != nil {
		return &ServerStatus{Running: resp.StatusCode == http.StatusOK}, nil
	}

	status.Running = true
	return &status, nil
}
