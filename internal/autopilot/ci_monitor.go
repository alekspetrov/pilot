package autopilot

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"sync"
	"time"

	"github.com/alekspetrov/pilot/internal/adapters/github"
)

// CIMonitor watches GitHub CI status for PRs.
type CIMonitor struct {
	ghClient     *github.Client
	owner        string
	repo         string
	pollInterval time.Duration
	waitTimeout  time.Duration
	ciChecks     *CIChecksConfig // replaces requiredChecks
	log          *slog.Logger

	// Discovery state (for auto mode)
	discoveredChecks map[string][]string   // sha -> check names
	discoveryStart   map[string]time.Time  // sha -> start time
	mu               sync.RWMutex
}

// NewCIMonitor creates a CI monitor with configuration from Config.
// Uses DevCITimeout for dev environment, CIWaitTimeout for stage/prod.
func NewCIMonitor(ghClient *github.Client, owner, repo string, cfg *Config) *CIMonitor {
	timeout := cfg.CIWaitTimeout
	if cfg.Environment == EnvDev && cfg.DevCITimeout > 0 {
		timeout = cfg.DevCITimeout
	}

	ciChecks := cfg.CIChecks
	if ciChecks == nil {
		ciChecks = &CIChecksConfig{Mode: "auto", DiscoveryGracePeriod: 60 * time.Second}
	}

	// Backward compatibility: legacy required_checks -> manual mode
	if len(cfg.RequiredChecks) > 0 {
		ciChecks.Mode = "manual"
		ciChecks.Required = cfg.RequiredChecks
	}

	return &CIMonitor{
		ghClient:         ghClient,
		owner:            owner,
		repo:             repo,
		pollInterval:     cfg.CIPollInterval,
		waitTimeout:      timeout,
		ciChecks:         ciChecks,
		discoveredChecks: make(map[string][]string),
		discoveryStart:   make(map[string]time.Time),
		log:              slog.Default().With("component", "ci-monitor"),
	}
}

// WaitForCI polls until all required checks complete or timeout.
// Returns CISuccess if all checks pass, CIFailure if any fail,
// or error on context cancellation or timeout.
func (m *CIMonitor) WaitForCI(ctx context.Context, sha string) (CIStatus, error) {
	deadline := time.Now().Add(m.waitTimeout)
	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()

	// Log initial status
	m.log.Info("waiting for CI", "sha", ShortSHA(sha), "timeout", m.waitTimeout, "mode", m.ciChecks.Mode)

	for {
		select {
		case <-ctx.Done():
			return CIPending, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return CIPending, fmt.Errorf("CI timeout after %v", m.waitTimeout)
			}

			status, err := m.checkStatus(ctx, sha)
			if err != nil {
				m.log.Warn("CI status check failed", "error", err)
				continue
			}

			m.log.Info("CI status", "sha", ShortSHA(sha), "status", status)

			if status == CISuccess || status == CIFailure {
				return status, nil
			}
		}
	}
}

// checkStatus gets current CI status for a SHA.
func (m *CIMonitor) checkStatus(ctx context.Context, sha string) (CIStatus, error) {
	// Get check runs (GitHub Actions)
	checkRuns, err := m.ghClient.ListCheckRuns(ctx, m.owner, m.repo, sha)
	if err != nil {
		return CIPending, err
	}

	if m.ciChecks.Mode == "manual" {
		return m.checkRequiredRuns(checkRuns), nil
	}
	return m.checkAutoDiscoveredRuns(ctx, sha, checkRuns)
}

// checkRequiredRuns checks status of explicitly required checks (manual mode).
func (m *CIMonitor) checkRequiredRuns(checkRuns *github.CheckRunsResponse) CIStatus {
	required := m.ciChecks.Required
	if len(required) == 0 {
		return m.checkAllRuns(checkRuns)
	}

	// Track required checks
	requiredStatus := make(map[string]CIStatus)
	for _, name := range required {
		requiredStatus[name] = CIPending
	}

	// Map check runs to status
	for _, run := range checkRuns.CheckRuns {
		if _, ok := requiredStatus[run.Name]; ok {
			requiredStatus[run.Name] = m.mapCheckStatus(run.Status, run.Conclusion)
		}
	}

	// Determine overall status
	return m.aggregateStatus(requiredStatus)
}

// checkAutoDiscoveredRuns discovers checks from API and tracks their status.
func (m *CIMonitor) checkAutoDiscoveredRuns(ctx context.Context, sha string, checkRuns *github.CheckRunsResponse) (CIStatus, error) {
	m.mu.Lock()

	// Initialize discovery start time if needed
	if _, ok := m.discoveryStart[sha]; !ok {
		m.discoveryStart[sha] = time.Now()
	}
	startTime := m.discoveryStart[sha]

	// Filter checks based on exclusion patterns
	var checks []string
	for _, run := range checkRuns.CheckRuns {
		if !m.matchesExclude(run.Name) {
			checks = append(checks, run.Name)
		}
	}

	// Update discovered checks for this SHA
	m.discoveredChecks[sha] = checks
	m.mu.Unlock()

	// During grace period, wait for checks to appear
	if len(checks) == 0 {
		elapsed := time.Since(startTime)
		if elapsed < m.ciChecks.DiscoveryGracePeriod {
			m.log.Debug("waiting for checks to appear",
				"sha", ShortSHA(sha),
				"elapsed", elapsed,
				"grace_period", m.ciChecks.DiscoveryGracePeriod,
			)
			return CIPending, nil
		}
		// Grace period expired with no checks - consider success (no CI configured)
		m.log.Info("no CI checks discovered after grace period",
			"sha", ShortSHA(sha),
			"grace_period", m.ciChecks.DiscoveryGracePeriod,
		)
		return CISuccess, nil
	}

	// Build status map for discovered checks
	checkStatus := make(map[string]CIStatus)
	for _, run := range checkRuns.CheckRuns {
		if !m.matchesExclude(run.Name) {
			checkStatus[run.Name] = m.mapCheckStatus(run.Status, run.Conclusion)
		}
	}

	return m.aggregateStatus(checkStatus), nil
}

