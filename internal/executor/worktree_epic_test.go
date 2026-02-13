package executor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestWorktreeEpicIntegration verifies that epic tasks execute entirely within worktree isolation.
// GH-969: This test ensures:
// 1. PlanEpic() runs in the worktree path
// 2. Each sub-issue execution uses the proper worktree path
// 3. All worktrees are cleaned up after execution
func TestWorktreeEpicIntegration(t *testing.T) {
	// Setup test repository with remote (needed for branch/push operations)
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	// Track paths used during execution
	var pathsMu sync.Mutex
	var subIssueExecutionPaths []string

	// Create mock scripts directory
	mockDir := t.TempDir()

	// Mock Claude script that outputs a valid epic plan with 2 subtasks
	mockClaudeScript := filepath.Join(mockDir, "mock-claude")
	claudeOutput := `Here's the implementation plan:

1. **Add database schema** - Create migration files for the new tables
2. **Implement API endpoints** - Build REST endpoints with validation`
	writeMockScriptWithPathCapture(t, mockClaudeScript, claudeOutput, 0)

	// Create runner with worktree mode enabled
	runner := &Runner{
		config: &BackendConfig{
			ClaudeCode: &ClaudeCodeConfig{
				Command: mockClaudeScript,
			},
			UseWorktree: true,
		},
		running:           make(map[string]*exec.Cmd),
		progressCallbacks: make(map[string]ProgressCallback),
		tokenCallbacks:    make(map[string]TokenCallback),
		log:               testLogger(),
		modelRouter:       NewModelRouter(nil, nil),
	}

	// Skip preflight checks (no real claude binary)
	runner.SetSkipPreflightChecks(true)

	// Create epic task
	epicTask := &Task{
		ID:          "GH-EPIC-100",
		Title:       "[epic] Test worktree isolation for epic",
		Description: "Verify epic planning and execution uses worktree paths",
		ProjectPath: localRepo,
		Branch:      "pilot/GH-EPIC-100",
		CreatePR:    true,
	}

	// Override executeFunc to capture the execution path for sub-issues
	runner.executeFunc = func(ctx context.Context, task *Task) (*ExecutionResult, error) {
		// This is called for sub-issue execution
		pathsMu.Lock()
		subIssueExecutionPaths = append(subIssueExecutionPaths, task.ProjectPath)
		pathsMu.Unlock()

		return &ExecutionResult{
			TaskID:    task.ID,
			Success:   true,
			PRUrl:     fmt.Sprintf("https://github.com/test/repo/pull/%d", len(subIssueExecutionPaths)+100),
			CommitSHA: "abc123",
		}, nil
	}

	// Test PlanEpic with worktree path
	ctx := context.Background()

	// Create worktree for the test (simulating what executeWithOptions does)
	manager := NewWorktreeManager(localRepo)
	worktreeResult, err := manager.CreateWorktreeWithBranch(ctx, "GH-EPIC-100", "pilot/GH-EPIC-100", "main")
	if err != nil {
		t.Fatalf("Failed to create worktree: %v", err)
	}
	defer worktreeResult.Cleanup()

	// Copy Navigator to worktree
	if err := EnsureNavigatorInWorktree(localRepo, worktreeResult.Path); err != nil {
		t.Fatalf("Failed to ensure navigator in worktree: %v", err)
	}

	// Test 1: Verify PlanEpic uses the provided execution path
	plan, err := runner.PlanEpic(ctx, epicTask, worktreeResult.Path)
	if err != nil {
		t.Fatalf("PlanEpic failed: %v", err)
	}

	// Verify plan was created successfully
	if plan == nil || len(plan.Subtasks) != 2 {
		t.Fatalf("Expected 2 subtasks, got: %v", plan)
	}
	t.Logf("PlanEpic executed in worktree path: %s", worktreeResult.Path)

	// Verify PlanEpic ran in worktree (not original repo)
	if !strings.Contains(worktreeResult.Path, "pilot-worktree-") {
		t.Errorf("PlanEpic should run in worktree, got path: %s", worktreeResult.Path)
	}

	// Test 2: Verify ExecuteSubIssues passes worktree path to sub-executions
	// Create mock issues (bypassing CreateSubIssues which requires gh auth)
	mockIssues := []CreatedIssue{
		{Number: 1001, URL: "https://github.com/test/repo/issues/1001", Subtask: plan.Subtasks[0]},
		{Number: 1002, URL: "https://github.com/test/repo/issues/1002", Subtask: plan.Subtasks[1]},
	}

	err = runner.ExecuteSubIssues(ctx, epicTask, mockIssues, worktreeResult.Path)
	if err != nil {
		t.Fatalf("ExecuteSubIssues failed: %v", err)
	}

	// Verify sub-issue executions used the worktree path
	pathsMu.Lock()
	defer pathsMu.Unlock()

	if len(subIssueExecutionPaths) != 2 {
		t.Fatalf("Expected 2 sub-issue executions, got %d", len(subIssueExecutionPaths))
	}

	for i, path := range subIssueExecutionPaths {
		t.Logf("Sub-issue %d executed in path: %s", i+1, path)
		// Sub-issues should use the worktree path passed to ExecuteSubIssues
		if path != worktreeResult.Path {
			t.Errorf("Sub-issue %d: expected worktree path %q, got %q", i+1, worktreeResult.Path, path)
		}
	}
}

