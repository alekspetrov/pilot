package executor

import (
	"testing"
)

func TestDetectComplexity(t *testing.T) {
	tests := []struct {
		name     string
		task     *Task
		expected Complexity
	}{
		// Trivial cases
		{
			name:     "typo fix",
			task:     &Task{Description: "Fix typo in README.md"},
			expected: ComplexityTrivial,
		},
		{
			name:     "add logging",
			task:     &Task{Description: "Add log statement to debug connection"},
			expected: ComplexityTrivial,
		},
		{
			name:     "rename variable",
			task:     &Task{Description: "Rename variable from x to count"},
			expected: ComplexityTrivial,
		},
		{
			name:     "update comment",
			task:     &Task{Description: "Update comment to reflect new behavior"},
			expected: ComplexityTrivial,
		},
		{
			name:     "remove unused import",
			task:     &Task{Description: "Remove unused import statements"},
			expected: ComplexityTrivial,
		},

		// Simple cases
		{
			name:     "add field",
			task:     &Task{Description: "Add field to user struct"},
			expected: ComplexitySimple,
		},
		{
			name:     "add parameter",
			task:     &Task{Description: "Add parameter to function"},
			expected: ComplexitySimple,
		},
		{
			name:     "quick fix",
			task:     &Task{Description: "Quick fix for null check"},
			expected: ComplexitySimple,
		},
		{
			name:     "short description heuristic",
			task:     &Task{Description: "Update the button color"},
			expected: ComplexitySimple,
		},

		// Epic cases
		{
			name:     "epic tag in title",
			task:     &Task{Title: "[epic] Implement new authentication system", Description: "Multi-phase auth rewrite"},
			expected: ComplexityEpic,
		},
		{
			name:     "epic keyword in description",
			task:     &Task{Description: "This is an epic task that spans multiple sprints"},
			expected: ComplexityEpic,
		},
		{
			name:     "roadmap keyword",
			task:     &Task{Description: "Implement the roadmap for Q2 features"},
			expected: ComplexityEpic,
		},
		{
			name:     "multi-phase keyword",
			task:     &Task{Description: "This is a multi-phase implementation"},
			expected: ComplexityEpic,
		},
		{
			name:     "milestone keyword",
			task:     &Task{Description: "Complete milestone 3 with all features"},
			expected: ComplexityEpic,
		},
		{
			name: "5+ checkboxes",
			task: &Task{Description: `Implement user management:
- [ ] Create user model
- [ ] Add user API endpoints
- [ ] Implement user validation
- [ ] Add user permissions
- [ ] Create user tests
- [ ] Add user documentation`},
			expected: ComplexityEpic,
		},
		{
			name: "3+ numbered phases",
			task: &Task{Description: `Implementation plan:
Phase 1: Design the database schema
Phase 2: Implement the API layer
Phase 3: Create the frontend components
Phase 4: Add integration tests`},
			expected: ComplexityEpic,
		},
		{
			name: "phase keyword sections",
			task: &Task{Description: `Implementation:
Phase 1: Setup
Phase 2: Core logic
Phase 3: Testing`},
			expected: ComplexityEpic,
		},
		{
			name: "200+ words with structural markers",
			task: &Task{Description: `## Overview
This is a comprehensive implementation that requires significant planning and coordination across multiple teams.
The feature spans multiple components and requires careful consideration of the existing architecture patterns.
We need to ensure backward compatibility while introducing the new functionality in a phased approach.
The project involves frontend, backend, database, and infrastructure changes that must be carefully orchestrated.
Each phase builds on the previous one and has its own set of deliverables and acceptance criteria to meet.

## Phase 1: Foundation
Set up the basic infrastructure and data models needed for the feature implementation.
This includes database migrations for new tables and columns, API scaffolding with proper versioning,
and initial frontend components with placeholder data. We also need to configure the CI/CD pipeline
to support the new deployment requirements and set up monitoring dashboards for the new services.

## Phase 2: Core Implementation
Build out the main business logic and user-facing features with full functionality.
This is the bulk of the work and requires coordination across teams including frontend, backend, and QA.
We need to implement the core algorithms, integrate with external services, handle edge cases,
and ensure the system performs well under expected load. Documentation should be written in parallel.

## Phase 3: Polish and Testing
Add comprehensive tests including unit tests, integration tests, and end-to-end tests.
Fix edge cases discovered during testing and polish the user experience based on feedback.
This phase ensures production readiness and documentation completeness before the final release.
We need to conduct load testing, security review, and accessibility audit before going live.

The implementation should follow our established patterns and coding standards for consistency.
We need to coordinate with the design team for the UI components and the platform team for infrastructure.
Performance testing will be critical given the expected load on this feature during peak hours.
We should also plan for graceful degradation and proper error handling throughout the system.
Regular sync meetings with stakeholders will be necessary to ensure alignment on priorities and timeline.`},
			expected: ComplexityEpic,
		},

		// False positive prevention - file paths and code blocks
		{
			name:     "file path with epic should not trigger",
			task:     &Task{Title: "Add method to epic.go", Description: "Add CreateSubIssues method to internal/executor/epic.go"},
			expected: ComplexitySimple,
		},
		{
			name:     "code block with EpicPlan should not trigger",
			task:     &Task{Description: "Add this code:\n```go\nfunc (r *Runner) Method(plan *EpicPlan) error {\n    return nil\n}\n```"},
			expected: ComplexitySimple,
		},
		{
			name:     "identifier PlanEpic should not trigger",
			task:     &Task{Description: "Call the `PlanEpic` method after detection"},
			expected: ComplexitySimple,
		},
		{
			name:     "actual epic keyword should still trigger",
			task:     &Task{Description: "This is an epic task spanning multiple sprints"},
			expected: ComplexityEpic,
		},
		{
			name:     "epic in prose triggers but not in file path",
			task:     &Task{Description: "This epic feature requires changes to epic.go"},
			expected: ComplexityEpic,
		},

		// Complex cases
		{
			name:     "refactor",
			task:     &Task{Description: "Refactor the authentication system"},
			expected: ComplexityComplex,
		},
		{
			name:     "migration",
			task:     &Task{Description: "Database migration for new schema"},
			expected: ComplexityComplex,
		},
		{
			name:     "architecture",
			task:     &Task{Description: "Update system architecture for microservices"},
			expected: ComplexityComplex,
		},
		{
			name:     "rewrite",
			task:     &Task{Description: "Rewrite the parser from scratch"},
			expected: ComplexityComplex,
		},

		// Medium cases (default)
		{
			name:     "medium length description",
			task:     &Task{Description: "Implement a new endpoint that fetches user data from the database and returns it formatted as JSON with proper error handling"},
			expected: ComplexityMedium,
		},
		{
			name:     "feature without keywords",
			task:     &Task{Description: "Create new component for displaying charts with proper styling and responsive design"},
			expected: ComplexityMedium,
		},

		// Edge cases
		{
			name:     "nil task",
			task:     nil,
			expected: ComplexityMedium,
		},
		{
			name:     "empty description",
			task:     &Task{Description: ""},
			expected: ComplexitySimple,
		},
		{
			name:     "title contains pattern",
			task:     &Task{Title: "Fix typo", Description: "Some description"},
			expected: ComplexityTrivial,
		},

		// Long description triggers complex
		{
			name: "very long description",
			task: &Task{Description: `This task requires implementing a comprehensive solution that spans multiple files and components.
				We need to update the data layer, add new API endpoints, modify the frontend components, update tests,
				and ensure backward compatibility. The implementation should follow our coding standards and include
				proper documentation. We also need to consider performance implications and add appropriate caching
				where necessary. The feature should support both authenticated and anonymous users with different
				permission levels.`},
			expected: ComplexityComplex,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectComplexity(tt.task)
			if got != tt.expected {
				t.Errorf("DetectComplexity() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestComplexity_Methods(t *testing.T) {
	tests := []struct {
		complexity          Complexity
		isTrivial           bool
		isSimple            bool
		isEpic              bool
		shouldSkipNavigator bool
		shouldRunResearch   bool
	}{
		{ComplexityTrivial, true, true, false, true, false},
		{ComplexitySimple, false, true, false, false, false},
		{ComplexityMedium, false, false, false, false, true},
		{ComplexityComplex, false, false, false, false, true},
		{ComplexityEpic, false, false, true, false, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.complexity), func(t *testing.T) {
			if got := tt.complexity.IsTrivial(); got != tt.isTrivial {
				t.Errorf("IsTrivial() = %v, want %v", got, tt.isTrivial)
			}
			if got := tt.complexity.IsSimple(); got != tt.isSimple {
				t.Errorf("IsSimple() = %v, want %v", got, tt.isSimple)
			}
			if got := tt.complexity.IsEpic(); got != tt.isEpic {
				t.Errorf("IsEpic() = %v, want %v", got, tt.isEpic)
			}
			if got := tt.complexity.ShouldSkipNavigator(); got != tt.shouldSkipNavigator {
				t.Errorf("ShouldSkipNavigator() = %v, want %v", got, tt.shouldSkipNavigator)
			}
			if got := tt.complexity.ShouldRunResearch(); got != tt.shouldRunResearch {
				t.Errorf("ShouldRunResearch() = %v, want %v", got, tt.shouldRunResearch)
			}
		})
	}
}

func TestComplexity_String(t *testing.T) {
	tests := []struct {
		complexity Complexity
		expected   string
	}{
		{ComplexityTrivial, "trivial"},
		{ComplexitySimple, "simple"},
		{ComplexityMedium, "medium"},
		{ComplexityComplex, "complex"},
		{ComplexityEpic, "epic"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.complexity.String(); got != tt.expected {
				t.Errorf("String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCollectSignalMetrics(t *testing.T) {
	tests := []struct {
		name     string
		task     *Task
		expected SignalMetrics
	}{
		{
			name: "nil task returns empty metrics",
			task: nil,
			expected: SignalMetrics{
				CheckboxCount:        0,
				PhaseCount:           0,
				WordCount:            0,
				HasEpicTag:           false,
				HasEpicKeywords:      false,
				HasStructuralMarkers: false,
			},
		},
		{
			name: "empty task returns empty metrics",
			task: &Task{},
			expected: SignalMetrics{
				CheckboxCount:        0,
				PhaseCount:           0,
				WordCount:            0,
				HasEpicTag:           false,
				HasEpicKeywords:      false,
				HasStructuralMarkers: false,
			},
		},
		{
			name: "epic tag in title",
			task: &Task{Title: "[epic] Implement new auth", Description: "Some description"},
			expected: SignalMetrics{
				CheckboxCount:        0,
				PhaseCount:           0,
				WordCount:            2,
				HasEpicTag:           true,
				HasEpicKeywords:      true, // "epic" keyword also found in combined text
				HasStructuralMarkers: false,
			},
		},
		{
			name: "epic keyword in description",
			task: &Task{Description: "This is an epic task spanning sprints"},
			expected: SignalMetrics{
				CheckboxCount:        0,
				PhaseCount:           0,
				WordCount:            7,
				HasEpicTag:           false,
				HasEpicKeywords:      true,
				HasStructuralMarkers: false,
			},
		},
		{
			name: "checkboxes counted",
			task: &Task{Description: `Tasks:
- [ ] First task
- [ ] Second task
- [x] Third task done
- [ ] Fourth task`},
			expected: SignalMetrics{
				CheckboxCount:        4,
				PhaseCount:           0,
				WordCount:            21, // includes all words in description
				HasEpicTag:           false,
				HasEpicKeywords:      false,
				HasStructuralMarkers: false,
			},
		},
		{
			name: "phases counted",
			task: &Task{Description: `Plan:
Phase 1: Setup
Phase 2: Implementation
Phase 3: Testing`},
			expected: SignalMetrics{
				CheckboxCount:        0,
				PhaseCount:           3,
				WordCount:            10, // "Plan:", "Phase", "1:", "Setup", etc.
				HasEpicTag:           false,
				HasEpicKeywords:      false,
				HasStructuralMarkers: true,
			},
		},
		{
			name: "structural markers detected",
			task: &Task{Description: "## Overview\nThis is a phased approach with multiple steps"},
			expected: SignalMetrics{
				CheckboxCount:        0,
				PhaseCount:           0,
				WordCount:            10, // "##" counts as a word too
				HasEpicTag:           false,
				HasEpicKeywords:      false,
				HasStructuralMarkers: true,
			},
		},
		{
			name: "trivial pattern matched",
			task: &Task{Description: "Fix typo in the documentation"},
			expected: SignalMetrics{
				CheckboxCount:         0,
				PhaseCount:            0,
				WordCount:             5,
				HasEpicTag:            false,
				HasEpicKeywords:       false,
				HasStructuralMarkers:  false,
				MatchedTrivialPattern: "fix typo",
			},
		},
		{
			name: "simple pattern matched",
			task: &Task{Description: "Add field to user struct"},
			expected: SignalMetrics{
				CheckboxCount:        0,
				PhaseCount:           0,
				WordCount:            5,
				HasEpicTag:           false,
				HasEpicKeywords:      false,
				HasStructuralMarkers: false,
				MatchedSimplePattern: "add field",
			},
		},
		{
			name: "complex pattern matched",
			task: &Task{Description: "Refactor the authentication system"},
			expected: SignalMetrics{
				CheckboxCount:         0,
				PhaseCount:            0,
				WordCount:             4,
				HasEpicTag:            false,
				HasEpicKeywords:       false,
				HasStructuralMarkers:  false,
				MatchedComplexPattern: "refactor",
			},
		},
		{
			name: "code blocks excluded from word count",
			task: &Task{Description: "Add this:\n```go\nfunc main() {\n    fmt.Println(\"hello\")\n}\n```\nDone"},
			expected: SignalMetrics{
				CheckboxCount:        0,
				PhaseCount:           0,
				WordCount:            3, // "Add this:" and "Done" only
				HasEpicTag:           false,
				HasEpicKeywords:      false,
				HasStructuralMarkers: false,
			},
		},
		{
			name: "multiple patterns can match",
			task: &Task{Description: "Fix typo and add field during refactor"},
			expected: SignalMetrics{
				CheckboxCount:         0,
				PhaseCount:            0,
				WordCount:             7,
				HasEpicTag:            false,
				HasEpicKeywords:       false,
				HasStructuralMarkers:  false,
				MatchedTrivialPattern: "fix typo", // matches "fix typo" pattern
				MatchedSimplePattern:  "add field",
				MatchedComplexPattern: "refactor",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CollectSignalMetrics(tt.task)

			if got.CheckboxCount != tt.expected.CheckboxCount {
				t.Errorf("CheckboxCount = %v, want %v", got.CheckboxCount, tt.expected.CheckboxCount)
			}
			if got.PhaseCount != tt.expected.PhaseCount {
				t.Errorf("PhaseCount = %v, want %v", got.PhaseCount, tt.expected.PhaseCount)
			}
			if got.WordCount != tt.expected.WordCount {
				t.Errorf("WordCount = %v, want %v", got.WordCount, tt.expected.WordCount)
			}
			if got.HasEpicTag != tt.expected.HasEpicTag {
				t.Errorf("HasEpicTag = %v, want %v", got.HasEpicTag, tt.expected.HasEpicTag)
			}
			if got.HasEpicKeywords != tt.expected.HasEpicKeywords {
				t.Errorf("HasEpicKeywords = %v, want %v", got.HasEpicKeywords, tt.expected.HasEpicKeywords)
			}
			if got.HasStructuralMarkers != tt.expected.HasStructuralMarkers {
				t.Errorf("HasStructuralMarkers = %v, want %v", got.HasStructuralMarkers, tt.expected.HasStructuralMarkers)
			}
			if got.MatchedTrivialPattern != tt.expected.MatchedTrivialPattern {
				t.Errorf("MatchedTrivialPattern = %v, want %v", got.MatchedTrivialPattern, tt.expected.MatchedTrivialPattern)
			}
			if got.MatchedSimplePattern != tt.expected.MatchedSimplePattern {
				t.Errorf("MatchedSimplePattern = %v, want %v", got.MatchedSimplePattern, tt.expected.MatchedSimplePattern)
			}
			if got.MatchedComplexPattern != tt.expected.MatchedComplexPattern {
				t.Errorf("MatchedComplexPattern = %v, want %v", got.MatchedComplexPattern, tt.expected.MatchedComplexPattern)
			}
		})
	}
}

func TestSignalMetrics_IsEpicSignal(t *testing.T) {
	tests := []struct {
		name     string
		metrics  SignalMetrics
		expected bool
	}{
		{
			name:     "empty metrics is not epic",
			metrics:  SignalMetrics{},
			expected: false,
		},
		{
			name:     "epic tag is epic",
			metrics:  SignalMetrics{HasEpicTag: true},
			expected: true,
		},
		{
			name:     "epic keywords is epic",
			metrics:  SignalMetrics{HasEpicKeywords: true},
			expected: true,
		},
		{
			name:     "5+ checkboxes is epic",
			metrics:  SignalMetrics{CheckboxCount: 5},
			expected: true,
		},
		{
			name:     "4 checkboxes is not epic",
			metrics:  SignalMetrics{CheckboxCount: 4},
			expected: false,
		},
		{
			name:     "3+ phases is epic",
			metrics:  SignalMetrics{PhaseCount: 3},
			expected: true,
		},
		{
			name:     "2 phases is not epic",
			metrics:  SignalMetrics{PhaseCount: 2},
			expected: false,
		},
		{
			name:     "200+ words with structural markers is epic",
			metrics:  SignalMetrics{WordCount: 201, HasStructuralMarkers: true},
			expected: true,
		},
		{
			name:     "200+ words without structural markers is not epic",
			metrics:  SignalMetrics{WordCount: 201, HasStructuralMarkers: false},
			expected: false,
		},
		{
			name:     "structural markers with fewer words is not epic",
			metrics:  SignalMetrics{WordCount: 50, HasStructuralMarkers: true},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.metrics.IsEpicSignal(); got != tt.expected {
				t.Errorf("IsEpicSignal() = %v, want %v", got, tt.expected)
			}
		})
	}
}
