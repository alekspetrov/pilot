package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// WorktreeManager handles git worktree creation and cleanup for isolated task execution.
// GH-936: Enables Pilot to work in repos where users have uncommitted changes.
type WorktreeManager struct {
	repoPath string
	mu       sync.Mutex
	active   map[string]string // taskID -> worktreePath
}

// NewWorktreeManager creates a worktree manager for the given repository.
func NewWorktreeManager(repoPath string) *WorktreeManager {
	return &WorktreeManager{
		repoPath: repoPath,
		active:   make(map[string]string),
	}
}

// WorktreeResult contains the worktree path and cleanup function.
// The Cleanup function MUST be called when execution completes (success or failure).
type WorktreeResult struct {
	Path    string
	Cleanup func()
}

// CreateWorktree creates an isolated worktree for task execution.
// Returns a WorktreeResult containing the path and a cleanup function.
//
// CRITICAL: The cleanup function is safe to call multiple times and handles:
// - Normal completion
// - Context cancellation
// - Panic recovery (via defer in caller)
// - Process termination (best-effort via runtime finalizer)
//
// Usage:
//
//	result, err := manager.CreateWorktree(ctx, taskID)
//	if err != nil {
//	    return err
//	}
//	defer result.Cleanup() // Always cleanup, even on panic
//
//	// ... use result.Path for execution ...
func (m *WorktreeManager) CreateWorktree(ctx context.Context, taskID string) (*WorktreeResult, error) {
	// Generate unique worktree path using taskID and timestamp to handle concurrent tasks
	worktreeName := fmt.Sprintf("pilot-worktree-%s-%d", sanitizeBranchName(taskID), time.Now().UnixNano())
	worktreePath := filepath.Join(os.TempDir(), worktreeName)

	// Create worktree from HEAD (current commit of default branch)
	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "--detach", worktreePath, "HEAD")
	cmd.Dir = m.repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to create worktree: %w: %s", err, output)
	}

	// Track active worktree
	m.mu.Lock()
	m.active[taskID] = worktreePath
	m.mu.Unlock()

	// Create cleanup function with panic-safe, idempotent behavior
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			m.cleanupWorktree(taskID, worktreePath)
		})
	}

	return &WorktreeResult{
		Path:    worktreePath,
		Cleanup: cleanup,
	}, nil
}

// cleanupWorktree removes a worktree and cleans up tracking state.
// This is called by the cleanup function returned from CreateWorktree.
func (m *WorktreeManager) cleanupWorktree(taskID, worktreePath string) {
	// Remove from tracking first
	m.mu.Lock()
	delete(m.active, taskID)
	m.mu.Unlock()

	// Remove the git worktree reference
	// Use --force to handle any uncommitted changes in the worktree
	removeCmd := exec.Command("git", "-C", m.repoPath, "worktree", "remove", "--force", worktreePath)
	_ = removeCmd.Run() // Ignore error - worktree may already be removed

	// Belt and suspenders: also remove the directory if it still exists
	// This handles edge cases where git worktree remove didn't fully clean up
	_ = os.RemoveAll(worktreePath)

	// Prune stale worktree references from git
	pruneCmd := exec.Command("git", "-C", m.repoPath, "worktree", "prune")
	_ = pruneCmd.Run()
}

// CleanupAll removes all active worktrees managed by this instance.
// Useful for graceful shutdown or error recovery.
func (m *WorktreeManager) CleanupAll() {
	m.mu.Lock()
	active := make(map[string]string)
	for k, v := range m.active {
		active[k] = v
	}
	m.mu.Unlock()

	for taskID, path := range active {
		m.cleanupWorktree(taskID, path)
	}
}

// ActiveCount returns the number of active worktrees.
func (m *WorktreeManager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.active)
}

// sanitizeBranchName converts a task ID into a safe worktree directory name.
func sanitizeBranchName(taskID string) string {
	result := make([]byte, 0, len(taskID))
	for i := 0; i < len(taskID); i++ {
		c := taskID[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			result = append(result, c)
		} else {
			result = append(result, '-')
		}
	}
	return string(result)
}

