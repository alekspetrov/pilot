package executor

import (
	"regexp"
	"strings"
)

// Complexity represents the detected complexity level of a task.
// Used for routing decisions: which model to use, whether to skip Navigator, etc.
type Complexity string

const (
	// ComplexityTrivial is for minimal changes: typos, logs, renames, comment updates.
	// These skip Navigator overhead and use the fastest model.
	ComplexityTrivial Complexity = "trivial"

	// ComplexitySimple is for small, focused changes: add field, small fix, single function.
	// May skip Navigator for known patterns.
	ComplexitySimple Complexity = "simple"

	// ComplexityMedium is for standard feature work: new endpoint, component, integration.
	// Uses full Navigator workflow with default model.
	ComplexityMedium Complexity = "medium"

	// ComplexityComplex is for architectural changes: refactors, migrations, system redesigns.
	// Uses full Navigator workflow with the most capable model.
	ComplexityComplex Complexity = "complex"

	// ComplexityEpic is for tasks too large for a single execution cycle.
	// These should be broken into multiple sub-tasks or phases.
	ComplexityEpic Complexity = "epic"
)

// trivialPatterns are exact or partial matches indicating trivial tasks.
// Order matters: more specific patterns first.
var trivialPatterns = []string{
	"fix typo",
	"typo",
	"add log",
	"add logging",
	"update comment",
	"fix comment",
	"rename variable",
	"rename function",
	"rename",
	"remove unused",
	"delete unused",
	"bump version",
	"update version",
	"fix import",
	"add import",
	"fix whitespace",
	"formatting",
	"lint fix",
}

// simplePatterns indicate tasks that are small but require some thought.
var simplePatterns = []string{
	"add field",
	"add property",
	"add parameter",
	"add argument",
	"small fix",
	"minor fix",
	"quick fix",
	"update config",
	"change config",
	"update constant",
	"add constant",
	"add test case",
	"fix test",
}

// complexPatterns indicate tasks requiring significant architectural consideration.
var complexPatterns = []string{
	"refactor",
	"rewrite",
	"redesign",
	"migrate",
	"migration",
	"architecture",
	"restructure",
	"overhaul",
	"system",
	"database schema",
	"api design",
	"multi-file",
	"cross-cutting",
}

// epicTagRegex matches [epic] tag in title.
var epicTagRegex = regexp.MustCompile(`(?i)\[epic\]`)

// codeBlockRegex matches fenced code blocks (```...``` or ~~~...~~~).
var codeBlockRegex = regexp.MustCompile("(?s)```.*?```|~~~.*?~~~")

// filePathRegex matches file paths like path/to/file.go or ./file.py.
var filePathRegex = regexp.MustCompile(`(?:^|[\s\x60])([./]*[\w-]+/)*[\w-]+\.(go|py|js|ts|tsx|jsx|rs|rb|java|c|cpp|h|hpp|yaml|yml|json|md|txt|sh|bash)(?:[\s\x60]|$)`)

// wordBoundaryEpicRegex matches "epic" as a standalone word, not as part of identifiers.
var wordBoundaryEpicRegex = regexp.MustCompile(`(?i)\b(epic|roadmap|multi-phase|milestone)\b`)

// numberedPhaseRegex matches explicit phase markers like "Phase 1", "Stage 1", etc.
// Does NOT match simple numbered lists (1., 2.) as those are common in task descriptions.
var numberedPhaseRegex = regexp.MustCompile(`(?mi)^(?:##\s*)?(?:phase|stage|part|milestone)\s+\d+`)

// checkboxRegex matches markdown checkboxes "- [ ]" or "- [x]".
var checkboxRegex = regexp.MustCompile(`(?m)^[\s]*-\s*\[[x ]\]`)

// DetectComplexity analyzes a task and returns its estimated complexity.
// The detection uses pattern matching on the description and heuristics
// like word count to estimate task complexity.
func DetectComplexity(task *Task) Complexity {
	if task == nil {
		return ComplexityMedium
	}

	desc := strings.ToLower(task.Description)
	title := strings.ToLower(task.Title)
	combined := desc + " " + title

	// Check epic patterns first (these are too large for single execution)
	if detectEpic(task.Title, task.Description, combined) {
		return ComplexityEpic
	}

	// Check trivial patterns (fastest path for small changes)
	for _, pattern := range trivialPatterns {
		if strings.Contains(combined, pattern) {
			return ComplexityTrivial
		}
	}

	// Check complex patterns next (prevents false simple classification)
	for _, pattern := range complexPatterns {
		if strings.Contains(combined, pattern) {
			return ComplexityComplex
		}
	}

	// Check simple patterns
	for _, pattern := range simplePatterns {
		if strings.Contains(combined, pattern) {
			return ComplexitySimple
		}
	}

	// Heuristics based on description length (strip code blocks to get actual prose length)
	cleanDesc := stripCodeBlocks(desc)
	wordCount := len(strings.Fields(cleanDesc))

	// Very short descriptions are likely simple tasks
	if wordCount < 10 {
		return ComplexitySimple
	}

	// Medium-length descriptions are standard feature work
	if wordCount < 50 {
		return ComplexityMedium
	}

	// Long descriptions suggest complex requirements
	return ComplexityComplex
}

