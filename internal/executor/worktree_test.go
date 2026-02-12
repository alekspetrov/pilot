package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// setupTestRepo creates a temporary git repository for testing worktrees.
func setupTestRepo(t *testing.T) string {
	t.Helper()

	// Create temp directory
	dir, err := os.MkdirTemp("", "worktree-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		os.RemoveAll(dir)
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git user for commits
	exec.Command("git", "-C", dir, "config", "user.email", "test@example.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test User").Run()

	// Create initial commit (required for worktree)
	testFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test Repo\n"), 0644); err != nil {
		os.RemoveAll(dir)
		t.Fatalf("failed to create test file: %v", err)
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = dir
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		os.RemoveAll(dir)
		t.Fatalf("failed to create initial commit: %v", err)
	}

	return dir
}

func TestCreateWorktree(t *testing.T) {
	repoPath := setupTestRepo(t)
	defer os.RemoveAll(repoPath)

	ctx := context.Background()
	manager := NewWorktreeManager(repoPath)

	result, err := manager.CreateWorktree(ctx, "GH-123")
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	// Verify worktree was created
	if _, err := os.Stat(result.Path); os.IsNotExist(err) {
		t.Error("worktree directory was not created")
	}

	// Verify it's a git worktree
	gitDir := filepath.Join(result.Path, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		t.Error("worktree does not have .git file/dir")
	}

	// Verify active count
	if count := manager.ActiveCount(); count != 1 {
		t.Errorf("expected 1 active worktree, got %d", count)
	}

	// Run cleanup
	result.Cleanup()

	// Verify worktree was removed
	if _, err := os.Stat(result.Path); !os.IsNotExist(err) {
		t.Error("worktree directory was not removed after cleanup")
	}

	// Verify active count after cleanup
	if count := manager.ActiveCount(); count != 0 {
		t.Errorf("expected 0 active worktrees after cleanup, got %d", count)
	}
}

func TestCleanupIsIdempotent(t *testing.T) {
	repoPath := setupTestRepo(t)
	defer os.RemoveAll(repoPath)

	ctx := context.Background()
	manager := NewWorktreeManager(repoPath)

	result, err := manager.CreateWorktree(ctx, "GH-456")
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	// Call cleanup multiple times - should not panic or error
	result.Cleanup()
	result.Cleanup()
	result.Cleanup()

	// Verify worktree is still gone
	if _, err := os.Stat(result.Path); !os.IsNotExist(err) {
		t.Error("worktree should be removed")
	}
}

func TestCleanupOnPanic(t *testing.T) {
	repoPath := setupTestRepo(t)
	defer os.RemoveAll(repoPath)

	ctx := context.Background()
	manager := NewWorktreeManager(repoPath)

	var worktreePath string

	// Simulate panic in a function that uses worktree
	func() {
		defer func() {
			recover() // Recover from panic
		}()

		result, err := manager.CreateWorktree(ctx, "GH-789")
		if err != nil {
			t.Fatalf("CreateWorktree failed: %v", err)
		}
		worktreePath = result.Path

		// Defer cleanup BEFORE any code that might panic
		defer result.Cleanup()

		// Simulate panic during execution
		panic("simulated execution error")
	}()

	// Give cleanup a moment to complete
	time.Sleep(100 * time.Millisecond)

	// Verify worktree was cleaned up despite panic
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Error("worktree should be cleaned up even after panic")
	}

	// Verify tracking is cleared
	if count := manager.ActiveCount(); count != 0 {
		t.Errorf("expected 0 active worktrees after panic cleanup, got %d", count)
	}
}

