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

func TestCountCheckboxes(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		// Empty and basic cases
		{
			name:     "empty string",
			text:     "",
			expected: 0,
		},
		{
			name:     "no checkboxes",
			text:     "Just some plain text without any checkboxes",
			expected: 0,
		},
		{
			name:     "single unchecked checkbox",
			text:     "- [ ] Task item",
			expected: 1,
		},
		{
			name:     "single checked checkbox",
			text:     "- [x] Completed task",
			expected: 1,
		},

		// Multiple checkboxes
		{
			name: "multiple unchecked checkboxes",
			text: `- [ ] First task
- [ ] Second task
- [ ] Third task`,
			expected: 3,
		},
		{
			name: "mixed checked and unchecked",
			text: `- [x] Done
- [ ] Not done
- [x] Also done
- [ ] Still pending`,
			expected: 4,
		},

		// Checkbox format variations
		{
			name:     "checkbox with leading spaces",
			text:     "  - [ ] Indented task",
			expected: 1,
		},
		{
			name:     "checkbox with tabs",
			text:     "\t- [ ] Tab-indented task",
			expected: 1,
		},
		{
			name: "nested checkboxes with varying indentation",
			text: `- [ ] Top level
  - [ ] Nested once
    - [ ] Nested twice`,
			expected: 3,
		},
		{
			name:     "uppercase X in checkbox",
			text:     "- [X] Task with uppercase X",
			expected: 0, // Only lowercase x is matched
		},

		// Code blocks should be stripped
		{
			name: "checkboxes inside code block should not count",
			text: "```markdown\n- [ ] This is in a code block\n- [x] This too\n```",
			expected: 0,
		},
		{
			name: "checkboxes outside code block should count",
			text: `- [ ] Real task
` + "```\n- [ ] In code block\n```" + `
- [x] Another real task`,
			expected: 2,
		},
		{
			name: "tilde code block should be stripped",
			text: "~~~\n- [ ] Inside tilde block\n~~~\n- [ ] Outside",
			expected: 1,
		},

		// Mixed content
		{
			name: "checkboxes with surrounding prose",
			text: `## Implementation Plan

Here's what needs to be done:

- [ ] Design the API
- [ ] Implement the backend
- [ ] Write tests
- [x] Create documentation

Additional notes below.`,
			expected: 4,
		},
		{
			name:     "inline code with checkbox-like pattern",
			text:     "Use `- [ ]` to create a checkbox.\n- [ ] Real task",
			expected: 1, // Checkbox must be at start of line
		},

		// Edge cases
		{
			name:     "checkbox without task text",
			text:     "- [ ] ",
			expected: 1,
		},
		{
			name:     "checkbox no space after dash matches",
			text:     "-[ ] No space after dash",
			expected: 1, // Regex uses \s* which allows zero spaces
		},
		{
			name:     "checkbox with extra spaces matches",
			text:     "-  [ ] Extra space",
			expected: 1, // Regex uses \s* which allows multiple spaces
		},
		{
			name:     "asterisk bullet not matched",
			text:     "* [ ] Asterisk bullet",
			expected: 0,
		},
		{
			name:     "plus bullet not matched",
			text:     "+ [ ] Plus bullet",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CountCheckboxes(tt.text)
			if got != tt.expected {
				t.Errorf("CountCheckboxes() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCountPhases(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		// Empty and basic cases
		{
			name:     "empty string",
			text:     "",
			expected: 0,
		},
		{
			name:     "no phases",
			text:     "Just a simple task description",
			expected: 0,
		},
		{
			name:     "single phase",
			text:     "Phase 1: Setup",
			expected: 1,
		},

		// Multiple phases
		{
			name: "three phases",
			text: `Phase 1: Design
Phase 2: Implementation
Phase 3: Testing`,
			expected: 3,
		},
		{
			name: "phases with markdown headers",
			text: `## Phase 1: Foundation
Setup the basic structure

## Phase 2: Core Logic
Implement main features

## Phase 3: Polish
Final touches`,
			expected: 3,
		},

		// Different phase keywords
		{
			name: "stage keyword",
			text: `Stage 1: Planning
Stage 2: Development
Stage 3: Release`,
			expected: 3,
		},
		{
			name: "part keyword",
			text: `Part 1: Introduction
Part 2: Main content`,
			expected: 2,
		},
		{
			name: "milestone keyword",
			text: `Milestone 1: MVP
Milestone 2: Beta
Milestone 3: GA`,
			expected: 3,
		},
		{
			name: "mixed phase keywords",
			text: `Phase 1: Start
Stage 2: Middle
Part 3: End`,
			expected: 3,
		},

		// Code blocks should be stripped
		{
			name: "phases inside code block should not count",
			text: "```\nPhase 1: In code\nPhase 2: Also in code\n```",
			expected: 0,
		},
		{
			name: "phases outside code block should count",
			text: `Phase 1: Real phase
` + "```\nPhase 2: In code block\n```" + `
Phase 3: Another real phase`,
			expected: 2,
		},

		// Case insensitivity
		{
			name: "lowercase phase",
			text: "phase 1: lowercase",
			expected: 1,
		},
		{
			name: "mixed case phase",
			text: "PHASE 1: uppercase\nPhase 2: title case\nphase 3: lowercase",
			expected: 3,
		},

		// Edge cases
		{
			name:     "phase without colon",
			text:     "Phase 1 description here",
			expected: 1,
		},
		{
			name:     "phase with leading whitespace",
			text:     "  Phase 1: Indented",
			expected: 0, // Regex requires start of line
		},
		{
			name:     "phase in middle of line",
			text:     "This is Phase 1 of the plan",
			expected: 0, // Regex requires start of line
		},
		{
			name:     "numbered list is not a phase",
			text:     "1. First item\n2. Second item\n3. Third item",
			expected: 0,
		},
		{
			name:     "step keyword not matched",
			text:     "Step 1: First\nStep 2: Second",
			expected: 0, // step is not in the pattern
		},
		{
			name: "phase numbers with varying digits",
			text: `Phase 1: Single digit
Phase 10: Double digit
Phase 123: Triple digit`,
			expected: 3,
		},

		// Realistic scenarios
		{
			name: "full implementation plan",
			text: `# Implementation Plan

## Overview
This feature requires multiple phases.

## Phase 1: Database Schema
Create the necessary tables and migrations.

## Phase 2: API Layer
Build REST endpoints with validation.

## Phase 3: Frontend
React components and state management.

## Phase 4: Testing
Unit, integration, and e2e tests.`,
			expected: 4,
		},
		{
			name: "phases with code examples",
			text: `Phase 1: Setup
` + "```go\nfunc Phase1Setup() {}\n```" + `

Phase 2: Implementation
` + "```go\nfunc Phase2Impl() {}\n```",
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CountPhases(tt.text)
			if got != tt.expected {
				t.Errorf("CountPhases() = %v, want %v", got, tt.expected)
			}
		})
	}
}
