# CLI Reference

Complete command reference for Pilot.

## Core Commands

### `pilot start`

Start the Pilot daemon with configured inputs.

```bash
pilot start                          # Config-driven
pilot start --telegram               # Enable Telegram polling
pilot start --github                 # Enable GitHub issue polling
pilot start --linear                 # Enable Linear webhooks
pilot start --slack                  # Enable Slack Socket Mode
pilot start --telegram --github      # Enable both
pilot start --dashboard              # With TUI dashboard
pilot start --no-gateway             # Polling only (no HTTP server)
pilot start --sequential             # Sequential execution mode
pilot start --autopilot=stage        # Autopilot mode (dev/stage/prod)
pilot start --auto-release           # Enable automatic releases after PR merge
pilot start --tunnel                 # Enable public tunnel for webhook ingress
pilot start --daemon                 # Run in background (daemon mode)
pilot start -p ~/Projects/myapp      # Specify project
pilot start --replace                # Kill existing instance first
pilot start --team myteam            # Team ID for project scoping
pilot start --team-member user@email # Member email for team scoping
pilot start --log-format json        # Log output format (text or json)
```

### `pilot stop`

Stop the running Pilot daemon.

**Syntax:**
```bash
pilot stop [flags]
```

**Description:**
Gracefully stop the Pilot daemon. This command provides instructions for stopping the daemon using standard OS signals. For immediate termination, use Ctrl+C or send SIGTERM to the running process.

**Flags:**
| Flag | Description |
|------|-------------|
| `-h, --help` | Show help for the stop command |

**Examples:**
```bash
# Request daemon to stop (provides instructions)
pilot stop

# Alternative: Direct termination with signal
kill $(pgrep pilot)
```

### `pilot status`

Show current Pilot status, running tasks, and adapter configuration.

**Syntax:**
```bash
pilot status [flags]
```

**Description:**
Display the current status of the Pilot daemon, including gateway address, enabled adapters (Linear, Slack, Telegram, GitHub, Jira), and configured projects. Useful for debugging connectivity and configuration issues.

**Flags:**
| Flag | Description |
|------|-------------|
| `--json` | Output status information as JSON |
| `-h, --help` | Show help for the status command |

**Examples:**
```bash
# Show status in human-readable format
pilot status

# Output status as JSON for scripting
pilot status --json
```

### `pilot init`

Initialize Pilot configuration.

```bash
pilot init                    # Interactive setup
pilot init --minimal          # Minimal config only
```

### `pilot setup`

Interactive wizard to configure Pilot step by step.

**Syntax:**
```bash
pilot setup [flags]
```

**Description:**
Run an interactive setup wizard that walks you through configuring Pilot's core features including Telegram bot connection, project paths, voice transcription, daily briefs, alerts, and optionally Cloudflare Tunnel for webhook ingress.

**Flags:**
| Flag | Description |
|------|-------------|
| `--skip-optional` | Skip optional feature setup during the wizard |
| `--tunnel` | Set up Cloudflare Tunnel for webhooks (runs pilot tunnel setup) |
| `--no-sleep` | Disable Mac sleep for always-on operation (macOS only, requires sudo) |
| `-h, --help` | Show help for the setup command |

**Examples:**
```bash
# Run full interactive setup wizard
pilot setup

# Skip optional features (faster setup)
pilot setup --skip-optional

# Set up with Cloudflare Tunnel for webhooks
pilot setup --tunnel
```

### `pilot version`

Show Pilot version and build information.

**Syntax:**
```bash
pilot version [flags]
```

**Description:**
Display the current version of Pilot, along with build timestamp information if available. This is useful for debugging issues and verifying which version you're running.

**Flags:**
| Flag | Description |
|------|-------------|
| `-h, --help` | Show help for the version command |

**Examples:**
```bash
# Show version information
pilot version

# Example output:
# Pilot v0.63.0
# Built: 2026-02-14T10:30:00Z
```

---

## Task Execution

### `pilot task`

Execute a single task.

```bash
pilot task "Add user authentication"                    # Run in cwd
pilot task "Fix login bug" -p ~/Projects/myapp          # Specify project
pilot task "Add feature" --alerts                       # Enable alerts
pilot task "Refactor API" --verbose                     # Stream output
pilot task "Update docs" --dry-run                      # Preview only
pilot task "Budget task" --budget                       # Enable budget enforcement
pilot task "Team task" --team myteam                    # Team scoping
pilot task "Member task" --team-member user@email       # Member scoping
```

| Flag | Description |
|------|-------------|
| `-p, --project` | Project path |
| `--alerts` | Enable alerts for task execution |
| `--budget` | Enable budget enforcement for this task |
| `--verbose` | Stream Claude Code output |
| `--dry-run` | Show what would be executed without running |
| `--team` | Team ID or name for project access scoping |
| `--team-member` | Member email for team access scoping |

---

## GitHub Integration

### `pilot github run`

Execute a specific GitHub issue.

```bash
pilot github run 42                  # Run issue #42
pilot github run 42 --verbose        # With streaming output
```

---

## Upgrade & Maintenance

### `pilot upgrade`

Check for and install updates.

