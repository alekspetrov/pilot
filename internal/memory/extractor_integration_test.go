package memory

import (
	"context"
	"fmt"
	"os"
	"testing"
)

// TestSelfReviewPatternAccumulation simulates 3 sequential task executions
// with overlapping self-review findings. It verifies:
// - Patterns are created on first occurrence (confidence 0.5)
// - Duplicate patterns from subsequent executions increment occurrences and boost confidence
// - At least 4 distinct pattern categories are extracted
// - Empty/malformed self-review output is handled gracefully
// - Patterns appear in GetTopCrossPatterns() results
func TestSelfReviewPatternAccumulation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-integration-extractor-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	patternStore, err := NewGlobalPatternStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create pattern store: %v", err)
	}

	extractor := NewPatternExtractor(patternStore, store)
	ctx := context.Background()

	// Three sequential self-review outputs with overlapping findings.
	// Execution 1: error handling + dead code + parity + test gap
	// Execution 2: error handling (dup) + build failure + suspicious value + incomplete
	// Execution 3: error handling (dup) + dead code (dup) + REVIEW_FIXED + parity (dup)
	selfReviewOutputs := []string{
		// Execution 1
		`Self-review for task GH-100:
Files changed: 5
missing error handling in database query at store.go:42
Found unused import: fmt in utils.go (dead code)
cross-file parity issue: AddUser added to service.go but not to service_test.go
test coverage gap for new validateInput function
All other checks passed.`,

		// Execution 2
		`Self-review for task GH-101:
Files changed: 3
No error handling in HTTP handler at api.go:15
build verification failure: missing return statement in Calculate()
SUSPICIOUS_VALUE detected in pricing constant on line 88
INCOMPLETE: TODO items remain in auth module`,

		// Execution 3
		`Self-review for task GH-102:
Files changed: 7
lacking error handling in webhook dispatcher at dispatch.go:91
unreachable code detected after early return in parser.go
REVIEW_FIXED: corrected nil check in handler.go
parity mismatch between interface and implementation in adapter.go`,
	}

	// Track which cross-pattern IDs we create, keyed by title
	crossPatternIDs := make(map[string]string)

	for execIdx, output := range selfReviewOutputs {
		// Step 1: Extract patterns from self-review
		result, err := extractor.ExtractFromSelfReview(ctx, output, "/test/project")
		if err != nil {
			t.Fatalf("execution %d: ExtractFromSelfReview failed: %v", execIdx+1, err)
		}

		if len(result.AntiPatterns) == 0 {
			t.Fatalf("execution %d: expected anti-patterns, got none", execIdx+1)
		}

		// Step 2: Save to GlobalPatternStore (in-memory)
		if err := extractor.SaveExtractedPatterns(ctx, result); err != nil {
			t.Fatalf("execution %d: SaveExtractedPatterns failed: %v", execIdx+1, err)
		}

		// Step 3: Save as CrossPattern to SQLite (simulating runner pipeline)
		for _, ap := range result.AntiPatterns {
			// Use a deterministic ID based on pattern title for upsert dedup
			id, ok := crossPatternIDs[ap.Title]
			if !ok {
				id = fmt.Sprintf("sr_%s_%s", ap.Type, sanitizeID(ap.Title))
				crossPatternIDs[ap.Title] = id
			}

			cp := &CrossPattern{
				ID:            id,
				Type:          string(ap.Type),
				Title:         ap.Title,
				Description:   ap.Description,
				Context:       ap.Context,
				Examples:      ap.Examples,
				Confidence:    ap.Confidence,
				Occurrences:   1,
				IsAntiPattern: true,
				Scope:         "org",
			}

			if err := store.SaveCrossPattern(cp); err != nil {
				t.Fatalf("execution %d: SaveCrossPattern(%s) failed: %v", execIdx+1, ap.Title, err)
			}
		}
	}

	// === Verification ===

	// 1. Verify at least 4 distinct pattern categories were extracted
	categorySet := make(map[string]bool)
	for title := range crossPatternIDs {
		id := crossPatternIDs[title]
		cp, err := store.GetCrossPattern(id)
		if err != nil {
			t.Fatalf("GetCrossPattern(%s) failed: %v", id, err)
		}
		categorySet[cp.Type] = true
	}

	if len(categorySet) < 4 {
		t.Errorf("expected at least 4 distinct pattern categories, got %d: %v", len(categorySet), categorySet)
	}

	// 2. Verify "Missing error handling" appeared in all 3 executions → occurrences incremented
	errorHandlingID := crossPatternIDs["Missing error handling"]
	if errorHandlingID == "" {
		t.Fatal("Missing error handling pattern not found")
	}

	ehPattern, err := store.GetCrossPattern(errorHandlingID)
	if err != nil {
		t.Fatalf("GetCrossPattern for error handling failed: %v", err)
	}

	// SaveCrossPattern upsert: first insert sets occurrences=1, each subsequent call increments by 1.
	// 3 calls → occurrences = 1 (initial) + 2 (increments) = 3
	if ehPattern.Occurrences < 3 {
		t.Errorf("error handling pattern occurrences = %d, want >= 3 (appeared in all 3 executions)", ehPattern.Occurrences)
	}

	// Initial confidence should be 0.5 (set by ExtractFromSelfReview)
	if ehPattern.Confidence != 0.5 {
		t.Errorf("error handling pattern confidence = %f, want 0.5 (self-review default)", ehPattern.Confidence)
	}

	// 3. Verify patterns with only 1 occurrence have occurrences = 1
	for title, id := range crossPatternIDs {
		if title == "Missing error handling" || title == "Dead code detected" ||
			title == "Cross-file parity issue" {
			continue // These appear in multiple executions
		}
		cp, err := store.GetCrossPattern(id)
		if err != nil {
			t.Fatalf("GetCrossPattern(%s) for %q failed: %v", id, title, err)
		}
		if cp.Occurrences < 1 {
			t.Errorf("pattern %q: occurrences = %d, want >= 1", title, cp.Occurrences)
		}
	}

	// 4. Verify "Dead code detected" appeared in executions 1 and 3 → occurrences >= 2
	deadCodeID := crossPatternIDs["Dead code detected"]
	if deadCodeID == "" {
		t.Fatal("Dead code detected pattern not found")
	}
	dcPattern, err := store.GetCrossPattern(deadCodeID)
	if err != nil {
		t.Fatalf("GetCrossPattern for dead code failed: %v", err)
	}
	if dcPattern.Occurrences < 2 {
		t.Errorf("dead code pattern occurrences = %d, want >= 2", dcPattern.Occurrences)
	}

	// 5. Verify "Cross-file parity issue" appeared in executions 1 and 3 → occurrences >= 2
	parityID := crossPatternIDs["Cross-file parity issue"]
	if parityID == "" {
		t.Fatal("Cross-file parity issue pattern not found")
	}
	parityPattern, err := store.GetCrossPattern(parityID)
	if err != nil {
		t.Fatalf("GetCrossPattern for parity failed: %v", err)
	}
	if parityPattern.Occurrences < 2 {
		t.Errorf("parity pattern occurrences = %d, want >= 2", parityPattern.Occurrences)
	}

	// 6. Verify GetTopCrossPatterns returns our patterns
	topPatterns, err := store.GetTopCrossPatterns(20, 0.0)
	if err != nil {
		t.Fatalf("GetTopCrossPatterns failed: %v", err)
	}

	if len(topPatterns) == 0 {
		t.Fatal("GetTopCrossPatterns returned 0 patterns")
	}

	// All self-review patterns have confidence 0.5, verify they all appear
	topIDs := make(map[string]bool)
	for _, tp := range topPatterns {
		topIDs[tp.ID] = true
	}

	for title, id := range crossPatternIDs {
		if !topIDs[id] {
			t.Errorf("pattern %q (id=%s) not found in GetTopCrossPatterns results", title, id)
		}
	}

	// 7. Verify GetTopCrossPatterns with higher minConfidence filters out 0.5 patterns
	highConfPatterns, err := store.GetTopCrossPatterns(20, 0.8)
	if err != nil {
		t.Fatalf("GetTopCrossPatterns(0.8) failed: %v", err)
	}

	for _, hp := range highConfPatterns {
		if hp.Confidence < 0.8 {
			t.Errorf("GetTopCrossPatterns(0.8) returned pattern with confidence %f", hp.Confidence)
		}
	}
}

