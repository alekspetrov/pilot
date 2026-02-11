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
	ghClient       *github.Client
	owner          string
	repo           string
	pollInterval   time.Duration
	waitTimeout    time.Duration
	requiredChecks []string // Legacy: explicit required checks (manual mode)
	ciChecks       *CIChecksConfig
	log            *slog.Logger

	// Auto-discovery state (per SHA)
	mu              sync.RWMutex
	discoveredSHAs  map[string][]string   // SHA → discovered check names
	discoveryStart  map[string]time.Time  // SHA → when discovery started
}

// NewCIMonitor creates a CI monitor with configuration from Config.
// Uses DevCITimeout for dev environment, CIWaitTimeout for stage/prod.
// Supports both legacy RequiredChecks and new CIChecks config.
func NewCIMonitor(ghClient *github.Client, owner, repo string, cfg *Config) *CIMonitor {
	timeout := cfg.CIWaitTimeout
	if cfg.Environment == EnvDev && cfg.DevCITimeout > 0 {
		timeout = cfg.DevCITimeout
	}

	// Determine CI checks config with backward compatibility
	ciChecks := cfg.CIChecks
	if ciChecks == nil {
		ciChecks = DefaultCIChecksConfig()
	}

	// Migrate legacy RequiredChecks to manual mode
	if len(cfg.RequiredChecks) > 0 && ciChecks.Mode == CIChecksModeAuto {
		// Legacy config present: switch to manual mode
		ciChecks = &CIChecksConfig{
			Mode:                 CIChecksModeManual,
			Required:             cfg.RequiredChecks,
			Exclude:              ciChecks.Exclude,
			DiscoveryGracePeriod: ciChecks.DiscoveryGracePeriod,
		}
	}

	return &CIMonitor{
		ghClient:        ghClient,
		owner:           owner,
		repo:            repo,
		pollInterval:    cfg.CIPollInterval,
		waitTimeout:     timeout,
		requiredChecks:  cfg.RequiredChecks, // Keep for backward compat
		ciChecks:        ciChecks,
		log:             slog.Default().With("component", "ci-monitor"),
		discoveredSHAs:  make(map[string][]string),
		discoveryStart:  make(map[string]time.Time),
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
	m.log.Info("waiting for CI", "sha", ShortSHA(sha), "timeout", m.waitTimeout, "required_checks", m.requiredChecks)

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

	// Determine which checks to track based on mode
	checksToTrack := m.getChecksToTrack(sha, checkRuns)

	// If no checks to track and no runs exist, stay pending
	if len(checksToTrack) == 0 && checkRuns.TotalCount == 0 {
		return CIPending, nil
	}

	// If no explicit checks to track, check all runs (minus exclusions)
	if len(checksToTrack) == 0 {
		return m.checkAllRunsFiltered(checkRuns), nil
	}

	// Track specific checks
	requiredStatus := make(map[string]CIStatus)
	for _, name := range checksToTrack {
		requiredStatus[name] = CIPending
	}

	// Map check runs to status
	for _, run := range checkRuns.CheckRuns {
		if _, ok := requiredStatus[run.Name]; ok {
			requiredStatus[run.Name] = m.mapCheckStatus(run.Status, run.Conclusion)
		}
	}

	// Determine overall status
	return m.aggregateStatus(requiredStatus), nil
}

// getChecksToTrack returns the list of check names to monitor for a SHA.
// In manual mode, returns the configured Required checks.
// In auto mode, returns discovered checks (or empty during grace period).
func (m *CIMonitor) getChecksToTrack(sha string, checkRuns *github.CheckRunsResponse) []string {
	if m.ciChecks == nil || m.ciChecks.Mode == CIChecksModeManual {
		// Manual mode or legacy: use configured checks
		if len(m.ciChecks.Required) > 0 {
			return m.ciChecks.Required
		}
		return m.requiredChecks
	}

	// Auto mode: discover from API
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if we've already discovered checks for this SHA
	if discovered, ok := m.discoveredSHAs[sha]; ok {
		return discovered
	}

	// Track discovery start time
	if _, ok := m.discoveryStart[sha]; !ok {
		m.discoveryStart[sha] = time.Now()
		m.log.Info("starting CI check discovery",
			"sha", ShortSHA(sha),
			"grace_period", m.ciChecks.DiscoveryGracePeriod,
		)
	}

	// Check if we're still in grace period
	gracePeriod := m.ciChecks.DiscoveryGracePeriod
	if gracePeriod == 0 {
		gracePeriod = 60 * time.Second
	}
	elapsed := time.Since(m.discoveryStart[sha])
	if elapsed < gracePeriod {
		// Still discovering - don't lock in yet
		m.log.Debug("CI discovery in progress",
			"sha", ShortSHA(sha),
			"elapsed", elapsed.Round(time.Second),
			"checks_found", len(checkRuns.CheckRuns),
		)
		return nil // Will use checkAllRunsFiltered
	}

	// Grace period expired: lock in discovered checks
	discovered := m.discoverChecks(checkRuns)
	m.discoveredSHAs[sha] = discovered
	m.log.Info("CI checks discovered",
		"sha", ShortSHA(sha),
		"checks", discovered,
		"count", len(discovered),
	)
	return discovered
}

// discoverChecks extracts unique check names from check runs, applying exclusions.
func (m *CIMonitor) discoverChecks(checkRuns *github.CheckRunsResponse) []string {
	seen := make(map[string]bool)
	var checks []string

	for _, run := range checkRuns.CheckRuns {
		if seen[run.Name] {
			continue
		}
		// Apply exclusion patterns
		if m.matchesExclude(run.Name) {
			m.log.Debug("excluding check", "name", run.Name)
			continue
		}
		seen[run.Name] = true
		checks = append(checks, run.Name)
	}
	return checks
}

// matchesExclude returns true if the check name matches any exclusion pattern.
func (m *CIMonitor) matchesExclude(name string) bool {
	if m.ciChecks == nil {
		return false
	}
	for _, pattern := range m.ciChecks.Exclude {
		matched, err := path.Match(pattern, name)
		if err != nil {
			m.log.Warn("invalid exclude pattern", "pattern", pattern, "error", err)
			continue
		}
		if matched {
			return true
		}
	}
	return false
}

// checkAllRunsFiltered returns aggregate status for all runs, excluding filtered checks.
func (m *CIMonitor) checkAllRunsFiltered(checkRuns *github.CheckRunsResponse) CIStatus {
	if checkRuns.TotalCount == 0 {
		return CIPending
	}

	hasFailure := false
	hasPending := false
	checkedAny := false

	for _, run := range checkRuns.CheckRuns {
		// Skip excluded checks
		if m.matchesExclude(run.Name) {
			continue
		}
		checkedAny = true
		status := m.mapCheckStatus(run.Status, run.Conclusion)
		switch status {
		case CIFailure:
			hasFailure = true
		case CIPending, CIRunning:
			hasPending = true
		}
	}

	if !checkedAny {
		return CIPending
	}
	if hasFailure {
		return CIFailure
	}
	if hasPending {
		return CIPending
	}
	return CISuccess
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
		"required_checks", m.requiredChecks,
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

// GetDiscoveredChecks returns the discovered CI checks for a SHA.
// Returns nil if discovery hasn't completed for this SHA.
func (m *CIMonitor) GetDiscoveredChecks(sha string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.discoveredSHAs[sha]
}

// ClearDiscovery removes discovery state for a SHA.
// Call this when a PR is removed from tracking.
func (m *CIMonitor) ClearDiscovery(sha string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.discoveredSHAs, sha)
	delete(m.discoveryStart, sha)
}

// GetCIChecksMode returns the current CI checks mode (auto or manual).
func (m *CIMonitor) GetCIChecksMode() CIChecksMode {
	if m.ciChecks == nil {
		return CIChecksModeManual
	}
	return m.ciChecks.Mode
}

// IsAutoDiscoveryEnabled returns true if auto-discovery mode is active.
func (m *CIMonitor) IsAutoDiscoveryEnabled() bool {
	return m.ciChecks != nil && m.ciChecks.Mode == CIChecksModeAuto
}
