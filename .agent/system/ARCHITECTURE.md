# Pilot Architecture

**Last Updated:** 2026-02-13 (GH-950 worktree + epic docs)

## System Overview

Pilot is a Go-based autonomous AI development pipeline that:
- Receives tickets from Telegram, GitHub Issues, Linear, or Jira
- Plans and executes implementation using Claude Code
- Creates branches, commits, and PRs
- Sends notifications via Slack/Telegram

```
                     ┌─────────────────────────────────────────┐
                     │              CLI (cmd/pilot)            │
                     │  start | task | telegram | github | ... │
                     └─────────────────┬───────────────────────┘
                                       │
                     ┌─────────────────▼───────────────────────┐
                     │           internal/pilot                │
                     │  Orchestration + Component Coordination │
                     └──────────┬─────────────────┬────────────┘
                                │                 │
         ┌──────────────────────┼─────────────────┼───────────────────────┐
         │                      │                 │                       │
┌────────▼────────┐  ┌──────────▼──────────┐  ┌──▼─────────────┐  ┌──────▼──────┐
│    Adapters     │  │     Executor        │  │    Memory      │  │   Gateway   │
│ telegram/github │  │ Claude Code Runner  │  │ SQLite + Graph │  │ HTTP + WS   │
│ linear/jira/    │  │ Progress Display    │  │ Patterns Store │  │ Webhooks    │
│ slack           │  │ Git Operations      │  └────────────────┘  └─────────────┘
└─────────────────┘  │ Quality Gates       │
                     │ Alerts Integration  │
                     └─────────────────────┘
```

## Data Flow

### Task Execution Flow (Primary)

```
                 User Message                    GitHub Issue
                      │                              │
                      ▼                              ▼
               ┌────────────┐               ┌──────────────┐
               │  Telegram  │               │GitHub Poller │
               │  Handler   │               │(30s interval)│
               └─────┬──────┘               └──────┬───────┘
                     │                              │
                     └──────────────┬───────────────┘
                                    │
                                    ▼
                          ┌─────────────────┐
                          │   Dispatcher    │  ← GH-46 task queue
                          │ (per-project    │    coordinates concurrent
                          │  serialization) │    requests
                          └────────┬────────┘
                                   │
                                   ▼
                          ┌─────────────────┐
                          │    Executor     │
                          │  runner.Execute │
                          └────────┬────────┘
                                   │
                    ┌──────────────┼──────────────┐
                    │              │              │
                    ▼              ▼              ▼
            ┌─────────────┐ ┌─────────────┐ ┌─────────────┐
            │ Git Ops     │ │Claude Code  │ │Progress     │
            │ (checkout,  │ │(subprocess  │ │Display      │
            │ branch, PR) │ │ stream-json)│ │(lipgloss)   │
            └─────────────┘ └─────────────┘ └─────────────┘
```

### Command → Package Mapping

| CLI Command | Primary Package(s) | Description |
|-------------|-------------------|-------------|
| `pilot start` | pilot, gateway, dashboard | Start daemon with optional TUI |
| `pilot task` | executor, alerts, quality | Execute single task |
| `pilot telegram` | adapters/telegram, executor | Telegram bot mode |
| `pilot github run` | adapters/github, executor | Execute GitHub issue |
| `pilot brief` | briefs, memory, adapters/slack | Generate daily reports |
| `pilot patterns` | memory | Query cross-project patterns |
| `pilot replay` | replay | Debug execution recordings |
| `pilot status` | config | Show configuration status |
| `pilot upgrade` | upgrade | Self-update binary |
| `pilot budget` | budget | View cost controls |
| `pilot team` | teams | Manage team permissions |
| `pilot tunnel` | tunnel | Cloudflare tunnel management |
| `pilot webhooks` | webhooks | Outbound webhook config |
| `pilot doctor` | health, config | System health check |

## Package Architecture

