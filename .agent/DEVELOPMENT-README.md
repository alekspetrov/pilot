# Pilot Development Navigator

**AI that ships your tickets.**

## Quick Navigation

| Document | When to Read |
|----------|--------------|
| CLAUDE.md | Every session (auto-loaded) |
| This file | Every session (navigator index) |
| `.agent/tasks/TASK-XX.md` | When working on specific task |
| `.agent/system/ARCHITECTURE.md` | When modifying core components |

## Current State

### Implementation Status

| Component | Status | Location |
|-----------|--------|----------|
| Gateway (WebSocket + HTTP) | ✅ Complete | `internal/gateway/` |
| Linear Adapter | ✅ Complete | `internal/adapters/linear/` |
| Slack Adapter | ✅ Complete | `internal/adapters/slack/` |
| Executor (Claude Code) | ✅ Complete | `internal/executor/` |
| Memory (SQLite) | ✅ Complete | `internal/memory/` |
| Config System | ✅ Complete | `internal/config/` |
| TUI Dashboard | ✅ Complete | `internal/dashboard/` |
| Orchestrator (Python) | ✅ Complete | `orchestrator/` |
| CLI Commands | ⚠️ Basic | `cmd/pilot/` |

### Week 1-2 Progress ✅

- [x] Go project setup
- [x] Gateway skeleton (WebSocket + HTTP)
- [x] Config system (YAML parsing)
- [x] Linear adapter (webhook receiver)
- [x] Basic CLI (`pilot start`, `pilot status`)

### Week 3-4 Progress ✅

- [x] Wire orchestrator to gateway
- [x] Ticket → Navigator task conversion
- [x] Python bridge for LLM planning
- [x] Go ↔ Python IPC (subprocess)
- [x] Pilot core integration
- [x] Tests (24 passing)

### Next Steps (Week 5-6)

- [ ] End-to-end testing with real Linear webhook
- [ ] TUI dashboard integration
- [ ] Git operations in executor
- [ ] PR creation workflow

## Project Structure

```
pilot/
├── cmd/pilot/           # CLI entrypoint
├── internal/
│   ├── gateway/         # WebSocket + HTTP server
│   ├── adapters/        # Linear, Slack integrations
│   ├── executor/        # Claude Code process management
│   ├── memory/          # SQLite + knowledge graph
│   ├── config/          # Configuration loading
│   └── dashboard/       # Terminal UI
├── orchestrator/        # Python LLM logic
├── configs/             # Example configs
└── .agent/              # Navigator docs
```

## Key Files

### Gateway
- `internal/gateway/server.go` - Main server with WebSocket + HTTP
- `internal/gateway/router.go` - Message and webhook routing
- `internal/gateway/sessions.go` - WebSocket session management
- `internal/gateway/auth.go` - Authentication handling

### Adapters
- `internal/adapters/linear/client.go` - Linear GraphQL client
- `internal/adapters/linear/webhook.go` - Webhook handler
- `internal/adapters/slack/notifier.go` - Slack notifications

### Executor
- `internal/executor/runner.go` - Claude Code process spawner
- `internal/executor/git.go` - Git operations
- `internal/executor/monitor.go` - Progress tracking

### Memory
- `internal/memory/store.go` - SQLite storage
- `internal/memory/graph.go` - Knowledge graph
- `internal/memory/patterns.go` - Global pattern store

## Development Commands

```bash
# Build
make build

# Run in development
make dev

# Run tests
make test

# Format code
make fmt
```

## Configuration

Copy `configs/pilot.example.yaml` to `~/.pilot/config.yaml`.

Required environment variables:
- `LINEAR_API_KEY` - Linear API key
- `SLACK_BOT_TOKEN` - Slack bot token

## Integration Points

### Linear Webhook
- Endpoint: `POST /webhooks/linear`
- Triggers on: Issue create with "pilot" label
- Handler: `internal/adapters/linear/webhook.go`

### Claude Code
- Spawned by: `internal/executor/runner.go`
- Command: `claude -p "task prompt"`
- Working dir: Project path from config

### Slack
- Notifications: Task started, progress, completed, failed
- Handler: `internal/adapters/slack/notifier.go`

## Documentation Loading Strategy

1. **Every session**: This file (2k tokens)
2. **Feature work**: Task doc (3k tokens)
3. **Architecture changes**: System doc (5k tokens)
4. **Integration work**: Relevant adapter code

Total: ~12k tokens vs 50k+ loading everything.
