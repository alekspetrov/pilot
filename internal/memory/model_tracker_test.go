package memory

import (
	"os"
	"testing"
	"time"
)

func newTestTracker(t *testing.T, opts ...ModelOutcomeTrackerOption) *ModelOutcomeTracker {
	t.Helper()
	dir, err := os.MkdirTemp("", "model-tracker-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	return NewModelOutcomeTracker(store, opts...)
}

func TestRecordOutcome(t *testing.T) {
	tracker := newTestTracker(t)

	err := tracker.RecordOutcome("build", "claude-haiku-4-5", "success", 1000, 5*time.Second)
	if err != nil {
		t.Fatalf("RecordOutcome: %v", err)
	}

	// Verify it was stored
	var count int
	err = tracker.store.db.QueryRow("SELECT COUNT(*) FROM model_outcomes").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row, got %d", count)
	}
}

func TestGetFailureRate(t *testing.T) {
	tests := []struct {
		name     string
		outcomes []string
		wantRate float64
	}{
		{
			name:     "all success",
			outcomes: []string{"success", "success", "success"},
			wantRate: 0.0,
		},
		{
			name:     "all failure",
			outcomes: []string{"failure", "failure", "failure"},
			wantRate: 1.0,
		},
		{
			name:     "mixed outcomes",
			outcomes: []string{"success", "failure", "success", "failure", "success"},
			wantRate: 0.4,
		},
		{
			name:     "includes error and killed",
			outcomes: []string{"success", "error", "killed", "success"},
			wantRate: 0.5,
		},
		{
			name:     "empty data",
			outcomes: nil,
			wantRate: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := newTestTracker(t)
			for _, outcome := range tt.outcomes {
				if err := tracker.RecordOutcome("test-task", "claude-haiku-4-5", outcome, 500, time.Second); err != nil {
					t.Fatal(err)
				}
			}
			got := tracker.GetFailureRate("test-task", "claude-haiku-4-5")
			if got != tt.wantRate {
				t.Errorf("GetFailureRate() = %v, want %v", got, tt.wantRate)
			}
		})
	}
}

func TestGetFailureRateWindowLimit(t *testing.T) {
	tracker := newTestTracker(t, WithRecentWindowSize(3))

	// Record 7 failures then 3 successes (most recent)
	for i := 0; i < 7; i++ {
		if err := tracker.RecordOutcome("task", "claude-haiku-4-5", "failure", 100, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := tracker.RecordOutcome("task", "claude-haiku-4-5", "success", 100, time.Second); err != nil {
			t.Fatal(err)
		}
	}

	// Window of 3 should only see the 3 most recent (all success)
	got := tracker.GetFailureRate("task", "claude-haiku-4-5")
	if got != 0.0 {
		t.Errorf("GetFailureRate() = %v, want 0.0 (window should see only recent successes)", got)
	}
}

func TestShouldEscalate(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		outcomes  []string
		wantEsc   bool
		wantModel string
	}{
		{
			name:      "haiku failing escalates to sonnet",
			model:     "claude-haiku-4-5",
			outcomes:  []string{"failure", "failure", "failure", "success"},
			wantEsc:   true,
			wantModel: "claude-sonnet-4-6",
		},
		{
			name:      "sonnet failing escalates to opus",
			model:     "claude-sonnet-4-6",
			outcomes:  []string{"failure", "failure", "success"},
			wantEsc:   true,
			wantModel: "claude-opus-4-6",
		},
		{
			name:      "opus failing cannot escalate further",
			model:     "claude-opus-4-6",
			outcomes:  []string{"failure", "failure", "failure"},
			wantEsc:   false,
			wantModel: "",
		},
		{
			name:      "low failure rate no escalation",
			model:     "claude-haiku-4-5",
			outcomes:  []string{"success", "success", "success", "failure"},
			wantEsc:   false,
			wantModel: "",
		},
		{
			name:      "unknown model no escalation",
			model:     "some-other-model",
			outcomes:  []string{"failure", "failure", "failure"},
			wantEsc:   false,
			wantModel: "",
		},
		{
			name:      "no data no escalation",
			model:     "claude-haiku-4-5",
			outcomes:  nil,
			wantEsc:   false,
			wantModel: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := newTestTracker(t)
			for _, outcome := range tt.outcomes {
				if err := tracker.RecordOutcome("build", tt.model, outcome, 500, time.Second); err != nil {
					t.Fatal(err)
				}
			}
			gotEsc, gotModel := tracker.ShouldEscalate("build", tt.model)
			if gotEsc != tt.wantEsc {
				t.Errorf("ShouldEscalate() escalate = %v, want %v", gotEsc, tt.wantEsc)
			}
			if gotModel != tt.wantModel {
				t.Errorf("ShouldEscalate() model = %q, want %q", gotModel, tt.wantModel)
			}
		})
	}
}