### Core Packages (Wired in main.go)

| Package | Purpose | Key Files |
|---------|---------|-----------|
| `pilot` | Top-level orchestration | `pilot.go` |
| `executor` | Claude Code process management | `runner.go`, `git.go`, `progress.go`, `dispatcher.go` |
| `config` | YAML configuration loading | `config.go`, `schema.go` |
| `memory` | SQLite + knowledge graph | `store.go`, `graph.go`, `patterns.go` |
| `logging` | Structured slog logging | `logger.go` |
| `alerts` | Event-based alerting | `engine.go`, `dispatcher.go`, `channels.go` |
| `quality` | Quality gates (test/lint) | `executor.go`, `gates.go` |
| `dashboard` | Bubbletea TUI | `tui.go` |
| `banner` | CLI visual branding | `banner.go` |
| `briefs` | Daily/weekly summaries | `generator.go`, `formatter.go` |
| `replay` | Execution recording viewer | `player.go`, `viewer.go`, `analyzer.go` |
| `upgrade` | Self-update mechanism | `upgrader.go` |

### Adapter Packages

| Package | Purpose | Status |
|---------|---------|--------|
| `adapters/telegram` | Telegram bot + voice | Fully wired |
| `adapters/github` | GitHub Issues + PR ops | Fully wired |
| `adapters/slack` | Notifications | Fully wired |
| `adapters/linear` | Linear webhooks | Implemented, not in main CLI |
| `adapters/jira` | Jira webhooks | Implemented, not in main CLI |

### Supporting Packages

| Package | Purpose | Status |
|---------|---------|--------|
| `gateway` | HTTP + WebSocket server | Implemented, used by pilot.Start() |
| `orchestrator` | Python bridge for LLM | Implemented, used internally |
| `approval` | Human-in-the-loop | Implemented, optional feature |
| `budget` | Cost controls | Implemented, CLI command exists |
| `teams` | Team permissions | Implemented, CLI command exists |
| `tunnel` | Cloudflare tunnel | Implemented, CLI command exists |
| `webhooks` | Outbound webhooks | Implemented, CLI command exists |
| `health` | Health checks | Implemented, used by doctor |
| `testutil` | Test utilities | Test-only, not runtime |
| `transcription` | Voice → text (OpenAI) | Used by telegram adapter |

## Key Integration Points

### Claude Code Integration

```go
// internal/executor/runner.go
cmd := exec.Command("claude",
    "-p", prompt,
    "--verbose",
    "--output-format", "stream-json",
    "--dangerously-skip-permissions",
)
```

Output parsing via `stream-json` events:
- `system` → initialization
- `assistant` → text responses
- `tool_use` → tool calls (Read, Write, Bash, etc.)
- `tool_result` → tool outputs
- `result` → final outcome

### Navigator Integration

```go
// internal/executor/runner.go:BuildPrompt()
if _, err := os.Stat(filepath.Join(task.ProjectPath, ".agent")); err == nil {
    sb.WriteString("Start my Navigator session.\n\n")  // CRITICAL
}
```

**DO NOT REMOVE** - This is Pilot's core value proposition.

### Progress Detection

```go
// internal/executor/runner.go - phase detection
switch {
case strings.Contains(text, "Navigator Session Started"):
    phase = "navigator"
case strings.Contains(text, "TASK MODE ACTIVATED"):
    phase = "task-mode"
case strings.Contains(text, "PHASE: → RESEARCH"):
    phase = "research"
case strings.Contains(text, "PHASE: → IMPL"):
    phase = "implementing"
// ...
}
```

### Alerts Bridge

```go
// internal/executor/alerts.go
type AlertEventProcessor interface {
    ProcessEvent(event alerts.Event)
}

// Used in runner.Execute() to emit:
// - EventTypeTaskStarted
// - EventTypeTaskProgress
// - EventTypeTaskCompleted
// - EventTypeTaskFailed
```

