# GH-318: Add CI failure dispatch to ci.yml

**Status**: 🚧 In Progress
**Created**: 2026-02-01
**Assignee**: Pilot

---

## Context

**Problem**:
The `ci-autofix.yml` workflow uses `workflow_run` trigger which has never worked (0 successful triggers in 200+ runs). GitHub creates "ghost runs" on every push with 0 jobs → marked as failure.

**Goal**:
Add a `notify-failure` job to CI workflow that dispatches a `ci-failure` event when any job fails. This replaces the broken `workflow_run` trigger with explicit `repository_dispatch`.

**Success Criteria**:
- [ ] New `notify-failure` job added to ci.yml
- [ ] Job runs only when test/lint/secret-patterns fails
- [ ] Dispatches `ci-failure` event with run metadata
- [ ] CI workflow still passes when all jobs succeed

---

## Implementation Plan

### Phase 1: Add notify-failure job

**Goal**: Dispatch `ci-failure` event when CI fails

**Tasks**:
- [ ] Add `permissions: contents: write` to workflow (needed for dispatch)
- [ ] Add `notify-failure` job with `needs: [test, lint, secret-patterns]`
- [ ] Use `if: failure()` condition
- [ ] Use `peter-evans/repository-dispatch@v3` action
- [ ] Pass run metadata as `client-payload`

**Files**:
- `.github/workflows/ci.yml` - Add new job

### Implementation

Add this job to `.github/workflows/ci.yml`:

```yaml
permissions:
  contents: write

jobs:
  # ... existing jobs ...

  notify-failure:
    needs: [test, lint, secret-patterns]
    if: failure()
    runs-on: ubuntu-latest
    steps:
      - name: Dispatch CI failure event
        uses: peter-evans/repository-dispatch@v3
        with:
          event-type: ci-failure
          client-payload: '{"run_id": "${{ github.run_id }}", "run_url": "${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}", "branch": "${{ github.ref_name }}", "sha": "${{ github.sha }}"}'
```

---

## Technical Decisions

| Decision | Options Considered | Chosen | Reasoning |
|----------|-------------------|--------|-----------|
| Dispatch action | peter-evans/repository-dispatch, manual API call | peter-evans/repository-dispatch@v3 | Well-maintained, widely used, handles auth |
| Trigger condition | `if: failure()`, `if: always()` | `if: failure()` | Only dispatch when something actually fails |

---

## Verify

```bash
# 1. Push a commit that breaks tests
echo 't.Fail()' >> internal/executor/runner_test.go
git add . && git commit -m "test: intentionally break test"
git push

# 2. Watch for dispatch event
gh run list --workflow=ci.yml --limit 1

# 3. Check if ci-autofix triggered (after GH-312 is merged)
gh run list --workflow=ci-autofix.yml --limit 1

# 4. Revert
git revert HEAD --no-edit && git push
```

---

## Done

- [ ] `notify-failure` job exists in ci.yml
- [ ] Job only runs when CI fails
- [ ] Dispatch event contains run_id, run_url, branch, sha
- [ ] Existing CI behavior unchanged when passing

---

## Dependencies

**Blocks**:
- GH-319 (ci-autofix needs this event to trigger)

---

**Last Updated**: 2026-02-01
