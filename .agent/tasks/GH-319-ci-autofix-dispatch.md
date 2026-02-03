# GH-319: Update ci-autofix to use repository_dispatch

**Status**: 🚧 In Progress
**Created**: 2026-02-01
**Assignee**: Pilot

---

## Context

**Problem**:
The `ci-autofix.yml` workflow uses `workflow_run` trigger which never fires. GitHub creates "ghost runs" with 0 jobs on every push, all marked as failures.

**Goal**:
Replace `workflow_run` trigger with `repository_dispatch` that receives the `ci-failure` event from CI workflow (GH-311).

**Success Criteria**:
- [ ] Workflow triggers on `repository_dispatch: types: [ci-failure]`
- [ ] Payload references updated from `workflow_run` to `client_payload`
- [ ] No more ghost runs on push events
- [ ] Creates fix issue when CI fails

---

## Implementation Plan

### Phase 1: Update trigger

**Goal**: Change from `workflow_run` to `repository_dispatch`

**Tasks**:
- [ ] Replace `on: workflow_run` with `on: repository_dispatch: types: [ci-failure]`
- [ ] Remove `check-status` job (no longer needed - trigger is explicit)
- [ ] Remove `conclusion` check (we only receive events on failure)

### Phase 2: Update payload references

**Goal**: Use `client_payload` instead of `workflow_run` context

**Tasks**:
- [ ] `github.event.workflow_run.id` → `github.event.client_payload.run_id`
- [ ] `github.event.workflow_run.html_url` → `github.event.client_payload.run_url`
- [ ] `github.event.workflow_run.head_branch` → `github.event.client_payload.branch`
- [ ] `github.event.workflow_run.head_sha` → `github.event.client_payload.sha`
- [ ] Update `context.payload.workflow_run.*` in JavaScript to `context.payload.client_payload.*`

**Files**:
- `.github/workflows/ci-autofix.yml` - Complete rewrite of trigger and payload handling

### Implementation

Replace `.github/workflows/ci-autofix.yml` with:

```yaml
name: CI Auto-Fix

on:
  repository_dispatch:
    types: [ci-failure]

jobs:
  create-fix-issue:
    runs-on: ubuntu-latest
    steps:
      - name: Check if already has fix issue
        uses: actions/github-script@v7
        id: check-existing
        with:
          script: |
            const issues = await github.rest.issues.listForRepo({
              owner: context.repo.owner,
              repo: context.repo.repo,
              labels: 'ci-fix',
              state: 'open'
            });
            const runId = context.payload.client_payload.run_id;
            const existing = issues.data.find(i => i.body.includes(`run_id: ${runId}`));
            return { exists: !!existing };

      - name: Check fix attempt count
        if: ${{ !fromJSON(steps.check-existing.outputs.result).exists }}
        id: fix-count
        uses: actions/github-script@v7
        with:
          script: |
            const issues = await github.rest.issues.listForRepo({
              owner: context.repo.owner,
              repo: context.repo.repo,
              labels: 'ci-fix',
              state: 'all'
            });
            const branch = context.payload.client_payload.branch;
            const count = issues.data.filter(i =>
              i.body?.includes(`**Branch:** ${branch}`)
            ).length;
            console.log(`Found ${count} existing ci-fix issues for branch: ${branch}`);
            if (count >= 3) {
              console.log(`Max fix attempts (3) reached for branch ${branch}. Skipping.`);
            }
            core.setOutput('count', count);
            core.setOutput('max_reached', count >= 3);

      - name: Get failed job info
        if: ${{ !fromJSON(steps.check-existing.outputs.result).exists && steps.fix-count.outputs.max_reached != 'true' }}
        uses: actions/github-script@v7
        id: get-failure
        with:
          script: |
            const payload = context.payload.client_payload;
            const jobs = await github.rest.actions.listJobsForWorkflowRun({
              owner: context.repo.owner,
              repo: context.repo.repo,
              run_id: payload.run_id
            });

            const failedJobs = jobs.data.jobs.filter(j => j.conclusion === 'failure');
            const jobNames = failedJobs.map(j => j.name).join(', ');

            return {
              runId: payload.run_id,
              runUrl: payload.run_url,
              branch: payload.branch,
              sha: payload.sha.substring(0, 7),
              jobs: jobNames
            };

      - name: Create fix issue
        if: ${{ !fromJSON(steps.check-existing.outputs.result).exists && steps.fix-count.outputs.max_reached != 'true' }}
        uses: actions/github-script@v7
        with:
          script: |
            const info = ${{ steps.get-failure.outputs.result }};

            // Don't auto-fix on pilot branches (prevent loops)
            if (info.branch.startsWith('pilot/')) {
              console.log('Skipping auto-fix for pilot branch');
              return;
            }

            await github.rest.issues.create({
              owner: context.repo.owner,
              repo: context.repo.repo,
              title: `CI Fix: ${info.jobs} failed on ${info.branch}`,
              labels: ['pilot', 'ci-fix'],
              body: `## CI Failure Auto-Fix Request

**Branch:** ${info.branch}
**Commit:** ${info.sha}
**Failed jobs:** ${info.jobs}
**Run:** ${info.runUrl}

<!-- run_id: ${info.runId} -->

---

Pilot: Please analyze the CI failure logs at the run URL above and fix the issues. Focus on:
1. Lint errors (golangci-lint)
2. Test failures
3. Build errors

Create a PR with the fix.`
            });
```

---

## Technical Decisions

| Decision | Options Considered | Chosen | Reasoning |
|----------|-------------------|--------|-----------|
| Trigger | workflow_run, repository_dispatch | repository_dispatch | workflow_run never worked; dispatch is explicit |
| Condition check | Keep conclusion check, remove it | Remove | We only dispatch on failure, no need to check |

---

## Dependencies

**Requires**:
- GH-318 (CI workflow must dispatch the event)

---

## Verify

```bash
# Manual test dispatch
gh api repos/alekspetrov/pilot/dispatches \
  -f event_type=ci-failure \
  -f 'client_payload={"run_id":"12345","run_url":"https://example.com","branch":"test","sha":"abc1234"}'

# Check workflow triggered
gh run list --workflow=ci-autofix.yml --limit 1 --json event,conclusion
# Should show: event=repository_dispatch, conclusion=success
```

---

## Done

- [ ] Trigger changed to `repository_dispatch`
- [ ] All payload references updated
- [ ] `check-status` job removed
- [ ] No ghost runs on push events
- [ ] Creates fix issue on dispatch

---

**Last Updated**: 2026-02-01