## Configuration Structure

```yaml
# ~/.pilot/config.yaml
gateway:
  host: "127.0.0.1"
  port: 9090

adapters:
  telegram:
    enabled: true
    bot_token: "..."
  github:
    enabled: true
    repo: "owner/repo"
    polling:
      enabled: true
      interval: 30s
      label: "pilot"
  slack:
    enabled: true
    bot_token: "..."

projects:
  - name: "my-project"
    path: "/path/to/project"
    navigator: true

memory:
  path: "~/.pilot/memory.db"

alerts:
  enabled: true
  channels: [...]
  rules: [...]

quality:
  enabled: true
  gates: [...]
```

## Database Schema (SQLite)

```sql
-- Task executions
CREATE TABLE executions (
    id TEXT PRIMARY KEY,
    task_id TEXT,
    project_path TEXT,
    status TEXT,
    started_at DATETIME,
    completed_at DATETIME,
    duration_ms INTEGER,
    output TEXT,
    error TEXT,
    commit_sha TEXT,
    pr_url TEXT
);

-- Cross-project patterns
CREATE TABLE cross_patterns (
    id TEXT PRIMARY KEY,
    title TEXT,
    description TEXT,
    type TEXT,
    scope TEXT,
    confidence REAL,
    occurrences INTEGER,
    is_anti_pattern BOOLEAN
);

-- Task queue (GH-46)
CREATE TABLE task_queue (
    id TEXT PRIMARY KEY,
    project_path TEXT,
    task_json TEXT,
    status TEXT,
    created_at DATETIME,
    started_at DATETIME,
    completed_at DATETIME
);
```

## Test Coverage

| Package | Test Files | Status |
|---------|-----------|--------|
| adapters/github | 5 | ✅ Pass |
| adapters/slack | 2 | ✅ Pass |
| adapters/telegram | 7 | ✅ Pass |
| adapters/jira | 3 | ✅ Pass |
| adapters/linear | 3 | ✅ Pass |
| alerts | 4 | ✅ Pass |
| approval | 2 | ✅ Pass |
| briefs | 4 | ✅ Pass |
| budget | 2 | ✅ Pass |
| config | 1 | ✅ Pass |
| executor | 15 | ✅ Pass |
| gateway | 4 | ✅ Pass |
| logging | 2 | ✅ Pass |
| memory | 8 | ✅ Pass |
| orchestrator | 1 | ✅ Pass |
| quality | 3 | ✅ Pass |
| replay | 4 | ✅ Pass |
| teams | 1 | ✅ Pass |
| tunnel | 6 | ✅ Pass |
| upgrade | 1 | ✅ Pass |
| webhooks | 1 | ✅ Pass |

**Packages without tests:** banner, dashboard, health, pilot, testutil, transcription

## Build & Deploy

```bash
# Build
make build    # → ./bin/pilot

# Test
make test     # go test ./...

# Lint
make lint     # golangci-lint

# Development
make dev      # Build + run with hot reload
```

Binary versioning: `v0.3.0-{commits}-g{sha}`

## Worktree Isolation (GH-936)

Pilot uses git worktrees to provide isolated execution environments, allowing tasks to run even when the user has uncommitted changes in their working directory.

### Architecture Overview

```
                    Original Repository               Temporary Worktrees
                    ┌─────────────────┐               ┌─────────────────┐
                    │ /project/repo   │               │ /tmp/pilot-     │
                    │                 │               │ worktree-GH-1   │
User Working Dir ──►│ .git/           │◄── shared ──►│ .git (file)     │
(may have changes)  │ src/            │   git data   │ src/            │
                    │ .agent/         │               │ .agent/ (copy)  │
                    └─────────────────┘               └─────────────────┘
                                                      ┌─────────────────┐
                                                      │ /tmp/pilot-     │
                                                      │ worktree-GH-2   │
                    Concurrent epics can ────────────►│ .git (file)     │
                    run in parallel worktrees         │ src/            │
                                                      └─────────────────┘
```

