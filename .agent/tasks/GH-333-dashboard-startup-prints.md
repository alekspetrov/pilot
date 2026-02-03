# GH-333: Fix Dashboard Startup Print Corruption

**Status**: 🚧 In Progress
**Created**: 2026-02-02
**Issue**: https://github.com/alekspetrov/pilot/issues/333

---

## Context

**Problem**:
Running `pilot start --dashboard --telegram --github` shows startup logs instead of TUI dashboard. The terminal is corrupted by `fmt.Print*` calls that execute BEFORE `program.Run()`.

**Root Cause**:
PR #338 only fixes prints during task processing (inside handlers), but NOT startup prints that occur before the dashboard initializes.

**Goal**:
Guard all startup `fmt.Print*` calls with `if !dashboardMode` so the TUI can take over cleanly.

---

## Implementation Plan

### Phase 1: Guard Startup Prints

**Goal**: Prevent all stdout writes before dashboard starts

**Files**:
- `cmd/pilot/main.go`

**Tasks**:
- [ ] Line 683: Guard `banner.StartupTelegram(...)` with `if !dashboardMode`
- [ ] Line 878: Guard `fmt.Printf("⚠️  GitHub polling disabled...")`
- [ ] Line 884: Guard `fmt.Printf("🐙 GitHub polling enabled...")`
- [ ] Line 886: Guard `fmt.Printf("   ⏳ Sequential mode...")`
- [ ] Line 900: Guard `fmt.Printf("🤖 Autopilot enabled...")`
- [ ] Lines 916-919: Guard stale label cleanup prints
- [ ] Line 930: Guard `fmt.Println("📱 Telegram polling started")`

**Code Pattern**:
```go
// Before
banner.StartupTelegram(version, projectPath, cfg.Adapters.Telegram.ChatID, cfg)

// After
if !dashboardMode {
    banner.StartupTelegram(version, projectPath, cfg.Adapters.Telegram.ChatID, cfg)
}
```

---

## Technical Decisions

| Decision | Options | Chosen | Reasoning |
|----------|---------|--------|-----------|
| Guard approach | Single wrapper vs individual guards | Individual guards | More explicit, easier to maintain |
| Dashboard mode check | Pass param vs global | Parameter already exists | `dashboardMode` is already a param in `runPollingMode()` |

---

## Verify

```bash
# Test dashboard mode - should show TUI immediately, no text output
pilot start --autopilot=prod --telegram --github --dashboard -p ~/Projects/startups/pilot

# Test non-dashboard mode - should show banner and status lines
pilot start --autopilot=prod --telegram --github -p ~/Projects/startups/pilot

# Run tests
make test
```

---

## Done

- [ ] All 7 print locations guarded with `if !dashboardMode`
- [ ] Dashboard shows immediately without text corruption
- [ ] Non-dashboard mode still shows startup banner/status
- [ ] Tests pass

---

**Last Updated**: 2026-02-02
