package executor

import (
	"bytes"
	"sync"
	"text/template"
	"time"

	"github.com/alekspetrov/pilot/internal/memory"
)

// DriftType represents the type of collaboration drift
type DriftType string

const (
	DriftRepeatedCorrection DriftType = "repeated_correction"
	DriftContextConfusion   DriftType = "context_confusion"
	DriftQualityDrop        DriftType = "quality_drop"
)

// DriftIndicator represents a sign of collaboration drift
type DriftIndicator struct {
	Type       DriftType
	Count      int
	LastSeen   time.Time
	Pattern    string
	Correction string
}

// DriftConfig configures drift detection thresholds
type DriftConfig struct {
	// Threshold is the number of corrections before triggering re-anchor
	Threshold int
	// WindowDuration is how long to track corrections (default 1 hour)
	WindowDuration time.Duration
	// Enabled controls whether drift detection is active
	Enabled bool
}

// DefaultDriftConfig returns default drift detection configuration
func DefaultDriftConfig() *DriftConfig {
	return &DriftConfig{
		Threshold:      3,
		WindowDuration: time.Hour,
		Enabled:        true,
	}
}

// DriftDetector monitors for collaboration drift
type DriftDetector struct {
	mu           sync.RWMutex
	indicators   []DriftIndicator
	config       *DriftConfig
	profileStore *memory.ProfileManager
}

// NewDriftDetector creates a drift detector
func NewDriftDetector(config *DriftConfig, profile *memory.ProfileManager) *DriftDetector {
	if config == nil {
		config = DefaultDriftConfig()
	}
	return &DriftDetector{
		indicators:   make([]DriftIndicator, 0),
		config:       config,
		profileStore: profile,
	}
}

// RecordCorrection logs a user correction
func (d *DriftDetector) RecordCorrection(pattern, correction string) {
	if !d.config.Enabled {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()

	// Check if pattern seen before
	for i := range d.indicators {
		if d.indicators[i].Pattern == pattern {
			d.indicators[i].Count++
			d.indicators[i].LastSeen = now
			d.indicators[i].Correction = correction
			d.persistCorrection(pattern, correction)
			return
		}
	}

	// New pattern
	d.indicators = append(d.indicators, DriftIndicator{
		Type:       DriftRepeatedCorrection,
		Count:      1,
		LastSeen:   now,
		Pattern:    pattern,
		Correction: correction,
	})

	d.persistCorrection(pattern, correction)
}

// RecordContextConfusion logs when the AI shows context confusion
func (d *DriftDetector) RecordContextConfusion(pattern string) {
	if !d.config.Enabled {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()

	for i := range d.indicators {
		if d.indicators[i].Type == DriftContextConfusion && d.indicators[i].Pattern == pattern {
			d.indicators[i].Count++
			d.indicators[i].LastSeen = now
			return
		}
	}

	d.indicators = append(d.indicators, DriftIndicator{
		Type:     DriftContextConfusion,
		Count:    1,
		LastSeen: now,
		Pattern:  pattern,
	})
}

// RecordQualityDrop logs when output quality drops
func (d *DriftDetector) RecordQualityDrop(pattern string) {
	if !d.config.Enabled {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()

	for i := range d.indicators {
		if d.indicators[i].Type == DriftQualityDrop && d.indicators[i].Pattern == pattern {
			d.indicators[i].Count++
			d.indicators[i].LastSeen = now
			return
		}
	}

	d.indicators = append(d.indicators, DriftIndicator{
		Type:     DriftQualityDrop,
		Count:    1,
		LastSeen: now,
		Pattern:  pattern,
	})
}

// ShouldReanchor returns true if drift detected
func (d *DriftDetector) ShouldReanchor() bool {
	if !d.config.Enabled {
		return false
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	// Prune old indicators
	cutoff := time.Now().Add(-d.config.WindowDuration)
	recentCount := 0

	for _, ind := range d.indicators {
		if ind.LastSeen.After(cutoff) {
			recentCount += ind.Count
		}
	}

	return recentCount >= d.config.Threshold
}

// GetRecentCorrections returns corrections within the window
func (d *DriftDetector) GetRecentCorrections() []DriftIndicator {
	d.mu.RLock()
	defer d.mu.RUnlock()

	cutoff := time.Now().Add(-d.config.WindowDuration)
	var recent []DriftIndicator

	for _, ind := range d.indicators {
		if ind.LastSeen.After(cutoff) {
			recent = append(recent, ind)
		}
	}

	return recent
}

// reanchorTemplate is the template for re-anchor prompts
const reanchorTemplate = `
## Re-Anchoring Required

Recent corrections indicate misalignment. Before proceeding:

1. Review recent corrections:
{{range .Corrections}}
   - Pattern: {{.Pattern}} → Correction: {{.Correction}}
{{end}}

2. Confirm understanding of user preferences
3. Apply corrections to current work
4. Proceed with adjusted approach
`

// GetReanchorPrompt returns prompt to restore alignment
func (d *DriftDetector) GetReanchorPrompt() string {
	corrections := d.GetRecentCorrections()
	if len(corrections) == 0 {
		return ""
	}

	tmpl, err := template.New("reanchor").Parse(reanchorTemplate)
	if err != nil {
		// Fallback to simple format
		return "## Re-Anchoring Required\n\nRecent corrections detected. Please review user preferences before proceeding."
	}

	var buf bytes.Buffer
	data := struct {
		Corrections []DriftIndicator
	}{
		Corrections: corrections,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "## Re-Anchoring Required\n\nRecent corrections detected. Please review user preferences before proceeding."
	}

	return buf.String()
}

// Reset clears drift indicators after successful re-anchor
func (d *DriftDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.indicators = d.indicators[:0]
}

// persistCorrection saves correction to profile store if available
func (d *DriftDetector) persistCorrection(pattern, correction string) {
	if d.profileStore == nil {
		return
	}

	profile, err := d.profileStore.Load()
	if err != nil {
		return
	}

	profile.RecordCorrection(pattern, correction)

	// Save to project-level profile (not global)
	_ = d.profileStore.Save(profile, false)
}

// CorrectionCount returns total correction count in current window
func (d *DriftDetector) CorrectionCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	cutoff := time.Now().Add(-d.config.WindowDuration)
	count := 0

	for _, ind := range d.indicators {
		if ind.LastSeen.After(cutoff) {
			count += ind.Count
		}
	}

	return count
}

// IndicatorCount returns the number of unique indicators
func (d *DriftDetector) IndicatorCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.indicators)
}