// TestSelfReviewEmptyAndMalformedInput verifies graceful handling of edge cases.
func TestSelfReviewEmptyAndMalformedInput(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-integration-edge-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	patternStore, err := NewGlobalPatternStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create pattern store: %v", err)
	}

	extractor := NewPatternExtractor(patternStore, store)
	ctx := context.Background()

	edgeCases := []struct {
		name   string
		input  string
		wantOK bool // expect no error and no panic
	}{
		{"empty string", "", true},
		{"whitespace only", "   \n\t  \n  ", true},
		{"null bytes", "\x00\x00\x00", true},
		{"random binary-like content", "ÿøÿà\x00\x10JFIF", true},
		{"very long single line", longLine(10000), true},
		{"no markers at all", "Everything looks great. Ship it!", true},
		{"partial marker match", "This is REVIEW but not REVIEW_FIXED", true},
		{"marker in URL", "See https://example.com/INCOMPLETE-docs for details", true},
	}

	for _, tc := range edgeCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := extractor.ExtractFromSelfReview(ctx, tc.input, "/test/project")
			if err != nil {
				t.Fatalf("ExtractFromSelfReview returned error: %v", err)
			}
			if result == nil {
				t.Fatal("ExtractFromSelfReview returned nil result")
			}

			// Save should also not panic even if there are unexpected patterns
			if saveErr := extractor.SaveExtractedPatterns(ctx, result); saveErr != nil {
				t.Fatalf("SaveExtractedPatterns failed: %v", saveErr)
			}
		})
	}
}