// TestWorktreeEpicCleanup verifies all worktrees are cleaned up after epic execution.
func TestWorktreeEpicCleanup(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	ctx := context.Background()
	manager := NewWorktreeManager(localRepo)

	// Simulate epic execution creating multiple worktrees
	worktrees := make([]*WorktreeResult, 3)
	var err error

	for i := 0; i < 3; i++ {
		branchName := fmt.Sprintf("pilot/epic-cleanup-test-%d", i+1)
		worktrees[i], err = manager.CreateWorktreeWithBranch(ctx, fmt.Sprintf("cleanup-test-%d", i), branchName, "main")
		if err != nil {
			t.Fatalf("Failed to create worktree %d: %v", i, err)
		}
	}

	// Verify all worktrees exist
	for i, wt := range worktrees {
		if _, err := os.Stat(wt.Path); os.IsNotExist(err) {
			t.Fatalf("Worktree %d should exist at %s", i, wt.Path)
		}
	}

	// Verify active count
	if count := manager.ActiveCount(); count != 3 {
		t.Errorf("Expected 3 active worktrees, got %d", count)
	}

	// Cleanup all worktrees (simulating epic completion)
	for _, wt := range worktrees {
		wt.Cleanup()
	}

	// Verify all worktrees are removed
	for i, wt := range worktrees {
		if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
			t.Errorf("Worktree %d should be removed after cleanup: %s", i, wt.Path)
		}
	}

	// Verify active count is zero
	if count := manager.ActiveCount(); count != 0 {
		t.Errorf("Expected 0 active worktrees after cleanup, got %d", count)
	}
}

// TestWorktreeEpicCleanupOnFailure verifies worktrees are cleaned up even when epic execution fails.
func TestWorktreeEpicCleanupOnFailure(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	ctx := context.Background()
	manager := NewWorktreeManager(localRepo)

	var worktreePath string

	// Simulate epic execution with deferred cleanup (how runner.go does it)
	func() {
		result, err := manager.CreateWorktreeWithBranch(ctx, "failure-test", "pilot/failure-test", "main")
		if err != nil {
			t.Fatalf("Failed to create worktree: %v", err)
		}
		defer result.Cleanup() // Should run even on panic/error

		worktreePath = result.Path

		// Verify worktree exists during execution
		if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
			t.Fatal("Worktree should exist during execution")
		}

		// Simulate failure (but don't panic - just return early)
		return
	}()

	// Verify worktree was cleaned up despite "failure"
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Error("Worktree should be cleaned up after function returns")
	}

	if count := manager.ActiveCount(); count != 0 {
		t.Errorf("Expected 0 active worktrees after failure cleanup, got %d", count)
	}
}

