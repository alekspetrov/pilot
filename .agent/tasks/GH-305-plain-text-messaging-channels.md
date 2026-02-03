# GH-305: Plain Text Mode for Messaging Channels

**Status**: 🚧 Ready for Implementation
**Created**: 2026-02-01
**Assignee**: Pilot

---

## Context

**Problem**:
Telegram, WhatsApp, and Meta messaging channels don't render Markdown well. Currently all Telegram outputs use `"Markdown"` parse mode with `escapeMarkdown()` calls, causing formatting artifacts in chat apps.

**Goal**:
Auto-detect messaging channel types and use plain text formatting (empty parse mode) while preserving Markdown for channels that support it well (Slack).

**Success Criteria**:
- [ ] Telegram messages display clean plain text without markdown artifacts
- [ ] Slack notifications still use Markdown formatting
- [ ] No breaking changes to existing behavior
- [ ] WhatsApp/Meta ready for future integration

---

## Implementation Plan

### Phase 1: Add Plain Text Mode Flag to Telegram Config

**Goal**: Enable config-based channel format detection

**Tasks**:
- [ ] Add `PlainTextMode bool` field to `internal/adapters/telegram/client.go` Config struct
- [ ] Default to `true` for Telegram (plain text by default)
- [ ] Update config loading in `internal/config/config.go`

**Files**:
- `internal/adapters/telegram/client.go:11-24` - Add config field
- `internal/config/config.go` - Update struct if needed

### Phase 2: Update Notifier to Use Conditional Parse Mode

**Goal**: Thread plain text mode through notification system

**Tasks**:
- [ ] Add `plainTextMode bool` field to `Notifier` struct
- [ ] Pass config value through `NewNotifier()` constructor
- [ ] Create helper method `getParseMode() string` that returns `""` or `"Markdown"`
- [ ] Update all `SendMessage()` calls in notifier.go to use `getParseMode()`

**Files**:
- `internal/adapters/telegram/notifier.go:28-138` - All notification methods

**Methods to update**:
| Method | Line | Current |
|--------|------|---------|
| `SendTaskStarted()` | 48-51 | `"Markdown"` |
| `SendTaskCompleted()` | 54-61 | `"Markdown"` |
| `SendTaskFailed()` | 64-68 | `"Markdown"` |
| `TaskProgress()` | 71-76 | `"Markdown"` |
| `PRReady()` | 79-83 | `"Markdown"` |
| `SendBudgetWarning()` | 100-123 | `"Markdown"` |
| `SendBudgetPaused()` | 126-130 | `"Markdown"` |
| `SendTaskBlocked()` | 133-137 | `"Markdown"` |

### Phase 3: Update Commands Handler

**Goal**: Conditional formatting in command responses

**Tasks**:
- [ ] Pass plain text mode to commands handler via Handler struct
- [ ] Update all `/help`, `/status`, `/projects`, `/cost`, `/tasks` commands
- [ ] Skip `escapeMarkdown()` calls when in plain text mode
- [ ] Remove markdown syntax (`*bold*`, `_italic_`, backticks) from output when plain text

**Files**:
- `internal/adapters/telegram/commands.go` - All command handlers
- `internal/adapters/telegram/handler.go` - Pass config to commands

### Phase 4: Update Formatter for Plain Text

**Goal**: Conditional formatting in formatter functions

**Tasks**:
- [ ] Update `FormatGreeting()` to accept plain text flag or create `FormatGreetingPlain()`
- [ ] Remove `*bold*` syntax when plain text mode
- [ ] Keep emoji (they work in all channels)
- [ ] Keep table→list conversion (improves readability)

**Files**:
- `internal/adapters/telegram/formatter.go:212-233` - FormatGreeting
- `internal/adapters/telegram/formatter.go:425-448` - escapeMarkdown (make conditional)

### Phase 5: Update Tests

**Goal**: Test both formatting modes

**Tasks**:
- [ ] Add tests for plain text mode in `notifier_test.go`
- [ ] Add tests for plain text mode in `formatter_test.go`
- [ ] Update existing tests that check for Markdown output

**Files**:
- `internal/adapters/telegram/notifier_test.go`
- `internal/adapters/telegram/formatter_test.go`
- `internal/adapters/telegram/commands_test.go`

---

## Technical Decisions

| Decision | Options Considered | Chosen | Reasoning |
|----------|-------------------|--------|-----------|
| Config location | Per-message, per-notifier, per-config | Per-config | Single source of truth, easy to change |
| Default mode | Markdown, Plain | Plain for Telegram | Messaging apps handle plain better |
| Implementation | Interface, bool flag, enum | Bool flag | KISS - only two modes needed |
| Markdown removal | Strip at send time, format differently | Format differently | Cleaner separation |

---

## Dependencies

**Requires**:
- None - self-contained change

**Blocks**:
- WhatsApp integration (can reuse pattern)
- Meta/Facebook integration (can reuse pattern)

---

## Verify

Run these commands to validate the implementation:

```bash
# Run tests
go test ./internal/adapters/telegram/... -v

# Type check
go build ./...

# Lint
make lint
```

---

## Done

Observable outcomes that prove completion:

- [ ] `PlainTextMode` config field exists and defaults to `true` for Telegram
- [ ] Telegram notifications sent with empty parse mode (no markdown)
- [ ] `/help`, `/status` commands show clean text without `*asterisks*`
- [ ] Slack notifications still use Markdown (verify alerts channel)
- [ ] All tests pass
- [ ] Build succeeds

---

## Notes

**Existing pattern**: `internal/briefs/formatter.go` already has `PlainTextFormatter` - follow same pattern.

**Key insight**: `SendMessage()` already accepts `""` for plain text - just need to thread the mode through.

**Emoji handling**: Keep all emoji - they render correctly in all messaging apps.

---

## Completion Checklist

Before marking complete:
- [ ] Implementation finished
- [ ] Tests written and passing
- [ ] No regressions in Slack formatting
- [ ] Manual test in Telegram confirmed clean output

---

**Last Updated**: 2026-02-01
