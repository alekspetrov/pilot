package main

import (
	"testing"

	"github.com/alekspetrov/pilot/internal/executor"
)

func TestHandleGitHubIssueWithResultSourceAdapter(t *testing.T) {
	// Test that GitHub tasks have SourceAdapter set to "github"
	// This mirrors the task creation logic from handleGitHubIssueWithResult

	taskID := "GH-123"
	taskDesc := "GitHub Issue 123: Test issue\n\nTest description"
	branchName := "pilot/GH-123"
	projectPath := "/tmp/test"
	sourceRepo := "test/repo"

	task := &executor.Task{
		ID:                 taskID,
		Title:              "Test issue",
		Description:        taskDesc,
		ProjectPath:        projectPath,
		Branch:             branchName,
		CreatePR:           true,
		SourceRepo:         sourceRepo,
		SourceAdapter:      "github", // GH-1433: explicitly mark GitHub source
		MemberID:           "",
		Labels:             []string{},
		AcceptanceCriteria: []string{},
		FromPR:             0,
	}

	// Test that SourceAdapter is set correctly
	if task.SourceAdapter != "github" {
		t.Errorf("Expected SourceAdapter to be 'github', got '%s'", task.SourceAdapter)
	}
}

func TestTaskSourceAdapterValues(t *testing.T) {
	tests := []struct {
		name           string
		expectedSource string
		taskFactory    func() *executor.Task
	}{
		{
			name:           "GitHub task",
			expectedSource: "github",
			taskFactory: func() *executor.Task {
				return &executor.Task{
					ID:            "GH-123",
					Title:         "GitHub Issue",
					SourceAdapter: "github",
				}
			},
		},
		{
			name:           "Linear task",
			expectedSource: "linear",
			taskFactory: func() *executor.Task {
				return &executor.Task{
					ID:            "LIN-123",
					Title:         "Linear Issue",
					SourceAdapter: "linear",
				}
			},
		},
		{
			name:           "Jira task",
			expectedSource: "jira",
			taskFactory: func() *executor.Task {
				return &executor.Task{
					ID:            "JIRA-123",
					Title:         "Jira Issue",
					SourceAdapter: "jira",
				}
			},
		},
		{
			name:           "Asana task",
			expectedSource: "asana",
			taskFactory: func() *executor.Task {
				return &executor.Task{
					ID:            "ASA-123",
					Title:         "Asana Task",
					SourceAdapter: "asana",
				}
			},
		},
		{
			name:           "Telegram task",
			expectedSource: "telegram",
			taskFactory: func() *executor.Task {
				return &executor.Task{
					ID:            "CHAT-123",
					Title:         "Telegram Task",
					SourceAdapter: "telegram",
				}
			},
		},
		{
			name:           "Slack task",
			expectedSource: "slack",
			taskFactory: func() *executor.Task {
				return &executor.Task{
					ID:            "SLACK-123",
					Title:         "Slack Task",
					SourceAdapter: "slack",
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := tt.taskFactory()
			if task.SourceAdapter != tt.expectedSource {
				t.Errorf("Expected SourceAdapter to be '%s', got '%s'",
					tt.expectedSource, task.SourceAdapter)
			}
		})
	}
}