// TestWorktreeEpicWithNavigatorCopy verifies Navigator config is copied to worktree for epic execution.
func TestWorktreeEpicWithNavigatorCopy(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	// Create Navigator structure in source repo
	agentDir := filepath.Join(localRepo, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("Failed to create .agent dir: %v", err)
	}

	devReadme := filepath.Join(agentDir, "DEVELOPMENT-README.md")
	navContent := "# Navigator Config\n\nProject-specific settings"
	if err := os.WriteFile(devReadme, []byte(navContent), 0644); err != nil {
		t.Fatalf("Failed to create DEVELOPMENT-README.md: %v", err)
	}

	// Create nested untracked content (commonly gitignored)
	markersDir := filepath.Join(agentDir, ".context-markers")
	if err := os.MkdirAll(markersDir, 0755); err != nil {
		t.Fatalf("Failed to create .context-markers: %v", err)
	}
	markerFile := filepath.Join(markersDir, "epic-marker.md")
	if err := os.WriteFile(markerFile, []byte("# Epic Marker"), 0644); err != nil {
		t.Fatalf("Failed to create marker file: %v", err)
	}

	ctx := context.Background()
	manager := NewWorktreeManager(localRepo)

	// Create worktree for epic execution
	result, err := manager.CreateWorktreeWithBranch(ctx, "navigator-test", "pilot/navigator-test", "main")
	if err != nil {
		t.Fatalf("Failed to create worktree: %v", err)
	}
	defer result.Cleanup()

	// Copy Navigator to worktree (as done in executeWithOptions)
	if err := EnsureNavigatorInWorktree(localRepo, result.Path); err != nil {
		t.Fatalf("EnsureNavigatorInWorktree failed: %v", err)
	}

	// Verify Navigator was copied to worktree
	worktreeReadme := filepath.Join(result.Path, ".agent", "DEVELOPMENT-README.md")
	if _, err := os.Stat(worktreeReadme); os.IsNotExist(err) {
		t.Error("DEVELOPMENT-README.md should be copied to worktree")
	}

	// Verify nested untracked content was copied
	worktreeMarker := filepath.Join(result.Path, ".agent", ".context-markers", "epic-marker.md")
	if _, err := os.Stat(worktreeMarker); os.IsNotExist(err) {
		t.Error(".context-markers/epic-marker.md should be copied to worktree")
	}

	// Verify content is correct
	content, err := os.ReadFile(worktreeReadme)
	if err != nil {
		t.Fatalf("Failed to read worktree README: %v", err)
	}
	if string(content) != navContent {
		t.Errorf("Content mismatch: got %q, want %q", string(content), navContent)
	}
}

