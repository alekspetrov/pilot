# Plan: feat(briefs): catch-up delivery on startup when scheduled brief was missed

## Subtasks

### 1. Add `brief_history` table and store methods (`internal/memory/store.go`, `internal/memory/store_test.go`)

Add a new migration entry to the `migrate()` method's migration slice creating the `brief_history` table:

```sql
CREATE TABLE IF NOT EXISTS brief_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    sent_at DATETIME NOT NULL,
    schedule TEXT NOT NULL,
    channels_attempted INTEGER DEFAULT 0,
    channels_succeeded INTEGER DEFAULT 0
);
```

Add two new methods to `Store`:

- `RecordBriefSent(sentAt time.Time, schedule string, attempted, succeeded int) error` — inserts a row into `brief_history`.
- `GetLastBriefSent() (*time.Time, error)` — queries `SELECT MAX(sent_at) FROM brief_history` and returns the result, or `nil` if no rows exist.

Add table-driven tests in `internal/memory/store_test.go`:
- `TestRecordBriefSent` — insert a record, verify it can be queried back.
- `TestGetLastBriefSent` — empty table returns nil; after multiple inserts, returns the most recent.
- `TestGetLastBriefSentEmpty` — verifies nil return on empty table without error.

**Verification:** `go test ./internal/memory/ -run TestBrief -v`

---

### 2. Add `store` field and `maybeCatchUp` method to Scheduler (`internal/briefs/scheduler.go`)

Modify the `Scheduler` struct to add a `store *memory.Store` field.

Update `NewScheduler` signature from:
```go
func NewScheduler(generator *Generator, delivery *DeliveryService, config *BriefConfig, logger *slog.Logger) *Scheduler
```
to:
```go
func NewScheduler(generator *Generator, delivery *DeliveryService, config *BriefConfig, store *memory.Store, logger *slog.Logger) *Scheduler
```

The `store` parameter can be `nil` for graceful degradation (existing behavior, no catch-up).

Add a private method `maybeCatchUp(ctx context.Context)`:
- If `s.store == nil`, return immediately (graceful skip).
- Call `s.store.GetLastBriefSent()` to get last sent time.
- Parse the cron schedule with `cron.ParseStandard(s.config.Schedule)`.
- Determine if catch-up is needed: `lastSent == nil` OR `schedule.Next(*lastSent).Before(now)`.
- If yes, log `"catching up missed brief"` and call `s.runBrief(ctx)`.
- If no, log `"brief scheduler started, last brief sent Xh ago — no catch-up needed"`.

Modify `Start()` to call `s.maybeCatchUp(ctx)` after `s.cron.Start()`.

Modify `runBriefWithResults()` to call `s.store.RecordBriefSent(time.Now(), s.config.Schedule, attempted, succeeded)` after successful delivery (only when store is non-nil). Count attempted/succeeded from the `[]DeliveryResult` slice.

**Verification:** `go build ./internal/briefs/`

---

### 3. Add scheduler catch-up tests (`internal/briefs/scheduler_test.go`)

Add table-driven tests for the catch-up logic:

- **TestMaybeCatchUp_MissedBrief** — Set up store with a `last_sent` time that's >1 interval ago (e.g., 25 hours ago for a daily schedule). Start scheduler, verify `runBrief` is invoked (brief is generated and delivery attempted), and verify `RecordBriefSent` is called.

- **TestMaybeCatchUp_CurrentBrief** — Set up store with a `last_sent` time within the current interval (e.g., 2 hours ago for a daily schedule). Start scheduler, verify no catch-up delivery happens.

- **TestMaybeCatchUp_NilStore** — Create scheduler with `nil` store. Start scheduler, verify no panic, no catch-up attempt, scheduler still runs normally.

- **TestMaybeCatchUp_NeverSent** — Set up store with empty `brief_history`. Start scheduler, verify catch-up fires (treats never-sent as missed).

- **TestRunBrief_RecordsBriefSent** — Verify that after a successful `RunNow()`, a record is written to `brief_history`.

Follow the existing test pattern: use `setupSchedulerTestStore` helper, table-driven where appropriate.

**Verification:** `go test ./internal/briefs/ -run TestMaybeCatchUp -v` and `go test ./internal/briefs/ -run TestRunBrief_Records -v`

---

### 4. Wire store into scheduler in `cmd/pilot/main.go` and fix all call sites

Update the `NewScheduler` call in `cmd/pilot/main.go` (~line 2109) from:
```go
briefScheduler = briefs.NewScheduler(generator, delivery, briefsConfig, slog.Default())
```
to:
```go
briefScheduler = briefs.NewScheduler(generator, delivery, briefsConfig, store, slog.Default())
```

The `store` variable is already available in scope at this point (confirmed from the wiring code at line 2091+).

Search for any other `NewScheduler` call sites (tests, other commands) and update them too — passing `nil` for store where no store is available, to maintain backward compatibility.

**Verification:** `go build ./cmd/pilot/` and `make test`

---

### 5. Run full validation (`make test`, `make lint`)

Run the complete test suite and linter to ensure:
- All new tests pass
- All existing tests pass (no regressions from signature change)
- No lint warnings on new code
- No secret patterns in test files (`make check-secrets`)

Fix any issues discovered.

**Verification:** `make test && make lint`