### Key Components

| Component | File | Purpose |
|-----------|------|---------|
| `WorktreeManager` | `worktree.go` | Creates/tracks/cleans worktrees |
| `CreateWorktreeWithBranch()` | `worktree.go` | Creates worktree with proper branch |
| `EnsureNavigatorInWorktree()` | `worktree.go` | Copies .agent/ to worktree |
| `CleanupOrphanedWorktrees()` | `worktree.go` | Startup cleanup for crash recovery |

### Worktree + Epic Interaction

When processing epic tasks with worktree mode enabled:

```
Epic Task (GH-EPIC-100)
        │
        ▼
┌───────────────────────────────────────────────────────────────┐
│ 1. executeWithOptions(task, allowWorktree=true)               │
│    └── CreateWorktreeWithBranch(ctx, repo, id, branch, base)  │
│        └── Creates /tmp/pilot-worktree-GH-EPIC-100-{ts}       │
│                                                               │
│ 2. EnsureNavigatorInWorktree(original, worktree)              │
│    └── Copies .agent/ including untracked content             │
│                                                               │
│ 3. Epic Planning Phase (in worktree)                          │
│    └── PlanEpic(ctx, task, executionPath)                     │
│        └── Claude runs in worktree, reads .agent/             │
│                                                               │
│ 4. Sub-Issue Execution (in worktree)                          │
│    └── ExecuteSubIssues(ctx, parent, issues, executionPath)   │
│        └── Each sub-issue uses executeWithOptions(allowWorktree=false)
│            └── No recursive worktree creation!                │
│                                                               │
│ 5. Cleanup (deferred)                                         │
│    └── worktreeResult.Cleanup() removes worktree + branch     │
└───────────────────────────────────────────────────────────────┘
```

**Critical**: Sub-issues call `executeWithOptions(ctx, task, false)` to prevent recursive worktree creation. The parent epic's worktree is reused for all sub-issues.

### Navigator Auto-Init in Worktrees

Navigator initialization respects the execution path:

```go
// maybeInitNavigator checks executionPath, not task.ProjectPath
if r.config.Navigator.AutoInit {
    r.maybeInitNavigator(executionPath)  // Uses worktree path
}
```

This ensures:
1. Navigator init command runs in the worktree
2. Created .agent/ structure stays in the worktree
3. Original repo remains unmodified

### Quality Gates in Worktree Context

Quality gates (`quality.DetectBuildCommand`, `quality.RunGates`) receive the worktree path:

```go
qualityChecker := qualityCheckerFactory(task.ID, executionPath)
// Build detection looks for go.mod, package.json, etc. in executionPath
// Tests run in executionPath directory
```

The quality package uses the provided path for:
- Project type detection (Go, Node, Rust, etc.)
- Build command execution
- Test command execution
- Lint command execution

### Concurrent Epic Execution

Multiple epics can execute concurrently in separate worktrees:

```
┌─────────────────┐   ┌─────────────────┐   ┌─────────────────┐
│ GH-EPIC-100     │   │ GH-EPIC-200     │   │ GH-EPIC-300     │
│ (worktree-100)  │   │ (worktree-200)  │   │ (worktree-300)  │
└────────┬────────┘   └────────┬────────┘   └────────┬────────┘
         │                     │                     │
         ▼                     ▼                     ▼
    sub-issue 1           sub-issue 1           sub-issue 1
    sub-issue 2           sub-issue 2           sub-issue 2
         │                     │                     │
         ▼                     ▼                     ▼
    all cleanup           all cleanup           all cleanup
```

Each worktree has:
- Unique branch (`pilot/GH-EPIC-xxx`)
- Isolated Navigator copy
- Independent git operations
- Separate cleanup lifecycle

### Edge Cases

