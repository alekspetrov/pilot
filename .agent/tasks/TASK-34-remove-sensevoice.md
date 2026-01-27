# TASK-34: Remove SenseVoice/FunASR - Use Whisper API Only

**Status**: ✅ Complete
**Priority**: P1 - Blocking voice functionality
**Created**: 2026-01-27

## Context

SenseVoice/FunASR local transcription has persistent issues:
- funasr dumps logs to stdout breaking JSON parsing
- torchcodec dependency issues
- Complex Python environment management

Decision: Remove SenseVoice entirely, use Whisper API as only backend.

## Files to Modify

### DELETE (1 file)
- [ ] `internal/transcription/sensevoice.go` - entire file

### MODIFY (6 files)

#### `internal/transcription/transcriber.go`
- [ ] Remove `SenseVoiceBin` config field
- [ ] Remove `case "sensevoice":` block
- [ ] Simplify `case "auto":` to use Whisper API only
- [ ] Update `DefaultConfig()` - remove SenseVoiceBin

#### `internal/transcription/setup.go`
- [ ] Remove `FunASRInstalled` field from SetupStatus
- [ ] Remove `InstallFunASR()` function
- [ ] Remove funasr dependency check
- [ ] Update `GetInstallInstructions()` - remove local option
- [ ] Update `FormatStatusMessage()` - remove SenseVoice references

#### `internal/health/health.go`
- [ ] Remove funasr Python module check
- [ ] Simplify voice feature status (Whisper API only)
- [ ] Remove "funasr not installed" messaging

#### `cmd/pilot/setup.go`
- [ ] Remove `checkPythonModule("funasr")` check
- [ ] Remove `installSenseVoice()` function
- [ ] Update voice setup flow - Whisper API only
- [ ] Remove SenseVoice installation prompts

#### `internal/adapters/telegram/handler.go`
- [ ] Remove "pip install funasr" messages
- [ ] Remove "Install SenseVoice" button
- [ ] Update voice setup help text

#### `README.md` or docs
- [ ] Update voice transcription docs

## Implementation Order

1. Delete sensevoice.go
2. Update transcriber.go (breaks build until done)
3. Update setup.go
4. Update health.go
5. Update cmd/pilot/setup.go
6. Update telegram/handler.go
7. Update docs
8. Test with `pilot doctor` and voice message

## Acceptance Criteria

- [ ] `go build` succeeds
- [ ] `go test ./...` passes
- [ ] `pilot doctor` shows voice feature with Whisper API only
- [ ] Voice transcription works via Whisper API
- [ ] No SenseVoice/funasr references in codebase (except git history)
