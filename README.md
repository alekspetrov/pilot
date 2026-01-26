# Pilot

> AI that ships your tickets.

**Navigator guides. Pilot executes.**

Pilot is an autonomous AI development pipeline that receives tickets from Linear/Jira/Asana, implements features using Claude Code, and creates PRs for review.

## Quick Start

```bash
# Install
brew install pilot-ai/tap/pilot

# Configure
pilot init

# Start daemon
pilot start

# Create ticket in Linear with label "pilot"
# ... Pilot implements the feature ...
# ... PR ready for review ...
```

## How It Works

```
Manager creates ticket → Pilot ships code → Engineer reviews PR
```

1. **Ticket Created**: Create a ticket in Linear with the "pilot" label
2. **Pilot Receives**: Webhook notifies Pilot of new work
3. **Task Planned**: LLM converts ticket to implementation plan
4. **Code Written**: Claude Code implements the feature
5. **PR Created**: Changes committed, PR opened
6. **Team Notified**: Slack message with PR link

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         PILOT                                   │
├─────────────────────────────────────────────────────────────────┤
│  Gateway (Go)          │  WebSocket + HTTP server              │
│  Adapters              │  Linear, Slack, GitHub (future)       │
│  Orchestrator (Python) │  LLM-powered task planning            │
│  Executor              │  Claude Code process management       │
│  Memory                │  SQLite + knowledge graph             │
│  Dashboard             │  Terminal UI (bubbletea)              │
└─────────────────────────────────────────────────────────────────┘
```

## Installation

### Homebrew (macOS)

```bash
brew install pilot-ai/tap/pilot
```

### From Source

```bash
git clone https://github.com/alekspetrov/pilot
cd pilot
make build
make install
```

### Requirements

- Go 1.22+
- Python 3.11+
- [Claude Code CLI](https://github.com/anthropics/claude-code)

## Configuration

```bash
# Interactive setup
pilot init
```

Or create `~/.pilot/config.yaml`:

```yaml
version: "1.0"

gateway:
  host: "127.0.0.1"
  port: 9090

adapters:
  linear:
    enabled: true
    api_key: "${LINEAR_API_KEY}"
    team_id: "your-team-id"

  slack:
    enabled: true
    bot_token: "${SLACK_BOT_TOKEN}"
    channel: "#dev-notifications"

projects:
  - name: "my-app"
    path: "/path/to/my-app"
    navigator: true
```

## Usage

### Start Daemon

```bash
# Start in foreground
pilot start

# Start in background
pilot start --daemon

# Check status
pilot status

# Stop daemon
pilot stop
```

### Dashboard

The built-in TUI shows real-time task progress:

```
🚀 Pilot Dashboard

📋 Tasks
────────────────────────────────────────────────────────────
▶ ● TASK-42 Add user authentication  [████████░░░░░░░░░░░░] 45%  2m 15s
  ○ TASK-43 Fix login bug            [░░░░░░░░░░░░░░░░░░░░]  0%  pending
  ✓ TASK-41 Update API docs          [████████████████████] 100% 5m 32s

📝 Logs
────────────────────────────────────────────────────────────
  [14:32:15] Starting TASK-42: Add user authentication
  [14:32:18] Creating branch: pilot/TASK-42
  [14:33:45] Phase: IMPL - Writing authentication middleware

q: quit • l: toggle logs • ↑/↓: select task
```

### CLI Commands

```bash
pilot start          # Start the daemon
pilot stop           # Stop the daemon
pilot status         # Show status and running tasks
pilot init           # Initialize configuration
pilot version        # Show version
```

## Integrations

### Linear

1. Create a Linear API key
2. Create a webhook pointing to `http://your-server:9090/webhooks/linear`
3. Create a "pilot" label for tasks you want Pilot to handle

### Slack

1. Create a Slack app with bot permissions
2. Add bot token to config
3. Invite bot to notification channel

## Development

```bash
# Install dependencies
make deps

# Run in development mode
make dev

# Run tests
make test

# Build for all platforms
make build-all
```

## Navigator Integration

Pilot uses [Navigator](https://github.com/alekspetrov/navigator) for context-efficient AI development:

- 92% token reduction vs bulk loading
- 20+ exchange sessions without restart
- Smart documentation loading

When `navigator: true` in project config, Pilot:
1. Starts Navigator session before implementation
2. Uses lazy-loading for documentation
3. Follows autonomous completion protocol

## Roadmap

- [x] Gateway foundation
- [x] Linear adapter
- [x] Slack notifications
- [x] Claude Code executor
- [x] Terminal dashboard
- [ ] GitHub Issues adapter
- [ ] Jira adapter
- [ ] Daily briefs
- [ ] Cross-project memory
- [ ] Pilot Cloud (hosted)

## License

MIT License - see [LICENSE](LICENSE)

## Contributing

Contributions welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) first.