// matchesExclude checks if a check name matches any exclusion pattern.
func (m *CIMonitor) matchesExclude(name string) bool {
	for _, pattern := range m.ciChecks.Exclude {
		if matched, _ := path.Match(pattern, name); matched {
			return true
		}
	}
	return false
}

// GetDiscoveredChecks returns the list of discovered check names for a SHA.
func (m *CIMonitor) GetDiscoveredChecks(sha string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if checks, ok := m.discoveredChecks[sha]; ok {
		result := make([]string, len(checks))
		copy(result, checks)
		return result
	}
	return nil
}

// ClearDiscovery removes discovery state for a SHA.
func (m *CIMonitor) ClearDiscovery(sha string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.discoveredChecks, sha)
	delete(m.discoveryStart, sha)
}

// checkAllRuns returns aggregate status when no required checks are configured.
func (m *CIMonitor) checkAllRuns(checkRuns *github.CheckRunsResponse) CIStatus {
	if checkRuns.TotalCount == 0 {
		return CIPending
	}

	hasFailure := false
	hasPending := false

	for _, run := range checkRuns.CheckRuns {
		status := m.mapCheckStatus(run.Status, run.Conclusion)
		switch status {
		case CIFailure:
			hasFailure = true
		case CIPending, CIRunning:
			hasPending = true
		}
	}

	if hasFailure {
		return CIFailure
	}
	if hasPending {
		return CIPending
	}
	return CISuccess
}

// aggregateStatus determines overall status from individual check statuses.
func (m *CIMonitor) aggregateStatus(statuses map[string]CIStatus) CIStatus {
	hasFailure := false
	hasPending := false

	for _, status := range statuses {
		switch status {
		case CIFailure:
			hasFailure = true
		case CIPending, CIRunning:
			hasPending = true
		}
	}

	if hasFailure {
		return CIFailure
	}
	if hasPending {
		return CIPending
	}
	return CISuccess
}

// mapCheckStatus maps GitHub check status to CIStatus.
func (m *CIMonitor) mapCheckStatus(status, conclusion string) CIStatus {
	switch status {
	case github.CheckRunQueued, github.CheckRunInProgress:
		return CIRunning
	case github.CheckRunCompleted:
		switch conclusion {
		case github.ConclusionSuccess:
			return CISuccess
		case github.ConclusionFailure, github.ConclusionCancelled, github.ConclusionTimedOut:
			return CIFailure
		case github.ConclusionSkipped, github.ConclusionNeutral:
			// Skipped/neutral checks don't block
			return CISuccess
		default:
			return CIPending
		}
	default:
		return CIPending
	}
}

// CheckCI checks CI status once and returns immediately.
// This is the non-blocking alternative to WaitForCI.
// Returns CIPending/CIRunning if checks are still running.
func (m *CIMonitor) CheckCI(ctx context.Context, sha string) (CIStatus, error) {
	status, err := m.checkStatus(ctx, sha)
	if err != nil {
		m.log.Debug("CheckCI: status check failed",
			"sha", ShortSHA(sha),
			"error", err,
		)
		return status, err
	}

	m.log.Debug("CheckCI: status check complete",
		"sha", ShortSHA(sha),
		"status", status,
		"mode", m.ciChecks.Mode,
	)
	return status, nil
}

// GetCIStatus returns the current overall CI status for a SHA.
// This is useful for point-in-time status checks without waiting.
// Deprecated: Use CheckCI instead for clarity.
func (m *CIMonitor) GetCIStatus(ctx context.Context, sha string) (CIStatus, error) {
	return m.checkStatus(ctx, sha)
}

// GetFailedChecks returns names of failed checks for a SHA.
func (m *CIMonitor) GetFailedChecks(ctx context.Context, sha string) ([]string, error) {
	checkRuns, err := m.ghClient.ListCheckRuns(ctx, m.owner, m.repo, sha)
	if err != nil {
		return nil, err
	}

	var failed []string
	for _, run := range checkRuns.CheckRuns {
		if run.Conclusion == github.ConclusionFailure {
			failed = append(failed, run.Name)
		}
	}
	return failed, nil
}

// GetCheckStatus returns the current status of a specific check by name.
func (m *CIMonitor) GetCheckStatus(ctx context.Context, sha, checkName string) (CIStatus, error) {
	checkRuns, err := m.ghClient.ListCheckRuns(ctx, m.owner, m.repo, sha)
	if err != nil {
		return CIPending, err
	}

	for _, run := range checkRuns.CheckRuns {
		if run.Name == checkName {
			return m.mapCheckStatus(run.Status, run.Conclusion), nil
		}
	}

	return CIPending, nil
}
