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
		name        string
		title       string
		description string
		expected    SignalMetrics
	}{
		{
			name:        "epic tag in title",
			title:       "[epic] Big feature",
			description: "Some description",
			expected: SignalMetrics{
				HasEpicTag:           true,
				HasEpicKeyword:       true, // "big" and "feature" in combined text matches word boundary
				CheckboxCount:        0,
				PhaseCount:           0,
				WordCount:            2,
				HasStructuralMarkers: false,
			},
		},
		{
			name:        "epic keyword in description",
			title:       "Feature",
			description: "This is an epic task",
			expected: SignalMetrics{
				HasEpicTag:           false,
				HasEpicKeyword:       true,
				CheckboxCount:        0,
				PhaseCount:           0,
				WordCount:            5,
				HasStructuralMarkers: false,
			},
		},
		{
			name:  "multiple checkboxes",
			title: "Task",
			description: `Tasks:
- [ ] Item 1
- [ ] Item 2
- [x] Item 3
- [ ] Item 4
- [ ] Item 5`,
			expected: SignalMetrics{
				HasEpicTag:           false,
				HasEpicKeyword:       false,
				CheckboxCount:        5,
				PhaseCount:           0,
				WordCount:            25, // Includes checkbox syntax tokens
				HasStructuralMarkers: false,
			},
		},
		{
			name:  "multiple phases",
			title: "Implementation",
			description: `Plan:
Phase 1: Setup
Phase 2: Build
Phase 3: Test`,
			expected: SignalMetrics{
				HasEpicTag:           false,
				HasEpicKeyword:       false,
				CheckboxCount:        0,
				PhaseCount:           3,
				WordCount:            10, // "Plan:" + "Phase 1: Setup" etc
				HasStructuralMarkers: true,
			},
		},
		{
			name:        "structural markers with step",
			title:       "Feature",
			description: "## Overview\nThis is the first step of the implementation",
			expected: SignalMetrics{
				HasEpicTag:           false,
				HasEpicKeyword:       false,
				CheckboxCount:        0,
				PhaseCount:           0,
				WordCount:            10, // "##" counts as word + rest
				HasStructuralMarkers: true,
			},
		},
		{
			name:        "file path should not trigger epic keyword",
			title:       "Update epic.go",
			description: "Add method to internal/executor/epic.go",
			expected: SignalMetrics{
				HasEpicTag:           false,
				HasEpicKeyword:       false,
				CheckboxCount:        0,
				PhaseCount:           0,
				WordCount:            4, // file path stripped
				HasStructuralMarkers: false,
			},
		},
		{
			name:        "code block should not trigger epic keyword",
			title:       "Add code",
			description: "```go\ntype EpicPlan struct{}\n```",
			expected: SignalMetrics{
				HasEpicTag:           false,
				HasEpicKeyword:       false,
				CheckboxCount:        0,
				PhaseCount:           0,
				WordCount:            0, // code block stripped
				HasStructuralMarkers: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CollectSignalMetrics(tt.title, tt.description)
			if got.HasEpicTag != tt.expected.HasEpicTag {
				t.Errorf("HasEpicTag = %v, want %v", got.HasEpicTag, tt.expected.HasEpicTag)
			}
			if got.HasEpicKeyword != tt.expected.HasEpicKeyword {
				t.Errorf("HasEpicKeyword = %v, want %v", got.HasEpicKeyword, tt.expected.HasEpicKeyword)
			}
			if got.CheckboxCount != tt.expected.CheckboxCount {
				t.Errorf("CheckboxCount = %v, want %v", got.CheckboxCount, tt.expected.CheckboxCount)
			}
			if got.PhaseCount != tt.expected.PhaseCount {
				t.Errorf("PhaseCount = %v, want %v", got.PhaseCount, tt.expected.PhaseCount)
			}
			if got.WordCount != tt.expected.WordCount {
				t.Errorf("WordCount = %v, want %v", got.WordCount, tt.expected.WordCount)
			}
			if got.HasStructuralMarkers != tt.expected.HasStructuralMarkers {
				t.Errorf("HasStructuralMarkers = %v, want %v", got.HasStructuralMarkers, tt.expected.HasStructuralMarkers)
			}
		})
	}
}

func TestSignalMetrics_IsEpic(t *testing.T) {
	tests := []struct {
		name     string
		metrics  SignalMetrics
		expected bool
	}{
		{
			name:     "epic tag triggers",
			metrics:  SignalMetrics{HasEpicTag: true},
			expected: true,
		},
		{
			name:     "epic keyword triggers",
			metrics:  SignalMetrics{HasEpicKeyword: true},
			expected: true,
		},
		{
			name:     "5 checkboxes triggers",
			metrics:  SignalMetrics{CheckboxCount: 5},
			expected: true,
		},
		{
			name:     "4 checkboxes does not trigger",
			metrics:  SignalMetrics{CheckboxCount: 4},
			expected: false,
		},
		{
			name:     "3 phases triggers",
			metrics:  SignalMetrics{PhaseCount: 3},
			expected: true,
		},
		{
			name:     "2 phases does not trigger",
			metrics:  SignalMetrics{PhaseCount: 2},
			expected: false,
		},
		{
			name:     "200+ words with structural markers triggers",
			metrics:  SignalMetrics{WordCount: 201, HasStructuralMarkers: true},
			expected: true,
		},
		{
			name:     "200+ words without structural markers does not trigger",
			metrics:  SignalMetrics{WordCount: 201, HasStructuralMarkers: false},
			expected: false,
		},
		{
			name:     "structural markers without 200+ words does not trigger",
			metrics:  SignalMetrics{WordCount: 100, HasStructuralMarkers: true},
			expected: false,
		},
		{
			name:     "no signals does not trigger",
			metrics:  SignalMetrics{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.metrics.IsEpic(); got != tt.expected {
				t.Errorf("IsEpic() = %v, want %v", got, tt.expected)
			}
		})
	}
}
