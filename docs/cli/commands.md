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

View execution metrics and analytics.

**Syntax:**
```bash
pilot metrics <command> [flags]
```

**Description:**
View aggregated metrics, daily breakdowns, and export data for analysis. Metrics include execution counts, success rates, token usage, costs, and code change statistics.

**Subcommands:**
| Command | Description |
|---------|-------------|
| `summary` | Show metrics summary for the last N days |
| `daily` | Show daily metrics breakdown |
| `projects` | Show metrics by project |
| `export` | Export metrics data to JSON or CSV |

**Examples:**
```bash
pilot metrics summary             # Last 7 days overview
pilot metrics summary --days 30   # Last 30 days
pilot metrics daily               # Daily breakdown
pilot metrics projects            # Per-project stats
pilot metrics export --format csv # Export to CSV
```

#### `pilot metrics summary`

Show aggregated metrics summary for a time period.

**Syntax:**
```bash
pilot metrics summary [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--days int` | Number of days to include (default: 7) |
| `--projects strings` | Filter by project paths |
| `-h, --help` | Show help for the summary command |

**Output includes:**
- Execution stats (total, success rate, PRs created)
- Duration stats (total, average, fastest, slowest)
- Token usage (total, input, output, average per task)
- Estimated cost (total, average per task)
- Code changes (files, lines added/removed)

**Examples:**
```bash
# Last 7 days overview
pilot metrics summary

# Last 30 days
pilot metrics summary --days 30

# Filter by project
pilot metrics summary --projects /path/to/project
```

#### `pilot metrics daily`

Show daily metrics breakdown in a tabular format.

**Syntax:**
```bash
pilot metrics daily [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--days int` | Number of days to include (default: 7) |
| `--projects strings` | Filter by project paths |
| `-h, --help` | Show help for the daily command |

**Output columns:**
- Date
- Total executions
- Passed/Failed counts
- Total duration
- Token usage
- Cost

**Examples:**
```bash
# Last 7 days daily breakdown
pilot metrics daily

# Last 14 days
pilot metrics daily --days 14
```

#### `pilot metrics projects`

Show metrics aggregated by project.

**Syntax:**
```bash
pilot metrics projects [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--days int` | Number of days to include (default: 30) |
| `--limit int` | Maximum projects to show (default: 10) |
| `-h, --help` | Show help for the projects command |

**Output per project:**
- Project name and path
- Task count and success rate
- Total duration
- Token usage
- Cost
- Last execution time

**Examples:**
```bash
# Top 10 projects by activity
pilot metrics projects

# All projects from last 90 days
pilot metrics projects --days 90 --limit 50
```

#### `pilot metrics export`

Export metrics data to JSON or CSV format.

**Syntax:**
```bash
pilot metrics export [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--days int` | Number of days to include (default: 30) |
| `--projects strings` | Filter by project paths |
| `--format string` | Output format: json or csv (default: json) |
| `-o, --output string` | Output file (- for stdout) |
| `-h, --help` | Show help for the export command |

**Examples:**
```bash
# Export to JSON (stdout)
pilot metrics export

# Export to CSV file
pilot metrics export --format csv -o metrics.csv

# Export specific projects
pilot metrics export --projects /path/to/project --format json -o project_metrics.json
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

View and manage cost controls for Claude usage across all Pilot operations.

**Syntax:**
```bash
pilot budget [command] [flags]
```

**Description:**
Budget controls help you monitor and limit API costs for Claude usage. Configure limits, track spending, and set enforcement actions when limits are exceeded.

**Subcommands:**
| Command | Description |
|---------|-------------|
| `status` | Show current budget status and spending |
| `config` | Show budget configuration settings |
| `reset` | Reset blocked tasks counter and resume execution |

**Examples:**
```bash
# Show current budget status
pilot budget status

# Filter budget status by user
pilot budget status --user alice

# View budget configuration
pilot budget config

