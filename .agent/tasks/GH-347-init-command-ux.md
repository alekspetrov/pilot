# GH-347: Improve `pilot init` UX When Config Exists

**Status**: 🚧 In Progress
**Created**: 2026-02-02
**Assignee**: Pilot

---

## Context

**Problem**:
When running `pilot init` with existing config, the output is unhelpful:
```
Config already exists at /Users/aleks.petrov/.pilot/config.yaml
Edit it manually or delete to reinitialize.
```

User doesn't know:
- What's currently configured
- How to edit (what editor command)
- What to do next
- How to force reinitialize

**Goal**:
Improve UX to match the fresh init path which shows banner, next steps, etc.

**Success Criteria**:
- [ ] Show config summary (projects count, integrations enabled)
- [ ] Add `--force` flag to reinitialize
- [ ] Show helpful next steps (edit command, start command)
- [ ] Consistent UX between fresh and existing config paths

---

## Implementation Plan

### Phase 1: Add --force Flag

**Goal**: Allow users to reinitialize config

**Tasks**:
- [ ] Add `--force` bool flag to init command
- [ ] When force=true and config exists, backup old config and create new

**Files**:
- `cmd/pilot/main.go` - Add flag and logic

### Phase 2: Show Config Summary

**Goal**: Display what's currently configured

**Tasks**:
- [ ] Load existing config when it exists
- [ ] Display summary: project count, enabled integrations
- [ ] Handle config load errors gracefully

**Files**:
- `cmd/pilot/main.go` - Add config loading and display

### Phase 3: Improve Output

**Goal**: Show helpful next steps

**Tasks**:
- [ ] Show config path with edit suggestion
- [ ] Show `pilot init --force` option
- [ ] Show `pilot start --help` for next steps
- [ ] Use consistent formatting (emoji, indentation)

**Example output**:
```
⚠️  Config already exists: ~/.pilot/config.yaml

   Current settings:
   • Projects: 2 configured
   • Telegram: enabled
   • GitHub: enabled

   Options:
   • Edit:   $EDITOR ~/.pilot/config.yaml
   • Reset:  pilot init --force
   • Start:  pilot start --help
```

---

## Technical Decisions

| Decision | Options Considered | Chosen | Reasoning |
|----------|-------------------|--------|-----------|
| Backup on force | No backup, backup to .bak, backup with timestamp | .bak suffix | Simple, one backup is enough |
| Config load error | Silent, warn, error | Warn and continue | Don't block on corrupt config |

---

## Verify

```bash
# Test existing config path
pilot init

# Test force reinit
pilot init --force

# Verify backup created
ls ~/.pilot/config.yaml.bak
```

---

## Done

- [ ] `pilot init` shows config summary when config exists
- [ ] `--force` flag creates new config (with backup)
- [ ] Output includes helpful next steps
- [ ] Tests pass

---

**Last Updated**: 2026-02-02