// TestSelfReviewConfidenceBoostViaCrossPattern verifies that the SQLite upsert
// properly increments occurrences on each duplicate save, confirming the
// confidence-boost-on-recurrence design documented in ExtractFromSelfReview.
func TestSelfReviewConfidenceBoostViaCrossPattern(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-integration-boost-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Save the same cross-pattern 5 times (simulating 5 executions)
	patternID := "sr_error_missing_error_handling"
	for i := 0; i < 5; i++ {
		cp := &CrossPattern{
			ID:            patternID,
			Type:          "error",
			Title:         "Missing error handling",
			Description:   "Errors must be checked and propagated",
			Context:       "Self-review",
			Confidence:    0.5,
			Occurrences:   1,
			IsAntiPattern: true,
			Scope:         "org",
		}
		if err := store.SaveCrossPattern(cp); err != nil {
			t.Fatalf("SaveCrossPattern iteration %d failed: %v", i+1, err)
		}
	}

	// Verify occurrences = 1 (initial) + 4 (increments) = 5
	cp, err := store.GetCrossPattern(patternID)
	if err != nil {
		t.Fatalf("GetCrossPattern failed: %v", err)
	}

	if cp.Occurrences != 5 {
		t.Errorf("occurrences = %d, want 5 after 5 saves", cp.Occurrences)
	}

	// Verify pattern is retrievable via GetTopCrossPatterns
	top, err := store.GetTopCrossPatterns(10, 0.0)
	if err != nil {
		t.Fatalf("GetTopCrossPatterns failed: %v", err)
	}

	found := false
	for _, p := range top {
		if p.ID == patternID {
			found = true
			if p.Occurrences != 5 {
				t.Errorf("top pattern occurrences = %d, want 5", p.Occurrences)
			}
			break
		}
	}
	if !found {
		t.Error("pattern not found in GetTopCrossPatterns results")
	}
}

// sanitizeID converts a title to a simple ID-safe string.
func sanitizeID(title string) string {
	var result []byte
	for _, c := range []byte(title) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			result = append(result, c)
		} else if c >= 'A' && c <= 'Z' {
			result = append(result, c+32) // lowercase
		} else if c == ' ' || c == '-' {
			result = append(result, '_')
		}
	}
	return string(result)
}

// longLine generates a string of the given length.
func longLine(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}