func TestThresholdBoundary(t *testing.T) {
	// Exactly at threshold (30%) should NOT escalate (uses >)
	tracker := newTestTracker(t, WithFailureThreshold(0.3))

	// 3 failures out of 10 = exactly 0.3
	for i := 0; i < 7; i++ {
		if err := tracker.RecordOutcome("task", "claude-haiku-4-5", "success", 100, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		if err := tracker.RecordOutcome("task", "claude-haiku-4-5", "failure", 100, time.Second); err != nil {
			t.Fatal(err)
		}
	}

	esc, _ := tracker.ShouldEscalate("task", "claude-haiku-4-5")
	if esc {
		t.Error("should NOT escalate at exactly threshold (0.3)")
	}

	// One more failure tips it over: 4/10 = 0.4 but window recalculates from latest 10
	// Add one more failure to push past
	if err := tracker.RecordOutcome("task", "claude-haiku-4-5", "failure", 100, time.Second); err != nil {
		t.Fatal(err)
	}

	// Now window is last 10: 6 success + 4 failure = 0.4
	esc, model := tracker.ShouldEscalate("task", "claude-haiku-4-5")
	if !esc {
		t.Error("should escalate above threshold")
	}
	if model != "claude-sonnet-4-6" {
		t.Errorf("expected claude-sonnet-4-6, got %s", model)
	}
}

func TestCustomThreshold(t *testing.T) {
	tracker := newTestTracker(t, WithFailureThreshold(0.5))

	// 4/10 = 0.4, below 0.5 threshold
	for i := 0; i < 6; i++ {
		if err := tracker.RecordOutcome("task", "claude-haiku-4-5", "success", 100, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 4; i++ {
		if err := tracker.RecordOutcome("task", "claude-haiku-4-5", "failure", 100, time.Second); err != nil {
			t.Fatal(err)
		}
	}

	esc, _ := tracker.ShouldEscalate("task", "claude-haiku-4-5")
	if esc {
		t.Error("should not escalate at 0.4 with 0.5 threshold")
	}
}

func TestIsolationByTaskType(t *testing.T) {
	tracker := newTestTracker(t)

	// Task A: all failures
	for i := 0; i < 5; i++ {
		if err := tracker.RecordOutcome("task-a", "claude-haiku-4-5", "failure", 100, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	// Task B: all success
	for i := 0; i < 5; i++ {
		if err := tracker.RecordOutcome("task-b", "claude-haiku-4-5", "success", 100, time.Second); err != nil {
			t.Fatal(err)
		}
	}

	rateA := tracker.GetFailureRate("task-a", "claude-haiku-4-5")
	rateB := tracker.GetFailureRate("task-b", "claude-haiku-4-5")

	if rateA != 1.0 {
		t.Errorf("task-a failure rate = %v, want 1.0", rateA)
	}
	if rateB != 0.0 {
		t.Errorf("task-b failure rate = %v, want 0.0", rateB)
	}
}

func TestGetOutcomeStats(t *testing.T) {
	tracker := newTestTracker(t)

	if err := tracker.RecordOutcome("build", "claude-haiku-4-5", "success", 1000, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := tracker.RecordOutcome("build", "claude-haiku-4-5", "failure", 2000, 10*time.Second); err != nil {
		t.Fatal(err)
	}

	total, failures, avgTokens, err := tracker.GetOutcomeStats("build", "claude-haiku-4-5")
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if failures != 1 {
		t.Errorf("failures = %d, want 1", failures)
	}
	if avgTokens != 1500.0 {
		t.Errorf("avgTokens = %v, want 1500", avgTokens)
	}
}