func TestConcurrentWorktrees(t *testing.T) {
	repoPath := setupTestRepo(t)
	defer os.RemoveAll(repoPath)

	ctx := context.Background()
	manager := NewWorktreeManager(repoPath)

	const numWorktrees = 5
	var wg sync.WaitGroup
	results := make([]*WorktreeResult, numWorktrees)
	errors := make([]error, numWorktrees)

	// Create multiple worktrees concurrently
	for i := 0; i < numWorktrees; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			taskID := "GH-" + string(rune('A'+idx))
			result, err := manager.CreateWorktree(ctx, taskID)
			results[idx] = result
			errors[idx] = err
		}(i)
	}
	wg.Wait()

	// Verify all worktrees were created
	for i := 0; i < numWorktrees; i++ {
		if errors[i] != nil {
			t.Errorf("worktree %d failed: %v", i, errors[i])
			continue
		}
		if _, err := os.Stat(results[i].Path); os.IsNotExist(err) {
			t.Errorf("worktree %d was not created", i)
		}
	}

	// Verify unique paths
	paths := make(map[string]bool)
	for i := 0; i < numWorktrees; i++ {
		if results[i] != nil {
			if paths[results[i].Path] {
				t.Error("duplicate worktree paths detected")
			}
			paths[results[i].Path] = true
		}
	}

	// Verify active count
	if count := manager.ActiveCount(); count != numWorktrees {
		t.Errorf("expected %d active worktrees, got %d", numWorktrees, count)
	}

	// Cleanup all concurrently
	for i := 0; i < numWorktrees; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if results[idx] != nil {
				results[idx].Cleanup()
			}
		}(i)
	}
	wg.Wait()

	// Verify all cleaned up
	if count := manager.ActiveCount(); count != 0 {
		t.Errorf("expected 0 active worktrees after cleanup, got %d", count)
	}
}

func TestCleanupAll(t *testing.T) {
	repoPath := setupTestRepo(t)
	defer os.RemoveAll(repoPath)

	ctx := context.Background()
	manager := NewWorktreeManager(repoPath)

	// Create multiple worktrees
	paths := make([]string, 3)
	for i := 0; i < 3; i++ {
		result, err := manager.CreateWorktree(ctx, "task-"+string(rune('1'+i)))
		if err != nil {
			t.Fatalf("CreateWorktree %d failed: %v", i, err)
		}
		paths[i] = result.Path
	}

	// Verify all created
	if count := manager.ActiveCount(); count != 3 {
		t.Errorf("expected 3 active worktrees, got %d", count)
	}

	// Cleanup all at once
	manager.CleanupAll()

	// Verify all removed
	if count := manager.ActiveCount(); count != 0 {
		t.Errorf("expected 0 active worktrees after CleanupAll, got %d", count)
	}

	for i, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("worktree %d should be removed after CleanupAll", i)
		}
	}
}

func TestStandaloneCreateWorktree(t *testing.T) {
	repoPath := setupTestRepo(t)
	defer os.RemoveAll(repoPath)

	ctx := context.Background()

	// Use standalone function
	path, cleanup, err := CreateWorktree(ctx, repoPath, "standalone-task")
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	// Verify created
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("worktree was not created")
	}

	// Cleanup via defer pattern
	cleanup()

	// Verify removed
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("worktree was not removed after cleanup")
	}
}

