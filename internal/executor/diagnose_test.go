package executor

import (
	"strings"
	"testing"
	"time"
)

func TestNewDriftDetector(t *testing.T) {
	t.Run("with nil config uses defaults", func(t *testing.T) {
		d := NewDriftDetector(nil, nil)
		if d == nil {
			t.Fatal("expected non-nil detector")
		}
		if d.config.Threshold != 3 {
			t.Errorf("expected default threshold 3, got %d", d.config.Threshold)
		}
		if d.config.WindowDuration != time.Hour {
			t.Errorf("expected default window 1h, got %v", d.config.WindowDuration)
		}
		if !d.config.Enabled {
			t.Error("expected enabled by default")
		}
	})

	t.Run("with custom config", func(t *testing.T) {
		cfg := &DriftConfig{
			Threshold:      5,
			WindowDuration: 30 * time.Minute,
			Enabled:        true,
		}
		d := NewDriftDetector(cfg, nil)
		if d.config.Threshold != 5 {
			t.Errorf("expected threshold 5, got %d", d.config.Threshold)
		}
	})
}

func TestDriftDetector_RecordCorrection(t *testing.T) {
	t.Run("records new correction", func(t *testing.T) {
		d := NewDriftDetector(nil, nil)
		d.RecordCorrection("use tabs", "always use tabs for indentation")

		if d.IndicatorCount() != 1 {
			t.Errorf("expected 1 indicator, got %d", d.IndicatorCount())
		}

		corrections := d.GetRecentCorrections()
		if len(corrections) != 1 {
			t.Fatalf("expected 1 correction, got %d", len(corrections))
		}
		if corrections[0].Pattern != "use tabs" {
			t.Errorf("expected pattern 'use tabs', got %q", corrections[0].Pattern)
		}
		if corrections[0].Count != 1 {
			t.Errorf("expected count 1, got %d", corrections[0].Count)
		}
	})

	t.Run("increments existing correction", func(t *testing.T) {
		d := NewDriftDetector(nil, nil)
		d.RecordCorrection("use tabs", "always use tabs")
		d.RecordCorrection("use tabs", "always use tabs")
		d.RecordCorrection("use tabs", "always use tabs")

		if d.IndicatorCount() != 1 {
			t.Errorf("expected 1 indicator, got %d", d.IndicatorCount())
		}

		corrections := d.GetRecentCorrections()
		if corrections[0].Count != 3 {
			t.Errorf("expected count 3, got %d", corrections[0].Count)
		}
	})

	t.Run("disabled detector does nothing", func(t *testing.T) {
		cfg := &DriftConfig{Enabled: false}
		d := NewDriftDetector(cfg, nil)
		d.RecordCorrection("use tabs", "always use tabs")

		if d.IndicatorCount() != 0 {
			t.Errorf("expected 0 indicators when disabled, got %d", d.IndicatorCount())
		}
	})
}

func TestDriftDetector_RecordContextConfusion(t *testing.T) {
	d := NewDriftDetector(nil, nil)
	d.RecordContextConfusion("forgot earlier instruction")
	d.RecordContextConfusion("forgot earlier instruction")

	if d.IndicatorCount() != 1 {
		t.Errorf("expected 1 indicator, got %d", d.IndicatorCount())
	}

	corrections := d.GetRecentCorrections()
	if corrections[0].Type != DriftContextConfusion {
		t.Errorf("expected type context_confusion, got %s", corrections[0].Type)
	}
	if corrections[0].Count != 2 {
		t.Errorf("expected count 2, got %d", corrections[0].Count)
	}
}

func TestDriftDetector_RecordQualityDrop(t *testing.T) {
	d := NewDriftDetector(nil, nil)
	d.RecordQualityDrop("code style degraded")

	corrections := d.GetRecentCorrections()
	if len(corrections) != 1 {
		t.Fatalf("expected 1 correction, got %d", len(corrections))
	}
	if corrections[0].Type != DriftQualityDrop {
		t.Errorf("expected type quality_drop, got %s", corrections[0].Type)
	}
}