# Reset blocked tasks counter (requires confirmation)
pilot budget reset --confirm
```

#### `pilot budget status`

Show current budget status with spending breakdown and visual progress bars.

**Syntax:**
```bash
pilot budget status [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--user string` | Filter by user ID (optional) |
| `-h, --help` | Show help for the status command |

**Examples:**
```bash
# Show overall budget status
pilot budget status

# Show budget for specific user
pilot budget status --user alice
```

**Output includes:**
- Daily/monthly spending vs limits
- Progress bars with color coding
- Enforcement status and warnings
- Blocked tasks count
- Per-task limits and policies

#### `pilot budget config`

Display the complete budget configuration including limits, enforcement actions, and thresholds.

**Syntax:**
```bash
pilot budget config [flags]
```

**Description:**
Shows the current configuration loaded from ~/.pilot/config.yaml and provides a YAML example for reference.

**Flags:**
| Flag | Description |
|------|-------------|
| `-h, --help` | Show help for the config command |

**Examples:**
```bash
# Show current budget configuration
pilot budget config
```

**Output includes:**
- Enable/disable status
- Daily and monthly limits
- Per-task token and duration limits
- Enforcement actions (pause/stop)
- Warning thresholds
- YAML configuration example

#### `pilot budget reset`

Reset the blocked tasks counter and resume task execution if paused due to daily limits.

**Syntax:**
```bash
pilot budget reset [flags]
```

**Description:**
This command clears the blocked tasks count and daily pause status, allowing Pilot to resume normal operation. Use when you want to override daily budget enforcement.

**Flags:**
| Flag | Description |
|------|-------------|
| `--confirm` | Confirm the reset operation (required for safety) |
| `-h, --help` | Show help for the reset command |

**Examples:**
```bash
# Show warning, require confirmation
pilot budget reset

# Reset immediately with confirmation
pilot budget reset --confirm
```

**CAUTION:** This will resume task execution even if daily limits have been exceeded. Monitor your budget carefully after reset.

---

## Project Management

### `pilot project`

Add, list, remove, and configure projects for Pilot.

**Syntax:**
```bash
pilot project <command> [flags]
```

**Description:**
Manage the projects that Pilot can work on. Projects store configuration like GitHub repository, default branch, and Navigator settings.

**Subcommands:**
| Command | Description |
|---------|-------------|
| `list` | List all configured projects |
| `add` | Add a new project |
| `remove` | Remove a project |
| `set-default` | Set the default project |
| `show` | Show project details |

**Examples:**
```bash
pilot project list                # List all projects
pilot project add --name my-app   # Add current directory as project
pilot project show my-app         # Show project details
pilot project set-default my-app  # Set as default
pilot project remove my-app       # Remove project
```

#### `pilot project list`

List all configured projects with their settings.

**Syntax:**
```bash
pilot project list [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `-h, --help` | Show help for the list command |

**Output columns:**
- NAME: Project name
- PATH: Project path
- GITHUB: Owner/repo
- BRANCH: Default branch
- NAV: Navigator enabled (*)
- DEFAULT: Default project (*)

#### `pilot project add`

Add a new project to Pilot configuration.

**Syntax:**
```bash
pilot project add [flags]
```

**Auto-detection:**
- If `--path` is omitted, uses current working directory
- If `--branch` is omitted, detects from git remote
- If `--navigator` is omitted, checks for `.agent/` directory
- If `--github` is omitted, parses from git remote origin

**Flags:**
| Flag | Description |
|------|-------------|
| `-n, --name string` | Project name (required) |
| `-p, --path string` | Project path (default: current directory) |
| `-g, --github string` | GitHub repo (owner/repo) |
| `-b, --branch string` | Default branch (auto-detected) |
| `--navigator` | Enable Navigator (auto-detected) |
| `-d, --set-default` | Set as default project |
| `-h, --help` | Show help for the add command |

**Examples:**
```bash
# Add current directory with auto-detection
pilot project add --name my-app

# Specify all options
pilot project add -n my-app -p /path/to/project -g owner/repo -b main

# Add and set as default
pilot project add --name my-app --set-default
```

#### `pilot project remove`

Remove a project from Pilot configuration.

**Syntax:**
```bash
pilot project remove [name] [flags]
```

**Arguments:**
| Argument | Description |
|----------|-------------|
| `name` | Project name to remove |

**Flags:**
| Flag | Description |
|------|-------------|
| `-n, --name string` | Project name (alternative to positional arg) |
| `-f, --force` | Skip confirmation prompt |
| `-h, --help` | Show help for the remove command |

**Examples:**
```bash
# Remove with confirmation
pilot project remove my-app

# Force remove without confirmation
pilot project remove my-app --force
```

#### `pilot project set-default`

Set the default project for Pilot commands.

**Syntax:**
```bash
pilot project set-default <name>
```

**Arguments:**
| Argument | Description |
|----------|-------------|
| `name` | Project name to set as default |

**Examples:**
```bash
pilot project set-default my-app
```

#### `pilot project show`

Show detailed information about a project.

**Syntax:**
```bash
pilot project show [name]
```

**Arguments:**
| Argument | Description |
|----------|-------------|
| `name` | Project name (optional, shows default if omitted) |

**Output includes:**
- Path
- GitHub owner/repo
- Default branch
- Navigator status
- Default project status

**Examples:**
```bash
# Show specific project
pilot project show my-app

# Show default project
pilot project show
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

## Access Control

### `pilot allow`

Manage Telegram allowed users.

**Syntax:**
```bash
pilot allow [user_id] [flags]
```

**Description:**
Add, remove, or list Telegram user IDs in the allowed_ids list. This controls which Telegram users can interact with the Pilot bot.

**Flags:**
| Flag | Description |
|------|-------------|
| `--remove` | Remove user from allowed_ids |
| `--list` | List current allowed users |
| `-h, --help` | Show help for the allow command |

**Examples:**
```bash
# Add a user
pilot allow 123456789

# Remove a user
pilot allow --remove 123456789

# List allowed users
pilot allow --list
```

**Note:** After adding or removing users, restart Pilot to apply changes: `pilot restart`

---

## Autopilot & Release

### `pilot autopilot`

View and manage autopilot PR lifecycle automation.

**Syntax:**
```bash
pilot autopilot <command> [flags]
```

**Description:**
Commands for viewing and managing autopilot PR tracking and automation. Autopilot manages PR lifecycle including CI monitoring, auto-review, auto-merge, and releases.

**Subcommands:**
| Command | Description |
|---------|-------------|
| `status` | Show tracked PRs and their current stage |

**Examples:**
```bash
pilot autopilot status            # Show autopilot status
pilot autopilot status --json     # JSON output
```

#### `pilot autopilot status`

Display autopilot status including configuration and release settings.

**Syntax:**
```bash
pilot autopilot status [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--json` | Output as JSON |
| `-h, --help` | Show help for the status command |

**Output includes:**
- Environment (dev/stage/prod)
- Configuration:
  - Auto Merge status
  - Auto Review status
  - Merge method
  - CI timeout
  - Max failures
- Release configuration:
  - Enabled status
  - Trigger type
  - Require CI flag
  - Tag prefix

**Examples:**
```bash
# Human-readable status
pilot autopilot status

# JSON output for scripting
pilot autopilot status --json
```

**Note:** Pilot must be running with `--autopilot` flag for autopilot features to be active.

---

### `pilot release`

Create a release manually.

**Syntax:**
```bash
pilot release [version] [flags]
```

**Description:**
Create a new release for the current repository. If no version is specified, detects version bump from commits since the last release using conventional commit messages.

**Arguments:**
| Argument | Description |
|----------|-------------|
| `version` | Specific version to release (optional, e.g., v1.2.3) |

**Flags:**
| Flag | Description |
|------|-------------|
| `--bump string` | Force bump type: patch, minor, major |
| `--draft` | Create release as draft |
| `--dry-run` | Show what would be released without creating |
| `-h, --help` | Show help for the release command |

**Version detection:**
When no version is specified, Pilot analyzes commits since the last release:
- `feat:` commits trigger a minor bump
- `fix:` commits trigger a patch bump
- `BREAKING CHANGE:` or `!` in commit triggers a major bump

**Examples:**
```bash
# Auto-detect version from commits
pilot release

# Force minor bump
pilot release --bump=minor

# Specific version
pilot release v1.2.3

# Create as draft
pilot release --draft

# Preview without creating
pilot release --dry-run
```

**Prerequisites:**
- GitHub token configured (`github.token` in config or `GITHUB_TOKEN` env var)
- Repository must have at least one prior release for version detection

---

## Infrastructure

### `pilot tunnel`

Manage Cloudflare Tunnel for permanent webhook URLs.

**Syntax:**
```bash
pilot tunnel <command> [flags]
```

**Description:**
The tunnel provides a permanent public URL for receiving webhooks from GitHub, Linear, and other services - no port forwarding required.

**Supported providers:**
- `cloudflare`: Free, permanent URLs via Cloudflare Tunnel
- `ngrok`: Quick testing (requires ngrok account for custom domains)

**Subcommands:**
| Command | Description |
|---------|-------------|
| `status` | Show tunnel status |
| `start` | Start the tunnel |
| `stop` | Stop the tunnel |
| `url` | Show the tunnel webhook URL |
| `setup` | Set up tunnel (create tunnel, configure DNS) |
| `service` | Manage tunnel auto-start service |

**Examples:**
```bash
pilot tunnel status               # Show tunnel status
pilot tunnel start                # Start tunnel (background)
pilot tunnel start --foreground   # Run in foreground
pilot tunnel stop                 # Stop tunnel
pilot tunnel url                  # Show base URL
pilot tunnel url --webhook /webhooks/github  # Show full webhook URL
```

#### `pilot tunnel status`

Show the current tunnel status including provider, connection state, URL, and service status.

**Syntax:**
```bash
pilot tunnel status [flags]
```

**Output includes:**
- Provider (cloudflare or ngrok)
- Running status
- Connection state
- Public URL
- Tunnel ID
- Service status (launchd on macOS)

#### `pilot tunnel start`

Start the Cloudflare Tunnel to expose the local webhook endpoint.

**Syntax:**
```bash
pilot tunnel start [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `-f, --foreground` | Run in foreground (default: background) |
| `-h, --help` | Show help for the start command |

**Examples:**
```bash
# Start tunnel in background
pilot tunnel start

# Start tunnel in foreground (Ctrl+C to stop)
pilot tunnel start --foreground
```

#### `pilot tunnel stop`

Stop the running tunnel and any associated service.

**Syntax:**
```bash
pilot tunnel stop [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `-h, --help` | Show help for the stop command |

#### `pilot tunnel url`

Show the tunnel webhook URL. Useful for configuring webhooks in external services.

**Syntax:**
```bash
pilot tunnel url [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--webhook string` | Append webhook path (e.g., /webhooks/github) |
| `-h, --help` | Show help for the url command |

**Examples:**
```bash
# Show base URL
pilot tunnel url

# Show full GitHub webhook URL
pilot tunnel url --webhook /webhooks/github
```

#### `pilot tunnel setup`

Set up Cloudflare Tunnel for permanent webhook URLs.

**Syntax:**
```bash
pilot tunnel setup [flags]
```

**Description:**
This command:
1. Checks for cloudflared CLI installation
2. Authenticates with Cloudflare (if needed)
3. Creates a tunnel named 'pilot-webhook'
4. Configures DNS routing (if custom domain provided)
5. Optionally installs auto-start service

**Prerequisites:**
- Cloudflare account (free tier is sufficient)
- cloudflared CLI: `brew install cloudflared`

**Flags:**
| Flag | Description |
|------|-------------|
| `--provider string` | Tunnel provider: cloudflare, ngrok (default: cloudflare) |
| `--domain string` | Custom domain (optional) |
| `--service` | Install auto-start service |
| `-h, --help` | Show help for the setup command |

**Examples:**
```bash
# Basic setup
pilot tunnel setup

# With custom domain
pilot tunnel setup --domain pilot.example.com

# With auto-start service
pilot tunnel setup --service
```

#### `pilot tunnel service`

Manage the tunnel auto-start service (macOS only via launchd).

**Syntax:**
```bash
pilot tunnel service <command> [flags]
```

**Subcommands:**
| Command | Description |
|---------|-------------|
| `install` | Install auto-start service |
| `uninstall` | Remove auto-start service |
| `status` | Show service status |

**Examples:**
```bash
# Install service (tunnel auto-starts on boot)
pilot tunnel service install

# Check service status
pilot tunnel service status

# Remove service
pilot tunnel service uninstall
```

---

### `pilot webhooks`

Manage outbound webhooks for Pilot events.

**Syntax:**
```bash
pilot webhooks <command> [flags]
```

**Description:**
Webhooks allow external integrations to receive real-time notifications when tasks start, complete, fail, or when PRs are created.

**Supported events:**
| Event | Description |
|-------|-------------|
| `task.started` | Task execution began |
| `task.progress` | Phase updates during execution |
| `task.completed` | Task finished successfully |
| `task.failed` | Task failed |
| `pr.created` | Pull request was created |
| `budget.warning` | Budget threshold reached |

**Subcommands:**
| Command | Description |
|---------|-------------|
| `list` | List configured webhook endpoints |
| `add` | Add a new webhook endpoint |
| `remove` | Remove a webhook endpoint |
| `test` | Send a test event to webhook endpoint(s) |
| `events` | List available webhook event types |

**Examples:**
```bash
pilot webhooks list                              # List configured webhooks
pilot webhooks add --url https://example.com/hook --secret $SECRET
pilot webhooks remove ep_abc123
pilot webhooks test ep_abc123                    # Send test event
pilot webhooks events                            # List event types
```

#### `pilot webhooks list`

List all configured webhook endpoints.

**Syntax:**
```bash
pilot webhooks list [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--json` | Output as JSON |
| `-h, --help` | Show help for the list command |

**Examples:**
```bash
# Human-readable format
pilot webhooks list

# JSON output for scripting
pilot webhooks list --json
```

#### `pilot webhooks add`

Add a new webhook endpoint to receive Pilot events.

**Syntax:**
```bash
pilot webhooks add [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--name string` | Endpoint name (optional, defaults to hostname) |
| `--url string` | Webhook URL (required) |
| `--secret string` | HMAC signing secret |
| `--events strings` | Event types to subscribe (default: all) |
| `--enabled` | Enable endpoint (default: true) |
| `-h, --help` | Show help for the add command |

**Examples:**
```bash
# Subscribe to all events
pilot webhooks add --url https://example.com/hook --secret $SECRET

# Subscribe to specific events
pilot webhooks add --url https://example.com/hook --secret $SECRET \
  --events task.completed,task.failed,pr.created

# With custom name
pilot webhooks add --name "Slack Integration" --url https://hooks.slack.com/... --secret $SECRET
```

#### `pilot webhooks remove`

Remove a webhook endpoint by ID.

**Syntax:**
```bash
pilot webhooks remove <endpoint-id>
```

**Arguments:**
| Argument | Description |
|----------|-------------|
| `endpoint-id` | The endpoint ID (e.g., ep_abc123) |

**Examples:**
```bash
pilot webhooks remove ep_abc123
```

#### `pilot webhooks test`

Send a test event to verify webhook endpoint configuration.

**Syntax:**
```bash
pilot webhooks test [endpoint-id] [flags]
```

**Arguments:**
| Argument | Description |
|----------|-------------|
| `endpoint-id` | Optional: specific endpoint to test (tests all if omitted) |

**Flags:**
| Flag | Description |
|------|-------------|
| `--event string` | Event type to test (default: task.completed) |
| `-h, --help` | Show help for the test command |

**Examples:**
```bash
# Test all enabled endpoints
pilot webhooks test

# Test specific endpoint
pilot webhooks test ep_abc123

# Test with specific event type
pilot webhooks test --event task.failed
```

#### `pilot webhooks events`

List all available webhook event types with descriptions.

**Syntax:**
```bash
pilot webhooks events
```

**Output:**
Displays all event categories (Task Events, PR Events, Budget Events) and their meanings.

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