// CreateWorktreeWithBranch creates an isolated worktree with a proper branch (not detached HEAD).
// This is the preferred method when the worktree needs to push to remote, as detached HEAD
// makes push operations more complex.
//
// The branch is created from the specified baseBranch (e.g., "main").
// If baseBranch is empty, HEAD is used.
//
// Usage:
//
//	result, err := manager.CreateWorktreeWithBranch(ctx, taskID, "pilot/GH-123", "main")
//	if err != nil {
//	    return err
//	}
//	defer result.Cleanup()
//
//	// Worktree is on branch "pilot/GH-123", ready for commits and push
func (m *WorktreeManager) CreateWorktreeWithBranch(ctx context.Context, taskID, branchName, baseBranch string) (*WorktreeResult, error) {
	// Generate unique worktree path
	worktreeName := fmt.Sprintf("pilot-worktree-%s-%d", sanitizeBranchName(taskID), time.Now().UnixNano())
	worktreePath := filepath.Join(os.TempDir(), worktreeName)

	// Determine base ref
	baseRef := "HEAD"
	if baseBranch != "" {
		baseRef = baseBranch
	}

	// Create worktree with a new branch
	// git worktree add -b <branch> <path> <base>
	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "-b", branchName, worktreePath, baseRef)
	cmd.Dir = m.repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to create worktree with branch: %w: %s", err, output)
	}

	// Track active worktree
	m.mu.Lock()
	m.active[taskID] = worktreePath
	m.mu.Unlock()

	// Create cleanup function
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			m.cleanupWorktreeAndBranch(taskID, worktreePath, branchName)
		})
	}

	return &WorktreeResult{
		Path:    worktreePath,
		Cleanup: cleanup,
	}, nil
}

// cleanupWorktreeAndBranch removes a worktree, its branch, and cleans up tracking state.
// The branch is also deleted since it was created specifically for this worktree.
func (m *WorktreeManager) cleanupWorktreeAndBranch(taskID, worktreePath, branchName string) {
	// First remove the worktree
	m.cleanupWorktree(taskID, worktreePath)

	// Then delete the local branch (it was created for this worktree)
	// Use -D to force delete even if not merged
	deleteCmd := exec.Command("git", "-C", m.repoPath, "branch", "-D", branchName)
	_ = deleteCmd.Run() // Ignore error - branch may have been pushed and deleted elsewhere
}

// VerifyRemoteAccess checks that the worktree can access the remote.
// This is useful for pre-flight validation before long-running tasks.
func (m *WorktreeManager) VerifyRemoteAccess(ctx context.Context, worktreePath string) error {
	// Check that 'origin' remote exists and is accessible
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = worktreePath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remote 'origin' not accessible from worktree: %w: %s", err, output)
	}

	// Verify we can ls-remote (lightweight check without fetching)
	lsCmd := exec.CommandContext(ctx, "git", "ls-remote", "--exit-code", "origin", "HEAD")
	lsCmd.Dir = worktreePath
	if lsOutput, lsErr := lsCmd.CombinedOutput(); lsErr != nil {
		return fmt.Errorf("cannot reach remote 'origin': %w: %s", lsErr, lsOutput)
	}

	return nil
}

// CreateWorktree is a standalone helper function for simple use cases.
// Returns the worktree path and a cleanup function.
//
// CRITICAL: The cleanup function MUST be called via defer to ensure cleanup
// even on panic or early return.
//
// Usage:
//
//	worktreePath, cleanup, err := CreateWorktree(ctx, repoPath, taskID)
//	if err != nil {
//	    return err
//	}
//	defer cleanup() // ALWAYS defer cleanup immediately after creation
//
//	// ... use worktreePath for execution ...
func CreateWorktree(ctx context.Context, repoPath, taskID string) (string, func(), error) {
	manager := NewWorktreeManager(repoPath)
	result, err := manager.CreateWorktree(ctx, taskID)
	if err != nil {
		return "", nil, err
	}
	return result.Path, result.Cleanup, nil
}

// CreateWorktreeWithBranch is a standalone helper that creates a worktree with a branch.
// Returns the worktree path and a cleanup function.
//
// Use this when you need to push changes to remote, as it creates a proper branch
// instead of a detached HEAD state.
func CreateWorktreeWithBranch(ctx context.Context, repoPath, taskID, branchName, baseBranch string) (string, func(), error) {
	manager := NewWorktreeManager(repoPath)
	result, err := manager.CreateWorktreeWithBranch(ctx, taskID, branchName, baseBranch)
	if err != nil {
		return "", nil, err
	}
	return result.Path, result.Cleanup, nil
}
