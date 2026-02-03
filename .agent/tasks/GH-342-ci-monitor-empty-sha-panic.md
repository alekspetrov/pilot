# GH-342: Fix CI Monitor Panic on Empty SHA

**Status**: 🚧 Planning
**Created**: 2026-02-02
**Priority**: P0 (Critical - causes crash)
**Assignee**: Pilot

---

## Context

**Problem**:
Autopilot crashes with panic when CI monitor receives an empty SHA string. The `WaitForCI` function attempts to slice `sha[:7]` for logging without checking if the string is empty.

**Root Cause**:
```
panic: runtime error: slice bounds out of range [:7] with length 0

goroutine 26 [running]:
github.com/alekspetrov/pilot/internal/autopilot.(*CIMonitor).WaitForCI(...)
    /home/runner/work/pilot/pilot/internal/autopilot/ci_monitor.go:50 +0x4d0
```

**Trigger Scenario**:
1. PR is registered with empty `HeadSHA` (can happen when `result.CommitSHA` is empty)
2. Autopilot processes the PR and calls `WaitForCI(ctx, "")`
3. Line 50 attempts `sha[:7]` → panic

**Evidence from logs**:
```
2026/02/02 12:18:06 INFO PR merged externally, removing from tracking component=autopilot pr=339
panic: runtime error: slice bounds out of range [:7] with length 0
```

**Goal**:
Prevent panic by validating SHA before slicing. Return early with error if SHA is empty.

**Success Criteria**:
- [ ] No panic when `WaitForCI` receives empty SHA
- [ ] Proper error returned instead of panic
- [ ] All existing tests pass
- [ ] New test case covers empty SHA scenario

---

## Implementation Plan

### Phase 1: Fix Empty SHA Guard

**Goal**: Add validation to prevent panic on empty or short SHA

**Tasks**:
- [ ] Add SHA validation at start of `WaitForCI`
- [ ] Use safe SHA display helper for logging
- [ ] Add same guard to line 67 (status log)

**Files**:
- `internal/autopilot/ci_monitor.go` - Add SHA validation

**Implementation**:

```go
// WaitForCI polls until all required checks complete or timeout.
func (m *CIMonitor) WaitForCI(ctx context.Context, sha string) (CIStatus, error) {
    // Validate SHA before proceeding
    if sha == "" {
        return CIPending, fmt.Errorf("empty SHA provided")
    }

    deadline := time.Now().Add(m.waitTimeout)
    ticker := time.NewTicker(m.pollInterval)
    defer ticker.Stop()

    // Safe SHA display (handles short SHAs)
    displaySHA := sha
    if len(sha) > 7 {
        displaySHA = sha[:7]
    }

    m.log.Info("waiting for CI", "sha", displaySHA, "timeout", m.waitTimeout, "required_checks", m.requiredChecks)

    for {
        select {
        case <-ctx.Done():
            return CIPending, ctx.Err()
        case <-ticker.C:
            if time.Now().After(deadline) {
                return CIPending, fmt.Errorf("CI timeout after %v", m.waitTimeout)
            }

            status, err := m.checkStatus(ctx, sha)
            if err != nil {
                m.log.Warn("CI status check failed", "error", err)
                continue
            }

            m.log.Info("CI status", "sha", displaySHA, "status", status)

            if status == CISuccess || status == CIFailure {
                return status, nil
            }
        }
    }
}
```

### Phase 2: Add Unit Test

**Goal**: Ensure empty SHA case is tested

**Tasks**:
- [ ] Add test case for empty SHA
- [ ] Add test case for short SHA (< 7 chars)

**Files**:
- `internal/autopilot/ci_monitor_test.go` - Add test cases

**Test Cases**:
```go
func TestCIMonitor_WaitForCI_EmptySHA(t *testing.T) {
    ghClient := github.NewClient("token", nil)
    cfg := &Config{
        CIPollInterval: time.Second,
        CIWaitTimeout:  time.Minute,
    }
    monitor := NewCIMonitor(ghClient, "owner", "repo", cfg)

    status, err := monitor.WaitForCI(context.Background(), "")

    if err == nil {
        t.Error("expected error for empty SHA")
    }
    if status != CIPending {
        t.Errorf("expected CIPending, got %v", status)
    }
    if !strings.Contains(err.Error(), "empty SHA") {
        t.Errorf("expected 'empty SHA' error, got %v", err)
    }
}

func TestCIMonitor_WaitForCI_ShortSHA(t *testing.T) {
    // Test that short SHA (< 7 chars) doesn't panic
    // Should still work, just display full SHA
}
```

---

## Technical Decisions

| Decision | Options Considered | Chosen | Reasoning |
|----------|-------------------|--------|-----------|
| Error handling | Panic recovery vs early return | Early return with error | More idiomatic Go, cleaner control flow |
| SHA display | Always truncate vs conditional | Conditional truncation | Handles edge cases gracefully |

---

## Dependencies

**Requires**:
- None

**Blocks**:
- Autopilot stability in production

---

## Verify

Run these commands to validate the implementation:

```bash
# Run specific tests
go test -v ./internal/autopilot/... -run TestCIMonitor

# Run all tests
make test

# Lint
make lint
```

---

## Done

Observable outcomes that prove completion:

- [ ] `internal/autopilot/ci_monitor.go` has SHA validation guard
- [ ] No panic when calling `WaitForCI("")`
- [ ] Error returned: "empty SHA provided"
- [ ] All tests pass including new empty SHA test
- [ ] `make lint` passes

---

## Notes

Related issues:
- This is separate from the PR URL parsing issue (GH-342)
- The empty SHA originates from `result.CommitSHA` being empty in executor

---

**Last Updated**: 2026-02-02