// stripCodeBlocks removes fenced code blocks from text to avoid false pattern matches.
func stripCodeBlocks(text string) string {
	return codeBlockRegex.ReplaceAllString(text, " ")
}

// stripFilePaths removes file path references from text to avoid false pattern matches.
func stripFilePaths(text string) string {
	return filePathRegex.ReplaceAllString(text, " ")
}

// detectEpic checks if a task matches epic patterns.
// Returns true if any epic indicator is found.
// Uses CollectSignalMetrics internally for consistent threshold evaluation.
func detectEpic(title, description, _ string) bool {
	metrics := CollectSignalMetrics(title, description)
	return metrics.IsEpic()
}

// IsTrivial returns true if the task complexity is trivial.
func (c Complexity) IsTrivial() bool {
	return c == ComplexityTrivial
}

// IsSimple returns true if the task complexity is simple or trivial.
func (c Complexity) IsSimple() bool {
	return c == ComplexityTrivial || c == ComplexitySimple
}

// ShouldSkipNavigator returns true if Navigator overhead should be skipped.
// Only trivial tasks skip Navigator to avoid workflow overhead.
func (c Complexity) ShouldSkipNavigator() bool {
	return c == ComplexityTrivial
}

// String returns the string representation of the complexity level.
func (c Complexity) String() string {
	return string(c)
}

// ShouldRunResearch returns true if parallel research phase should run.
// Medium and complex tasks benefit from pre-execution research.
func (c Complexity) ShouldRunResearch() bool {
	return c == ComplexityMedium || c == ComplexityComplex
}

// IsEpic returns true if the task complexity is epic.
func (c Complexity) IsEpic() bool {
	return c == ComplexityEpic
}

// SignalMetrics holds extracted metrics from task description for epic detection.
// These metrics can be used for debugging, logging, or threshold tuning.
type SignalMetrics struct {
	// HasEpicTag indicates [epic] tag presence in title
	HasEpicTag bool
	// HasEpicKeyword indicates epic/roadmap/multi-phase/milestone keyword presence
	HasEpicKeyword bool
	// CheckboxCount is the number of markdown checkboxes found
	CheckboxCount int
	// PhaseCount is the number of numbered phase/stage/part sections
	PhaseCount int
	// WordCount is the word count of description (excluding code blocks)
	WordCount int
	// HasStructuralMarkers indicates presence of ##, phase, stage, step markers
	HasStructuralMarkers bool
}

// CollectSignalMetrics extracts all epic detection signals from task content.
// This allows callers to inspect individual metrics for debugging or custom logic.
func CollectSignalMetrics(title, description string) SignalMetrics {
	combined := strings.ToLower(description) + " " + strings.ToLower(title)

	// Check for [epic] tag in title
	hasEpicTag := epicTagRegex.MatchString(title)

	// Preprocess: strip code blocks and file paths to avoid false matches
	cleanCombined := stripCodeBlocks(combined)
	cleanCombined = stripFilePaths(cleanCombined)

	// Check for epic keywords using word boundary matching
	hasEpicKeyword := wordBoundaryEpicRegex.MatchString(cleanCombined)

	// Preprocess description for structural checks
	cleanDescription := stripCodeBlocks(description)

	// Count checkboxes
	checkboxMatches := checkboxRegex.FindAllString(cleanDescription, -1)
	checkboxCount := len(checkboxMatches)

	// Count numbered phases/sections
	phaseMatches := numberedPhaseRegex.FindAllString(cleanDescription, -1)
	phaseCount := len(phaseMatches)

	// Word count
	wordCount := len(strings.Fields(cleanDescription))

	// Structural markers
	hasStructuralMarkers := strings.Contains(cleanDescription, "##") ||
		strings.Contains(strings.ToLower(cleanDescription), "phase") ||
		strings.Contains(strings.ToLower(cleanDescription), "stage") ||
		strings.Contains(strings.ToLower(cleanDescription), "step")

	return SignalMetrics{
		HasEpicTag:           hasEpicTag,
		HasEpicKeyword:       hasEpicKeyword,
		CheckboxCount:        checkboxCount,
		PhaseCount:           phaseCount,
		WordCount:            wordCount,
		HasStructuralMarkers: hasStructuralMarkers,
	}
}

// IsEpic evaluates the metrics against epic thresholds.
// Returns true if any epic indicator threshold is met.
func (m SignalMetrics) IsEpic() bool {
	if m.HasEpicTag {
		return true
	}
	if m.HasEpicKeyword {
		return true
	}
	if m.CheckboxCount >= 5 {
		return true
	}
	if m.PhaseCount >= 3 {
		return true
	}
	if m.WordCount > 200 && m.HasStructuralMarkers {
		return true
	}
	return false
}