```bash
pilot upgrade                    # Check and upgrade
pilot upgrade check              # Only check for updates
pilot upgrade check --json       # Check with JSON output
pilot upgrade run                # Download and install latest version
pilot upgrade run --force        # Skip task completion wait
pilot upgrade run --yes          # Skip confirmation prompt
pilot upgrade rollback           # Restore previous version
```

### `pilot doctor`

Run system health checks.

```bash
pilot doctor                     # Run all checks
pilot doctor --verbose          # Show detailed output with fix suggestions
```

### `pilot logs`

View task execution logs.

```bash
pilot logs                       # Show recent task logs
pilot logs TASK-12345            # Show logs for specific task
pilot logs GH-15                 # Show logs for GitHub issue task
pilot logs --limit 20            # Show last 20 tasks
pilot logs --verbose             # Show detailed output
pilot logs --json                # Output as JSON
```

---

## Analytics & Reporting

### `pilot brief`

Generate and send daily/weekly briefs.

```bash
pilot brief                       # Show scheduler status
pilot brief --now                 # Generate and send immediately
pilot brief --weekly              # Generate weekly summary
```

### `pilot metrics`

View execution metrics.

```bash
pilot metrics summary             # Last 7 days overview
pilot metrics summary --days 30   # Last 30 days
pilot metrics daily               # Daily breakdown
pilot metrics projects            # Per-project stats
```

### `pilot usage`

View billable usage events, summaries, and export data for billing.

**Syntax:**
```bash
pilot usage [command] [flags]
```

**Description:**
Track and analyze API usage, costs, and billing data for Pilot operations. Provides detailed breakdowns by project, time period, and usage patterns to help manage costs and understand resource consumption.

**Subcommands:**
| Command | Description |
|---------|-------------|
| `summary` | Show usage summary for billing |
| `daily` | Show daily usage breakdown |
| `events` | Show raw usage events |
| `projects` | Show usage by project |
| `export` | Export usage data to JSON or CSV |

**Flags:**
| Flag | Description |
|------|-------------|
| `-h, --help` | Show help for the usage command |

**Examples:**
```bash
# Show billing summary
pilot usage summary

# Show daily usage breakdown
pilot usage daily

# Show raw usage events
pilot usage events

# Show usage by project
pilot usage projects

# Export usage data to JSON
pilot usage export --format json

# Export usage data to CSV
pilot usage export --format csv
```

---

## Memory & Patterns

### `pilot patterns`

Query learned patterns from cross-project memory.

```bash
pilot patterns list               # List all patterns
pilot patterns search "auth"      # Search by keyword
pilot patterns stats              # Usage statistics
```

---

## Replay & Debug

### `pilot replay`

View and analyze execution recordings.

```bash
pilot replay list                 # List recordings
pilot replay list --project myapp # Filter by project
pilot replay list --failed        # Only failed executions
pilot replay show <id>            # Show recording metadata
pilot replay play <id>            # Interactive TUI replay
pilot replay analyze <id>         # Token/phase breakdown
pilot replay export <id>          # Export to HTML/JSON/MD
pilot replay export <id> --format json
```

---

## Budget & Cost Control

### `pilot budget`

View budget status and limits.

```bash
pilot budget                      # Show current budget status
pilot budget status               # Detailed breakdown
```

---

## Team Management

### `pilot team`

Manage team permissions.

```bash
pilot team list                   # List teams
pilot team add <name>             # Add team
pilot team remove <name>          # Remove team
```

---

## Infrastructure

### `pilot tunnel`

Manage Cloudflare tunnel for webhook ingress.

```bash
pilot tunnel start                # Start tunnel
pilot tunnel stop                 # Stop tunnel
pilot tunnel status               # Show tunnel status
```

### `pilot webhooks`

Configure outbound webhooks.

```bash
pilot webhooks list               # List configured webhooks
pilot webhooks add <url>          # Add webhook endpoint
pilot webhooks remove <id>        # Remove webhook
pilot webhooks test <id>          # Test webhook delivery
```

---

## Shell Completion

### `pilot completion`

Generate shell completion scripts for Pilot commands.

**Syntax:**
```bash
pilot completion [bash|zsh|fish|powershell]
```

**Description:**
Generate completion scripts for different shells to enable tab completion for Pilot commands and flags. The completion scripts help you work faster by auto-completing command names and options.

**Flags:**
| Flag | Description |
|------|-------------|
| `-h, --help` | Show help for the completion command |

**Examples:**
```bash
# Generate bash completion
pilot completion bash

# Generate zsh completion
pilot completion zsh

# Generate fish completion
pilot completion fish

# Generate PowerShell completion
pilot completion powershell

# Install bash completion (Linux)
pilot completion bash > /etc/bash_completion.d/pilot

# Install bash completion (macOS)
pilot completion bash > $(brew --prefix)/etc/bash_completion.d/pilot

# Install zsh completion
pilot completion zsh > "${fpath[1]}/_pilot"
```

---

## Global Flags

These flags work with all commands:

| Flag | Description |
|------|-------------|
| `--config` | Config file path (default: ~/.pilot/config.yaml) |
| `--verbose` | Enable verbose output |
| `--quiet` | Suppress non-error output |
| `--help` | Show help for command |