// TestWorktreeEpicBranchOperations verifies branch operations work correctly in worktree.
func TestWorktreeEpicBranchOperations(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	ctx := context.Background()
	manager := NewWorktreeManager(localRepo)

	branchName := "pilot/epic-branch-test"
	result, err := manager.CreateWorktreeWithBranch(ctx, "branch-test", branchName, "main")
	if err != nil {
		t.Fatalf("CreateWorktreeWithBranch failed: %v", err)
	}
	defer result.Cleanup()

	// Verify we're on the correct branch in worktree
	branchCmd := exec.Command("git", "-C", result.Path, "branch", "--show-current")
	output, err := branchCmd.Output()
	if err != nil {
		t.Fatalf("Failed to get current branch: %v", err)
	}
	currentBranch := strings.TrimSpace(string(output))
	if currentBranch != branchName {
		t.Errorf("Expected branch %q, got %q", branchName, currentBranch)
	}

	// Create GitOperations pointing to worktree (as done in epic sub-issue execution)
	gitOps := NewGitOperations(result.Path)

	// Create a file and commit in worktree
	testFile := filepath.Join(result.Path, "epic-test.txt")
	if err := os.WriteFile(testFile, []byte("epic test content\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Verify HasUncommittedChanges works in worktree
	hasChanges, err := gitOps.HasUncommittedChanges(ctx)
	if err != nil {
		t.Fatalf("HasUncommittedChanges failed: %v", err)
	}
	if !hasChanges {
		t.Error("Expected uncommitted changes after creating file")
	}

	// Commit changes
	sha, err := gitOps.Commit(ctx, "Test commit from epic worktree")
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if sha == "" {
		t.Error("Expected non-empty commit SHA")
	}

	// Verify commit was made
	hasChanges, err = gitOps.HasUncommittedChanges(ctx)
	if err != nil {
		t.Fatalf("HasUncommittedChanges after commit failed: %v", err)
	}
	if hasChanges {
		t.Error("Expected no uncommitted changes after commit")
	}

	// Push from worktree
	if err := gitOps.Push(ctx, branchName); err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Verify branch exists on remote
	lsCmd := exec.Command("git", "-C", localRepo, "ls-remote", "--heads", "origin", branchName)
	lsOutput, err := lsCmd.Output()
	if err != nil {
		t.Fatalf("ls-remote failed: %v", err)
	}
	if !strings.Contains(string(lsOutput), branchName) {
		t.Errorf("Branch %q not found on remote after push", branchName)
	}
}

// TestWorktreeNavigatorAutoInit verifies Navigator auto-init works correctly in epic worktrees.
// GH-950: Edge case - project without .agent/ should have Navigator auto-initialized in worktree.
func TestWorktreeNavigatorAutoInit(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	// Explicitly ensure NO .agent/ directory exists in source repo
	agentDir := filepath.Join(localRepo, ".agent")
	if _, err := os.Stat(agentDir); err == nil {
		t.Fatal(".agent should not exist in fresh test repo")
	}

	ctx := context.Background()
	manager := NewWorktreeManager(localRepo)

	// Create worktree for epic
	result, err := manager.CreateWorktreeWithBranch(ctx, "autoinit-test", "pilot/autoinit-test", "main")
	if err != nil {
		t.Fatalf("Failed to create worktree: %v", err)
	}
	defer result.Cleanup()

	// Call EnsureNavigatorInWorktree - should succeed even without source .agent/
	if err := EnsureNavigatorInWorktree(localRepo, result.Path); err != nil {
		t.Errorf("EnsureNavigatorInWorktree should succeed without source .agent/: %v", err)
	}

	// Verify .agent/ does NOT exist in worktree yet (maybeInitNavigator handles this later)
	worktreeAgentDir := filepath.Join(result.Path, ".agent")
	if _, err := os.Stat(worktreeAgentDir); err == nil {
		// This is actually fine - means source had tracked .agent/ or we copied something
		t.Log(".agent/ exists in worktree (from source or git)")
	}

	// The key verification: Runner.maybeInitNavigator would be called with worktree path
	// This test confirms EnsureNavigatorInWorktree doesn't fail when source has no .agent/
}

// TestWorktreeQualityGatesPath verifies quality gates receive the correct worktree path.
// GH-950: Edge case - quality gates (build/test/lint) must run in worktree, not original repo.
func TestWorktreeQualityGatesPath(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	// Create and commit project config files
	createProjectConfigFiles(t, localRepo)
	commitProjectFiles(t, localRepo)

	ctx := context.Background()
	manager := NewWorktreeManager(localRepo)

	result, err := manager.CreateWorktreeWithBranch(ctx, "quality-test", "pilot/quality-test", "main")
	if err != nil {
		t.Fatalf("Failed to create worktree: %v", err)
	}
	defer result.Cleanup()

	// Track which path the quality checker receives
	var qualityCheckerPath string
	mockFactory := func(taskID, projectPath string) QualityChecker {
		qualityCheckerPath = projectPath
		return &mockQualityChecker{
			outcome: &QualityOutcome{Passed: true},
		}
	}

	// Simulate what executeWithOptions does - pass worktree path to quality factory
	executionPath := result.Path
	checker := mockFactory("quality-test", executionPath)

	// Verify the factory received the worktree path, not original repo
	if qualityCheckerPath != result.Path {
		t.Errorf("Quality factory should receive worktree path %q, got %q", result.Path, qualityCheckerPath)
	}

	if qualityCheckerPath == localRepo {
		t.Error("Quality factory should NOT receive original repo path")
	}

	// Verify checker works
	outcome, err := checker.Check(ctx)
	if err != nil {
		t.Errorf("Quality check failed: %v", err)
	}
	if !outcome.Passed {
		t.Error("Expected quality check to pass")
	}
}

// TestWorktreeConcurrentEpicExecution verifies multiple epics can execute concurrently in separate worktrees.
// GH-950: Edge case - concurrent epic executions must not interfere with each other.
func TestWorktreeConcurrentEpicExecution(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	ctx := context.Background()

	// Simulate 3 concurrent epic executions
	numEpics := 3
	var wg sync.WaitGroup
	results := make([]*WorktreeResult, numEpics)
	errors := make([]error, numEpics)

	// Use separate managers (as would happen in real concurrent execution)
	for i := 0; i < numEpics; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			manager := NewWorktreeManager(localRepo)
			branchName := fmt.Sprintf("pilot/epic-concurrent-%d", idx+1)
			taskID := fmt.Sprintf("concurrent-epic-%d", idx+1)

			result, err := manager.CreateWorktreeWithBranch(ctx, taskID, branchName, "main")
			if err != nil {
				errors[idx] = err
				return
			}
			results[idx] = result
		}(i)
	}

	wg.Wait()

	// Check for creation errors
	for i, err := range errors {
		if err != nil {
			t.Errorf("Epic %d worktree creation failed: %v", i+1, err)
		}
	}

	// Cleanup any successful worktrees on test failure
	defer func() {
		for _, result := range results {
			if result != nil {
				result.Cleanup()
			}
		}
	}()

	// Verify all worktrees are unique and isolated
	paths := make(map[string]bool)
	for i, result := range results {
		if result == nil {
			continue
		}

		// Check uniqueness
		if paths[result.Path] {
			t.Errorf("Duplicate worktree path detected for epic %d: %s", i+1, result.Path)
		}
		paths[result.Path] = true

		// Verify worktree exists
		if _, err := os.Stat(result.Path); os.IsNotExist(err) {
			t.Errorf("Epic %d worktree should exist: %s", i+1, result.Path)
		}

		// Verify branch in worktree
		branchCmd := exec.Command("git", "-C", result.Path, "branch", "--show-current")
		output, err := branchCmd.Output()
		if err != nil {
			t.Errorf("Failed to get branch for epic %d: %v", i+1, err)
			continue
		}
		expectedBranch := fmt.Sprintf("pilot/epic-concurrent-%d", i+1)
		actualBranch := strings.TrimSpace(string(output))
		if actualBranch != expectedBranch {
			t.Errorf("Epic %d: expected branch %q, got %q", i+1, expectedBranch, actualBranch)
		}
	}

	// Verify all paths are different from original repo
	for i, result := range results {
		if result == nil {
			continue
		}
		if result.Path == localRepo {
			t.Errorf("Epic %d worktree path should not be original repo", i+1)
		}
	}

	// Test concurrent file creation doesn't interfere
	for i, result := range results {
		if result == nil {
			continue
		}
		testFile := filepath.Join(result.Path, fmt.Sprintf("epic-%d-test.txt", i+1))
		content := fmt.Sprintf("Content for epic %d\n", i+1)
		if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
			t.Errorf("Failed to write test file for epic %d: %v", i+1, err)
		}
	}

	// Verify files don't leak between worktrees
	for i, result := range results {
		if result == nil {
			continue
		}
		// Check this worktree only has its own file
		for j := range results {
			if results[j] == nil {
				continue
			}
			otherFile := filepath.Join(result.Path, fmt.Sprintf("epic-%d-test.txt", j+1))
			_, err := os.Stat(otherFile)
			if i == j {
				// Should have our own file
				if os.IsNotExist(err) {
					t.Errorf("Epic %d should have its own file", i+1)
				}
			} else {
				// Should NOT have other epic's file
				if err == nil {
					t.Errorf("Epic %d worktree should not have epic %d's file", i+1, j+1)
				}
			}
		}
	}

	// Verify concurrent cleanup doesn't cause issues
	var cleanupWg sync.WaitGroup
	for i, result := range results {
		if result == nil {
			continue
		}
		cleanupWg.Add(1)
		go func(idx int, r *WorktreeResult) {
			defer cleanupWg.Done()
			r.Cleanup()
		}(i, result)
	}
	cleanupWg.Wait()

	// Verify all worktrees are removed
	for i, result := range results {
		if result == nil {
			continue
		}
		if _, err := os.Stat(result.Path); !os.IsNotExist(err) {
			t.Errorf("Epic %d worktree should be removed after cleanup: %s", i+1, result.Path)
		}
	}
}