func TestSanitizeBranchName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"GH-123", "GH-123"},
		{"task_name", "task_name"},
		{"feature/branch", "feature-branch"},
		{"special@chars!", "special-chars-"},
		{"spaces in name", "spaces-in-name"},
		{"UPPER-lower-123", "UPPER-lower-123"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeBranchName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeBranchName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// setupTestRepoWithRemote creates a local repo with a "remote" for push testing.
// Returns (localRepo, remoteRepo) paths.
func setupTestRepoWithRemote(t *testing.T) (string, string) {
	t.Helper()

	// Create "remote" bare repository
	remoteDir, err := os.MkdirTemp("", "worktree-remote-*")
	if err != nil {
		t.Fatalf("failed to create remote dir: %v", err)
	}

	cmd := exec.Command("git", "init", "--bare")
	cmd.Dir = remoteDir
	if err := cmd.Run(); err != nil {
		os.RemoveAll(remoteDir)
		t.Fatalf("failed to init bare repo: %v", err)
	}

	// Create local repository
	localDir, err := os.MkdirTemp("", "worktree-local-*")
	if err != nil {
		os.RemoveAll(remoteDir)
		t.Fatalf("failed to create local dir: %v", err)
	}

	cmd = exec.Command("git", "init")
	cmd.Dir = localDir
	if err := cmd.Run(); err != nil {
		os.RemoveAll(remoteDir)
		os.RemoveAll(localDir)
		t.Fatalf("failed to init local repo: %v", err)
	}

	// Configure git user
	exec.Command("git", "-C", localDir, "config", "user.email", "test@example.com").Run()
	exec.Command("git", "-C", localDir, "config", "user.name", "Test User").Run()

	// Create initial commit
	testFile := filepath.Join(localDir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Test Repo\n"), 0644); err != nil {
		os.RemoveAll(remoteDir)
		os.RemoveAll(localDir)
		t.Fatalf("failed to create test file: %v", err)
	}

	exec.Command("git", "-C", localDir, "add", ".").Run()
	cmd = exec.Command("git", "-C", localDir, "commit", "-m", "Initial commit")
	if err := cmd.Run(); err != nil {
		os.RemoveAll(remoteDir)
		os.RemoveAll(localDir)
		t.Fatalf("failed to commit: %v", err)
	}

	// Add remote
	cmd = exec.Command("git", "-C", localDir, "remote", "add", "origin", remoteDir)
	if err := cmd.Run(); err != nil {
		os.RemoveAll(remoteDir)
		os.RemoveAll(localDir)
		t.Fatalf("failed to add remote: %v", err)
	}

	// Push initial commit to remote
	cmd = exec.Command("git", "-C", localDir, "push", "-u", "origin", "HEAD:main")
	if err := cmd.Run(); err != nil {
		os.RemoveAll(remoteDir)
		os.RemoveAll(localDir)
		t.Fatalf("failed to push to remote: %v", err)
	}

	return localDir, remoteDir
}

func TestCreateWorktreeWithBranch(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer os.RemoveAll(localRepo)
	defer os.RemoveAll(remoteRepo)

	ctx := context.Background()
	manager := NewWorktreeManager(localRepo)

	branchName := "pilot/test-branch"
	result, err := manager.CreateWorktreeWithBranch(ctx, "GH-999", branchName, "main")
	if err != nil {
		t.Fatalf("CreateWorktreeWithBranch failed: %v", err)
	}

	// Verify worktree exists
	if _, err := os.Stat(result.Path); os.IsNotExist(err) {
		t.Error("worktree directory was not created")
	}

	// Verify we're on the correct branch (not detached HEAD)
	branchCmd := exec.Command("git", "-C", result.Path, "branch", "--show-current")
	output, err := branchCmd.Output()
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}
	currentBranch := strings.TrimSpace(string(output))
	if currentBranch != branchName {
		t.Errorf("expected branch %q, got %q", branchName, currentBranch)
	}

	// Cleanup
	result.Cleanup()

	// Verify worktree removed
	if _, err := os.Stat(result.Path); !os.IsNotExist(err) {
		t.Error("worktree should be removed after cleanup")
	}

	// Verify branch deleted
	branchExistsCmd := exec.Command("git", "-C", localRepo, "show-ref", "--verify", "--quiet", "refs/heads/"+branchName)
	if branchExistsCmd.Run() == nil {
		t.Error("branch should be deleted after cleanup")
	}
}

func TestWorktreeCanPushToRemote(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer os.RemoveAll(localRepo)
	defer os.RemoveAll(remoteRepo)

	ctx := context.Background()
	manager := NewWorktreeManager(localRepo)

	branchName := "pilot/push-test"
	result, err := manager.CreateWorktreeWithBranch(ctx, "GH-888", branchName, "main")
	if err != nil {
		t.Fatalf("CreateWorktreeWithBranch failed: %v", err)
	}
	defer result.Cleanup()

	// Create a file in the worktree
	testFile := filepath.Join(result.Path, "new-file.txt")
	if err := os.WriteFile(testFile, []byte("test content\n"), 0644); err != nil {
		t.Fatalf("failed to create file in worktree: %v", err)
	}

	// Stage and commit in worktree
	exec.Command("git", "-C", result.Path, "add", ".").Run()
	commitCmd := exec.Command("git", "-C", result.Path, "commit", "-m", "Add new file")
	if err := commitCmd.Run(); err != nil {
		t.Fatalf("failed to commit in worktree: %v", err)
	}

	// Push from worktree to remote
	pushCmd := exec.Command("git", "-C", result.Path, "push", "-u", "origin", branchName)
	output, err := pushCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to push from worktree: %v: %s", err, output)
	}

	// Verify the branch exists on remote
	lsRemoteCmd := exec.Command("git", "-C", localRepo, "ls-remote", "--heads", "origin", branchName)
	lsOutput, err := lsRemoteCmd.Output()
	if err != nil {
		t.Fatalf("ls-remote failed: %v", err)
	}
	if !strings.Contains(string(lsOutput), branchName) {
		t.Errorf("branch %q not found on remote after push", branchName)
	}
}

