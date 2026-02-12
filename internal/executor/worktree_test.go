package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