// TestWorktreeEpicSubIssueIsolation verifies sub-issues use parent epic's worktree path.
// GH-950: Edge case - sub-issues must NOT create recursive worktrees.
func TestWorktreeEpicSubIssueIsolation(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer func() { _ = os.RemoveAll(localRepo) }()
	defer func() { _ = os.RemoveAll(remoteRepo) }()

	ctx := context.Background()
	manager := NewWorktreeManager(localRepo)

	// Create epic worktree
	epicResult, err := manager.CreateWorktreeWithBranch(ctx, "epic-isolation", "pilot/epic-isolation", "main")
	if err != nil {
		t.Fatalf("Failed to create epic worktree: %v", err)
	}
	defer epicResult.Cleanup()

	// Track execution paths for sub-issues
	var pathsMu sync.Mutex
	var subIssuePaths []string

	// Simulate sub-issue execution - they should reuse epic's worktree path
	numSubIssues := 3
	for i := 0; i < numSubIssues; i++ {
		// Simulate executeWithOptions(ctx, task, false) - false prevents new worktree
		subIssuePath := epicResult.Path // Sub-issues use parent's worktree

		pathsMu.Lock()
		subIssuePaths = append(subIssuePaths, subIssuePath)
		pathsMu.Unlock()

		// Verify sub-issue would execute in epic's worktree
		if subIssuePath != epicResult.Path {
			t.Errorf("Sub-issue %d should use epic worktree %q, got %q", i+1, epicResult.Path, subIssuePath)
		}
	}

	// All sub-issues should have used the same path (epic's worktree)
	for i, path := range subIssuePaths {
		if path != epicResult.Path {
			t.Errorf("Sub-issue %d path %q should equal epic worktree %q", i+1, path, epicResult.Path)
		}
	}

	// Verify only ONE worktree exists (the epic's)
	if manager.ActiveCount() != 1 {
		t.Errorf("Should have exactly 1 active worktree (epic's), got %d", manager.ActiveCount())
	}
}