func TestDriftDetector_ShouldReanchor(t *testing.T) {
	t.Run("returns false when below threshold", func(t *testing.T) {
		d := NewDriftDetector(nil, nil)
		d.RecordCorrection("pattern1", "correction1")
		d.RecordCorrection("pattern2", "correction2")

		if d.ShouldReanchor() {
			t.Error("expected false when below threshold")
		}
	})

	t.Run("returns true when at threshold", func(t *testing.T) {
		d := NewDriftDetector(nil, nil)
		d.RecordCorrection("pattern1", "correction1")
		d.RecordCorrection("pattern2", "correction2")
		d.RecordCorrection("pattern3", "correction3")

		if !d.ShouldReanchor() {
			t.Error("expected true when at threshold")
		}
	})

	t.Run("returns true when above threshold via repeated pattern", func(t *testing.T) {
		d := NewDriftDetector(nil, nil)
		d.RecordCorrection("same pattern", "correction")
		d.RecordCorrection("same pattern", "correction")
		d.RecordCorrection("same pattern", "correction")

		if !d.ShouldReanchor() {
			t.Error("expected true when same pattern repeated to threshold")
		}
	})

	t.Run("returns false when disabled", func(t *testing.T) {
		cfg := &DriftConfig{Enabled: false, Threshold: 1}
		d := NewDriftDetector(cfg, nil)
		// Can't record when disabled, but test the method directly
		if d.ShouldReanchor() {
			t.Error("expected false when disabled")
		}
	})
}

func TestDriftDetector_GetReanchorPrompt(t *testing.T) {
	t.Run("returns empty when no corrections", func(t *testing.T) {
		d := NewDriftDetector(nil, nil)
		prompt := d.GetReanchorPrompt()
		if prompt != "" {
			t.Errorf("expected empty prompt, got %q", prompt)
		}
	})

	t.Run("returns prompt with corrections", func(t *testing.T) {
		d := NewDriftDetector(nil, nil)
		d.RecordCorrection("use tabs", "always use tabs")
		d.RecordCorrection("no emojis", "avoid emojis in code")

		prompt := d.GetReanchorPrompt()
		if !strings.Contains(prompt, "Re-Anchoring Required") {
			t.Error("expected prompt to contain 'Re-Anchoring Required'")
		}
		if !strings.Contains(prompt, "use tabs") {
			t.Error("expected prompt to contain pattern 'use tabs'")
		}
		if !strings.Contains(prompt, "no emojis") {
			t.Error("expected prompt to contain pattern 'no emojis'")
		}
	})
}

func TestDriftDetector_Reset(t *testing.T) {
	d := NewDriftDetector(nil, nil)
	d.RecordCorrection("pattern1", "correction1")
	d.RecordCorrection("pattern2", "correction2")
	d.RecordCorrection("pattern3", "correction3")

	if !d.ShouldReanchor() {
		t.Fatal("expected to need reanchor before reset")
	}

	d.Reset()

	if d.ShouldReanchor() {
		t.Error("expected not to need reanchor after reset")
	}
	if d.IndicatorCount() != 0 {
		t.Errorf("expected 0 indicators after reset, got %d", d.IndicatorCount())
	}
}

func TestDriftDetector_CorrectionCount(t *testing.T) {
	d := NewDriftDetector(nil, nil)
	d.RecordCorrection("pattern1", "correction1")
	d.RecordCorrection("pattern1", "correction1") // Same pattern, count = 2
	d.RecordCorrection("pattern2", "correction2") // New pattern, count = 1

	// Total should be 3 (2 + 1)
	if count := d.CorrectionCount(); count != 3 {
		t.Errorf("expected correction count 3, got %d", count)
	}
}

func TestDriftConfig_Defaults(t *testing.T) {
	cfg := DefaultDriftConfig()
	if cfg.Threshold != 3 {
		t.Errorf("expected threshold 3, got %d", cfg.Threshold)
	}
	if cfg.WindowDuration != time.Hour {
		t.Errorf("expected window 1h, got %v", cfg.WindowDuration)
	}
	if !cfg.Enabled {
		t.Error("expected enabled by default")
	}
}

func TestDriftType_Values(t *testing.T) {
	tests := []struct {
		dt       DriftType
		expected string
	}{
		{DriftRepeatedCorrection, "repeated_correction"},
		{DriftContextConfusion, "context_confusion"},
		{DriftQualityDrop, "quality_drop"},
	}

	for _, tt := range tests {
		if string(tt.dt) != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, tt.dt)
		}
	}
}
