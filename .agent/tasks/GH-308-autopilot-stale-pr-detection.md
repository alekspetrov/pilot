# GH-308: Detect Externally Merged/Closed PRs in Autopilot

**Status**: 🚧 In Progress
**Created**: 2026-02-01
**Assignee**: Pilot

---

## Context

**Problem**:
The autopilot controller tracks PRs in memory (`activePRs` map) but never checks if PRs were merged or closed externally (by humans or other processes). This causes stale state in the dashboard:

- PR #305 shows `waiting_ci` but was already MERGED
- PR #307 shows `pr_created` but CI has already passed

**Root Cause**:
In `internal/autopilot/controller.go`, the `processAllPRs()` function iterates over tracked PRs without verifying they're still open. The only way a PR gets removed is via `removePR()` which is only called from:
- `handleMerged()` (dev mode)
- `handlePostMergeCI()` (stage/prod mode)

If a PR is merged externally, it stays in tracking forever.

**Goal**:
Before processing each PR, check if it's still open. If merged/closed, remove from tracking.

**Success Criteria**:
- [ ] PRs merged externally are detected and removed from tracking
- [ ] PRs closed without merge are detected and removed
- [ ] Dashboard shows accurate PR states
- [ ] No stale PRs accumulate over time

---

## Implementation Plan

### Phase 1: Add PR State Check

**Goal**: Query GitHub to verify PR is still open before processing

**Tasks**:
- [ ] Add `GetPullRequest(ctx, owner, repo, number)` to GitHub client if not exists
- [ ] In `processAllPRs()`, check PR state before calling `ProcessPR()`
- [ ] If PR state is "merged" or "closed", call `removePR()` and skip processing
- [ ] Log when external merge/close is detected

**Files**:
- `internal/autopilot/controller.go` - Add state check in processing loop
- `internal/adapters/github/client.go` - Add GetPullRequest if needed

### Phase 2: Handle State Transitions

**Goal**: Properly transition tracked state when external events detected

**Tasks**:
- [ ] If PR merged externally, optionally notify (Telegram) before removing
- [ ] If PR closed without merge, mark as failed with reason
- [ ] Consider adding `StageExternallyMerged` and `StageExternallyClosed` terminal states

**Files**:
- `internal/autopilot/controller.go` - State transitions
- `internal/autopilot/types.go` - New stages (optional)

---

## Technical Decisions

| Decision | Options Considered | Chosen | Reasoning |
|----------|-------------------|--------|-----------|
| Check timing | Every tick vs on-demand | Every tick | Simple, catches external changes quickly |
| Notification | Silent removal vs notify | Notify on merge | User should know PR was merged |

---

## Verify

```bash
# Run tests
go test ./internal/autopilot/... -v

# Manual verification
# 1. Start pilot with autopilot
# 2. Create a PR, let it be tracked
# 3. Merge PR manually via `gh pr merge`
# 4. Verify dashboard removes PR within 30s
```

---

## Done

- [ ] External merges detected within one poll interval (30s)
- [ ] External closes detected within one poll interval
- [ ] Dashboard shows accurate active PRs
- [ ] Tests cover external merge/close scenarios

---

**Last Updated**: 2026-02-01