func TestVerifyRemoteAccess(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer os.RemoveAll(localRepo)
	defer os.RemoveAll(remoteRepo)

	ctx := context.Background()
	manager := NewWorktreeManager(localRepo)

	result, err := manager.CreateWorktree(ctx, "GH-777")
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}
	defer result.Cleanup()

	// Verify remote is accessible from worktree
	if err := manager.VerifyRemoteAccess(ctx, result.Path); err != nil {
		t.Errorf("VerifyRemoteAccess failed: %v", err)
	}
}

func TestVerifyRemoteAccessNoRemote(t *testing.T) {
	// Create repo without remote
	repoPath := setupTestRepo(t)
	defer os.RemoveAll(repoPath)

	ctx := context.Background()
	manager := NewWorktreeManager(repoPath)

	result, err := manager.CreateWorktree(ctx, "GH-666")
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}
	defer result.Cleanup()

	// Should fail - no remote configured
	err = manager.VerifyRemoteAccess(ctx, result.Path)
	if err == nil {
		t.Error("expected error when remote is not configured")
	}
}

func TestStandaloneCreateWorktreeWithBranch(t *testing.T) {
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer os.RemoveAll(localRepo)
	defer os.RemoveAll(remoteRepo)

	ctx := context.Background()

	branchName := "pilot/standalone-branch"
	path, cleanup, err := CreateWorktreeWithBranch(ctx, localRepo, "standalone", branchName, "main")
	if err != nil {
		t.Fatalf("CreateWorktreeWithBranch failed: %v", err)
	}

	// Verify worktree exists with correct branch
	branchCmd := exec.Command("git", "-C", path, "branch", "--show-current")
	output, err := branchCmd.Output()
	if err != nil {
		cleanup()
		t.Fatalf("failed to get branch: %v", err)
	}
	if strings.TrimSpace(string(output)) != branchName {
		cleanup()
		t.Errorf("expected branch %q, got %q", branchName, string(output))
	}

	cleanup()

	// Verify cleanup
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("worktree should be removed after cleanup")
	}
}

func TestWorktreeGitOperationsIntegration(t *testing.T) {
	// Test that GitOperations works correctly with worktree path
	localRepo, remoteRepo := setupTestRepoWithRemote(t)
	defer os.RemoveAll(localRepo)
	defer os.RemoveAll(remoteRepo)

	ctx := context.Background()
	manager := NewWorktreeManager(localRepo)

	branchName := "pilot/git-ops-test"
	result, err := manager.CreateWorktreeWithBranch(ctx, "GH-555", branchName, "main")
	if err != nil {
		t.Fatalf("CreateWorktreeWithBranch failed: %v", err)
	}
	defer result.Cleanup()

	// Create GitOperations pointing to worktree
	gitOps := NewGitOperations(result.Path)

	// Test GetCurrentBranch
	currentBranch, err := gitOps.GetCurrentBranch(ctx)
	if err != nil {
		t.Errorf("GetCurrentBranch failed: %v", err)
	}
	if currentBranch != branchName {
		t.Errorf("expected branch %q, got %q", branchName, currentBranch)
	}

	// Test HasUncommittedChanges (should be false initially)
	hasChanges, err := gitOps.HasUncommittedChanges(ctx)
	if err != nil {
		t.Errorf("HasUncommittedChanges failed: %v", err)
	}
	if hasChanges {
		t.Error("expected no uncommitted changes initially")
	}

	// Create a change
	testFile := filepath.Join(result.Path, "test.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Test HasUncommittedChanges (should be true now)
	hasChanges, err = gitOps.HasUncommittedChanges(ctx)
	if err != nil {
		t.Errorf("HasUncommittedChanges failed: %v", err)
	}
	if !hasChanges {
		t.Error("expected uncommitted changes after creating file")
	}

	// Test Commit
	sha, err := gitOps.Commit(ctx, "Test commit from worktree")
	if err != nil {
		t.Errorf("Commit failed: %v", err)
	}
	if sha == "" {
		t.Error("expected non-empty commit SHA")
	}

	// Test Push
	if err := gitOps.Push(ctx, branchName); err != nil {
		t.Errorf("Push failed: %v", err)
	}

	// Verify push succeeded by checking remote
	lsCmd := exec.Command("git", "-C", localRepo, "ls-remote", "--heads", "origin", branchName)
	output, _ := lsCmd.Output()
	if !strings.Contains(string(output), branchName) {
		t.Error("branch not found on remote after push")
	}
}