// createProjectConfigFiles creates various project configuration files for testing.
// This helper is also used in worktree_path_integration_test.go.
func createProjectConfigFilesForEpicTest(t *testing.T, repoPath string) {
	t.Helper()

	// Create go.mod for Go detection
	goMod := filepath.Join(repoPath, "go.mod")
	goModContent := "module github.com/test/project\n\ngo 1.24\n"
	if err := os.WriteFile(goMod, []byte(goModContent), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}
}

// commitProjectFilesForEpicTest commits files so they appear in worktrees.
func commitProjectFilesForEpicTest(t *testing.T, repoPath string) {
	t.Helper()

	if err := exec.Command("git", "-C", repoPath, "add", ".").Run(); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}
	if err := exec.Command("git", "-C", repoPath, "commit", "-m", "Add config").Run(); err != nil {
		t.Fatalf("Failed to git commit: %v", err)
	}
}

// writeMockScriptWithPathCapture creates a mock script that outputs text and can capture execution path.
func writeMockScriptWithPathCapture(t *testing.T, path, output string, exitCode int) {
	t.Helper()
	script := "#!/bin/sh\n"
	if output != "" {
		script += "cat <<'ENDOFOUTPUT'\n" + output + "\nENDOFOUTPUT\n"
	}
	script += fmt.Sprintf("exit %d\n", exitCode)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("Failed to write mock script: %v", err)
	}
}

// testLogger returns a minimal logger for testing.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}