1. **Navigator untracked content**: `.agent/.context-markers/` and other gitignored content is copied via `CopyNavigatorToWorktree()`.

2. **Crash recovery**: `CleanupOrphanedWorktrees()` scans `/tmp/pilot-worktree-*` at startup to remove stale directories.

3. **Branch conflicts**: `CreateWorktreeWithBranch()` fails if branch exists; caller handles retry with different name.

4. **Remote access**: `VerifyRemoteAccess()` validates worktree can reach origin before long operations.

### Configuration

```yaml
# ~/.pilot/config.yaml
executor:
  use_worktree: true  # Enable worktree isolation (default: false)

navigator:
  auto_init: true     # Auto-init Navigator in worktrees without .agent/
```

### Test Coverage

| Test File | Coverage |
|-----------|----------|
| `worktree_test.go` | Basic worktree lifecycle |
| `worktree_epic_test.go` | Epic + worktree integration |
| `worktree_path_integration_test.go` | Path handling, quality gates, Navigator copy |

---

## Security Considerations

1. **Tokens in tests**: Use `internal/testutil/tokens.go` for fake tokens
2. **API keys**: Environment variables or config file
3. **Sandbox mode**: Claude Code runs with `--dangerously-skip-permissions` (trusted context)
4. **Webhook secrets**: HMAC validation for incoming webhooks

---

## Appendix: Full Package Audit (GH-52)

**Audit Date:** 2026-01-28

| Package | Exists | Imported | Wired in main.go | Has Tests | Tests Pass |
|---------|--------|----------|------------------|-----------|------------|
| adapters/github | ✅ | ✅ | ✅ | ✅ | ✅ |
| adapters/jira | ✅ | ✅ | ❌ | ✅ | ✅ |
| adapters/linear | ✅ | ✅ | ❌ | ✅ | ✅ |
| adapters/slack | ✅ | ✅ | ✅ | ✅ | ✅ |
| adapters/telegram | ✅ | ✅ | ✅ | ✅ | ✅ |
| alerts | ✅ | ✅ | ✅ | ✅ | ✅ |
| approval | ✅ | ✅ | ❌ | ✅ | ✅ |
| banner | ✅ | ✅ | ✅ | ❌ | N/A |
| briefs | ✅ | ✅ | ✅ | ✅ | ✅ |
| budget | ✅ | ✅ | ❌ | ✅ | ✅ |
| config | ✅ | ✅ | ✅ | ✅ | ✅ |
| dashboard | ✅ | ✅ | ✅ | ❌ | N/A |
| executor | ✅ | ✅ | ✅ | ✅ | ✅ |
| gateway | ✅ | ✅ | ❌ | ✅ | ✅ |
| health | ✅ | ✅ | ❌ | ❌ | N/A |
| logging | ✅ | ✅ | ✅ | ✅ | ✅ |
| memory | ✅ | ✅ | ✅ | ✅ | ✅ |
| orchestrator | ✅ | ✅ | ❌ | ✅ | ✅ |
| pilot | ✅ | ✅ | ✅ | ❌ | N/A |
| quality | ✅ | ✅ | ✅ | ✅ | ✅ |
| replay | ✅ | ✅ | ✅ | ✅ | ✅ |
| teams | ✅ | ✅ | ❌ | ✅ | ✅ |
| testutil | ✅ | ✅ | ❌ | ❌ | N/A |
| transcription | ✅ | ✅ | ❌ | ❌ | N/A |
| tunnel | ✅ | ✅ | ❌ | ✅ | ✅ |
| upgrade | ✅ | ✅ | ✅ | ✅ | ✅ |
| webhooks | ✅ | ✅ | ❌ | ✅ | ✅ |

**Summary:**
- 27 packages total (24 + 5 adapter subpackages - 2 counted above)
- 100% exist and are imported somewhere
- 52% wired directly in main.go
- 79% have test files
- 100% of tested packages pass
