# Plan: Wire Navigator Port Scaffolding into Execution Pipeline (GH-1026)

## Subtasks

### 1. **Wire knowledge store and profile manager initialization in `NewRunnerWithConfig`** — Bootstrap the three dead-code components so every Runner instance gets them automatically

Add initialization of `KnowledgeStore`, `ProfileManager`, and `DriftDetector` inside `NewRunnerWithConfig()` in `internal/executor/runner.go` (after retrier init, lines 372-374). This follows the existing pattern where `intentJudge` and `retrier` are created inline during runner construction rather than requiring external callers to wire them.

**Approach:** `NewRunnerWithConfig` doesn't have a `*sql.DB`, so the knowledge store must be wired externally via the setter from `main.go`. However, `ProfileManager` and `DriftDetector` can be created inside `NewRunnerWithConfig` using config paths (profile) and defaults (drift detector with threshold=3). For knowledge store, add wiring at the two primary runner creation sites in `cmd/pilot/main.go` (the `start` command ~line 1236 and gateway mode ~line 460) where `memory.Store` is already available via `store.DB()`.

**Changes:**
- `internal/executor/runner.go` lines 371-376: After retrier init, create `DriftDetector` (default threshold 3, nil profile until profile manager is set) and `ProfileManager` (using config paths if available from `BackendConfig`).
- `cmd/pilot/main.go` ~line 1253 (after runner creation in start command): Wire `knowledgeStore` via `runner.SetKnowledgeStore(memory.NewKnowledgeStore(store.DB()))` with `InitSchema()` call, following the existing pattern used by `autopilot.NewStateStore(store.DB())`.
- `cmd/pilot/main.go` ~line 508 (gateway mode): Same knowledge store wiring using `gwStore.DB()`.

**Verify:** `go build ./...` passes, setters are called, nil-check pattern preserved.

---

### 2. **Inject user preferences and knowledge memories into `BuildPrompt()`** — Make the prompt context-aware using profile and knowledge store data

Add two new sections to `BuildPrompt()` in `internal/executor/runner.go` between the workflow instructions block (line ~2451) and the pre-commit verification section (line ~2454). Both use nil-checks so they're no-ops when components aren't initialized.

**Changes in `BuildPrompt()` (runner.go ~line 2452):**
- Profile injection: If `r.profileManager != nil`, call `r.profileManager.Load()`. On success, emit `## User Preferences\n\n` section with verbosity, code patterns, and frameworks.
- Knowledge injection: If `r.knowledge != nil`, call `r.knowledge.QueryByTopic(task.Title, task.ProjectPath)`. On success with results, emit `## Relevant Knowledge\n\n` section with up to 5 memories formatted as `- [type] content`.

**Verify:** Write a unit test that creates a Runner with a populated knowledge store and profile, calls `BuildPrompt()`, and asserts the output contains "User Preferences" and "Relevant Knowledge" sections. Also verify nil components produce no output (existing behavior preserved).

---

### 3. **Wire post-task archival, markers, and learning capture in `executeWithOptions()`** — Close the loop: archive docs, create markers, store memories after task completion

Add post-task wiring in `executeWithOptions()` in `internal/executor/runner.go` after the webhook dispatch block (~line 2081), before the method returns the result.

**Changes:**
- **Archive task doc:** Check for `.agent` directory in `executionPath`. If present, call `ArchiveTaskDoc(agentPath, task.ID)`. Log warning on failure, never block.
- **Create context marker:** If `.agent` exists, create a `ContextMarker{Description, TaskID, CurrentFocus}` and call `CreateMarker(agentPath, marker)`. Log warning on failure.
- **Capture learning:** If `r.knowledge != nil && result.Success`, create a `memory.Memory{Type: MemoryTypeDecision, Content, Context, ProjectID, Confidence: 0.8}` and call `r.knowledge.AddMemory()`. Log warning on failure.

**Verify:** Unit test that mocks the agent path and knowledge store, runs through the post-task path, and asserts `ArchiveTaskDoc` was attempted, marker file was created, and memory was stored.

---

### 4. **Wire drift detector recording on retry path** — Connect `RecordCorrection()` so `ShouldReanchor()` (already in BuildPrompt) can actually trigger

Add `RecordCorrection` call in the retry/error handling path of `executeWithOptions()` in `internal/executor/runner.go` (~lines 1223-1329), specifically when a retry decision is made.

**Changes:**
- After `r.retrier.Evaluate()` returns `decision.ShouldRetry == true` (~line 1227), add:
  ```go
  if r.driftDetector != nil {
      r.driftDetector.RecordCorrection("execution_retry", fmt.Sprintf("Task %s failed attempt %d, retrying", task.ID, state.smartRetryAttempt))
  }
  ```
- This ensures that repeated failures accumulate drift indicators, and the existing `ShouldReanchor()` check in `BuildPrompt` (line 2535) fires after the threshold is crossed.

**Verify:** Unit test that creates a DriftDetector with threshold=2, records 3 corrections, and asserts `ShouldReanchor()` returns true. Integration-level: confirm the retry path in `executeWithOptions` calls `RecordCorrection`.

---

### 5. **Add integration tests and verify acceptance criteria** — Ensure all wiring works end-to-end and existing tests still pass

Run the full test suite and add targeted integration tests for the new wiring.

**Changes:**
- Add `TestBuildPrompt_WithKnowledgeAndProfile` in `internal/executor/runner_test.go` — populates knowledge store and profile, verifies prompt sections appear.
- Add `TestExecuteWithOptions_PostTaskWiring` in `internal/executor/runner_test.go` — verifies archive, marker, and learning capture after successful execution (using temp `.agent` dir and in-memory SQLite).
- Add `TestDriftDetector_RetryIntegration` in `internal/executor/diagnose_test.go` — verifies `RecordCorrection` accumulates and `ShouldReanchor` triggers.
- Run `go test ./internal/executor/...` and `go test ./internal/memory/...` — all pass.
- Run `go build ./...` — clean build.

**Acceptance criteria checklist:**
- [ ] `go build ./...` passes
- [ ] `go test ./internal/executor/...` passes
- [ ] `go test ./internal/memory/...` passes
- [ ] BuildPrompt includes "Relevant Knowledge" section when knowledge store has entries
- [ ] BuildPrompt includes "User Preferences" section when profile exists
- [ ] Task doc archived to `.agent/tasks/archive/` after completion
- [ ] Context marker created in `.agent/.context-markers/` after completion
- [ ] Memory stored in SQLite after successful task
- [ ] DriftDetector.RecordCorrection called on retry path
- [ ] All components initialized via existing setters
