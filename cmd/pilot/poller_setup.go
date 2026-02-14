package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/alekspetrov/pilot/internal/adapters/asana"
	"github.com/alekspetrov/pilot/internal/adapters/github"
	"github.com/alekspetrov/pilot/internal/adapters/linear"
	"github.com/alekspetrov/pilot/internal/adapters/slack"
	"github.com/alekspetrov/pilot/internal/adapters/telegram"
	"github.com/alekspetrov/pilot/internal/alerts"
	"github.com/alekspetrov/pilot/internal/approval"
	"github.com/alekspetrov/pilot/internal/autopilot"
	"github.com/alekspetrov/pilot/internal/banner"
	"github.com/alekspetrov/pilot/internal/briefs"
	"github.com/alekspetrov/pilot/internal/budget"
	"github.com/alekspetrov/pilot/internal/config"
	"github.com/alekspetrov/pilot/internal/dashboard"
	"github.com/alekspetrov/pilot/internal/executor"
	"github.com/alekspetrov/pilot/internal/logging"
	"github.com/alekspetrov/pilot/internal/memory"
	"github.com/alekspetrov/pilot/internal/quality"
	"github.com/alekspetrov/pilot/internal/teams"
	"github.com/alekspetrov/pilot/internal/tunnel"
	"github.com/alekspetrov/pilot/internal/upgrade"
)

// applyInputOverrides applies CLI flag overrides to config
func applyInputOverrides(cfg *config.Config, cmd *cobra.Command, telegramFlag, githubFlag, linearFlag, slackFlag, tunnelFlag bool) {
	if cmd.Flags().Changed("telegram") {
		if cfg.Adapters.Telegram == nil {
			cfg.Adapters.Telegram = telegram.DefaultConfig()
		}
		cfg.Adapters.Telegram.Enabled = telegramFlag
		cfg.Adapters.Telegram.Polling = telegramFlag
	}
	if cmd.Flags().Changed("github") {
		if cfg.Adapters.GitHub == nil {
			cfg.Adapters.GitHub = github.DefaultConfig()
		}
		cfg.Adapters.GitHub.Enabled = githubFlag
		if cfg.Adapters.GitHub.Polling == nil {
			cfg.Adapters.GitHub.Polling = &github.PollingConfig{}
		}
		cfg.Adapters.GitHub.Polling.Enabled = githubFlag
	}
	if cmd.Flags().Changed("linear") {
		if cfg.Adapters.Linear == nil {
			cfg.Adapters.Linear = linear.DefaultConfig()
		}
		cfg.Adapters.Linear.Enabled = linearFlag
	}
	if cmd.Flags().Changed("slack") {
		if cfg.Adapters.Slack == nil {
			cfg.Adapters.Slack = slack.DefaultConfig()
		}
		cfg.Adapters.Slack.Enabled = slackFlag
		cfg.Adapters.Slack.SocketMode = slackFlag
	}
	if cmd.Flags().Changed("tunnel") {
		if cfg.Tunnel == nil {
			cfg.Tunnel = tunnel.DefaultConfig()
		}
		cfg.Tunnel.Enabled = tunnelFlag
	}
}

// applyTeamOverrides applies --team and --team-member CLI flag overrides to config (GH-635).
// When --team is set, enables team-based project access scoping.
func applyTeamOverrides(cfg *config.Config, cmd *cobra.Command, teamID, teamMember string) {
	if !cmd.Flags().Changed("team") {
		return
	}
	if cfg.Team == nil {
		cfg.Team = &config.TeamConfig{}
	}
	cfg.Team.Enabled = true
	cfg.Team.TeamID = teamID
	if cmd.Flags().Changed("team-member") {
		cfg.Team.MemberEmail = teamMember
	}
}

// runPollingMode runs lightweight polling-only mode (no HTTP gateway)
func runPollingMode(cfg *config.Config, projectPath string, replace, dashboardMode bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Check Telegram config if enabled
	hasTelegram := cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled
	if hasTelegram && cfg.Adapters.Telegram.BotToken == "" {
		return fmt.Errorf("telegram enabled but bot_token not configured")
	}

	// GH-710: Validate Slack Socket Mode config — degrade gracefully if app_token missing
	if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.SocketMode && cfg.Adapters.Slack.AppToken == "" {
		logging.WithComponent("slack").Warn("socket_mode enabled but app_token not configured, skipping Slack Socket Mode")
		cfg.Adapters.Slack.SocketMode = false
	}

	// Suppress logging BEFORE creating runner in dashboard mode (GH-190)
	// Runner caches its logger at creation time, so suppression must happen first
	if dashboardMode {
		logging.Suppress()
	}

	// Create runner with config (GH-956: enables worktree isolation, decomposer, model routing)
	runner, err := executor.NewRunnerWithConfig(cfg.Executor)
	if err != nil {
		return fmt.Errorf("failed to create executor runner: %w", err)
	}

	// Set up quality gates if configured (GH-207)
	if cfg.Quality != nil && cfg.Quality.Enabled {
		runner.SetQualityCheckerFactory(func(taskID, taskProjectPath string) executor.QualityChecker {
			return &qualityCheckerWrapper{
				executor: quality.NewExecutor(&quality.ExecutorConfig{
					Config:      cfg.Quality,
					ProjectPath: taskProjectPath,
					TaskID:      taskID,
				}),
			}
		})
		logging.WithComponent("start").Info("quality gates enabled for polling mode")
	}

	// Set up team project access checker if configured (GH-635)
	if teamCleanup := wireProjectAccessChecker(runner, cfg); teamCleanup != nil {
		defer teamCleanup()
	}

	// GH-962: Clean up orphaned worktree directories from previous crashed executions
	if cfg.Executor != nil && cfg.Executor.UseWorktree {
		if err := executor.CleanupOrphanedWorktrees(ctx, projectPath); err != nil {
			// Log the cleanup but don't fail startup - this is best-effort cleanup
			logging.WithComponent("start").Info("worktree cleanup completed", slog.String("result", err.Error()))
		} else {
			logging.WithComponent("start").Debug("worktree cleanup scan completed, no orphans found")
		}
	}

	// Create approval manager
	approvalMgr := approval.NewManager(cfg.Approval)

	// Register Telegram approval handler if enabled
	if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled && cfg.Adapters.Telegram.BotToken != "" {
		tgClient := telegram.NewClient(cfg.Adapters.Telegram.BotToken)
		tgApprovalHandler := approval.NewTelegramHandler(&telegramApprovalAdapter{client: tgClient}, cfg.Adapters.Telegram.ChatID)
		approvalMgr.RegisterHandler(tgApprovalHandler)
		logging.WithComponent("start").Info("registered Telegram approval handler")
	}

	// Register Slack approval handler if enabled
	if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled && cfg.Adapters.Slack.BotToken != "" {
		if cfg.Adapters.Slack.Approval != nil && cfg.Adapters.Slack.Approval.Enabled {
			slackClient := slack.NewClient(cfg.Adapters.Slack.BotToken)
			slackAdapter := slack.NewSlackClientAdapter(slackClient)
			slackChannel := cfg.Adapters.Slack.Approval.Channel
			if slackChannel == "" {
				slackChannel = cfg.Adapters.Slack.Channel
			}
			slackApprovalHandler := approval.NewSlackHandler(&slackApprovalClientAdapter{adapter: slackAdapter}, slackChannel)
			approvalMgr.RegisterHandler(slackApprovalHandler)
			logging.WithComponent("start").Info("registered Slack approval handler",
				slog.String("channel", slackChannel))
		}
	}

	// GH-929: Create autopilot controllers map (one per repo) if enabled
	autopilotControllers := make(map[string]*autopilot.Controller)
	var autopilotController *autopilot.Controller // Default controller for backwards compat
	if cfg.Orchestrator.Autopilot != nil && cfg.Orchestrator.Autopilot.Enabled {
		// Need GitHub client for autopilot
		ghToken := ""
		if cfg.Adapters.GitHub != nil {
			ghToken = cfg.Adapters.GitHub.Token
			if ghToken == "" {
				ghToken = os.Getenv("GITHUB_TOKEN")
			}
		}
		if ghToken != "" {
			ghClient := github.NewClient(ghToken)

			// Create controller for default repo (adapters.github.repo)
			if cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Repo != "" {
				parts := strings.SplitN(cfg.Adapters.GitHub.Repo, "/", 2)
				if len(parts) == 2 {
					controller := autopilot.NewController(
						cfg.Orchestrator.Autopilot,
						ghClient,
						approvalMgr,
						parts[0],
						parts[1],
					)
					autopilotControllers[cfg.Adapters.GitHub.Repo] = controller
					autopilotController = controller // Default for backwards compat
				}
			}

			// GH-929: Create controllers for each project with GitHub config
			for _, proj := range cfg.Projects {
				if proj.GitHub == nil || proj.GitHub.Owner == "" || proj.GitHub.Repo == "" {
					continue
				}
				repoFullName := fmt.Sprintf("%s/%s", proj.GitHub.Owner, proj.GitHub.Repo)
				if _, exists := autopilotControllers[repoFullName]; exists {
					continue // Skip duplicates
				}
				controller := autopilot.NewController(
					cfg.Orchestrator.Autopilot,
					ghClient,
					approvalMgr,
					proj.GitHub.Owner,
					proj.GitHub.Repo,
				)
				autopilotControllers[repoFullName] = controller
				logging.WithComponent("autopilot").Info("created controller for project",
					slog.String("project", proj.Name),
					slog.String("repo", repoFullName),
				)
			}
		}
	}

	// Initialize memory store early for dashboard persistence (GH-367)
	store, err := memory.NewStore(cfg.Memory.Path)
	if err != nil {
		logging.WithComponent("start").Warn("Failed to open memory store", slog.Any("error", err))
		store = nil
	} else {
		defer func() {
			if store != nil {
				_ = store.Close()
			}
		}()
	}

	// GH-726: Initialize autopilot state store for crash recovery
	var autopilotStateStore *autopilot.StateStore
	if store != nil && len(autopilotControllers) > 0 {
		var storeErr error
		autopilotStateStore, storeErr = autopilot.NewStateStore(store.DB())
		if storeErr != nil {
			logging.WithComponent("autopilot").Warn("Failed to initialize state store", slog.Any("error", storeErr))
		} else {
			// GH-929: Wire state store to all controllers
			for repoName, controller := range autopilotControllers {
				controller.SetStateStore(autopilotStateStore)
				restored, restoreErr := controller.RestoreState()
				if restoreErr != nil {
					logging.WithComponent("autopilot").Warn("Failed to restore state from SQLite",
						slog.String("repo", repoName),
						slog.Any("error", restoreErr))
				} else if restored > 0 {
					logging.WithComponent("autopilot").Info("Restored autopilot PR states from SQLite",
						slog.String("repo", repoName),
						slog.Int("count", restored))
				}
			}
		}
	}

	// GH-634: Initialize teams service for RBAC enforcement
	if store != nil {
		teamStore, teamErr := teams.NewStore(store.DB())
		if teamErr != nil {
			logging.WithComponent("teams").Warn("Failed to initialize team store", slog.Any("error", teamErr))
		} else {
			teamSvc := teams.NewService(teamStore)
			teamAdapter = teams.NewServiceAdapter(teamSvc)
			runner.SetTeamChecker(teamAdapter)
			logging.WithComponent("teams").Info("team RBAC enforcement enabled for polling mode")
		}
	}

	// GH-1027: Initialize knowledge store for experiential memories
	if store != nil {
		knowledgeStore := memory.NewKnowledgeStore(store.DB())
		if err := knowledgeStore.InitSchema(); err != nil {
			logging.WithComponent("knowledge").Warn("Failed to initialize knowledge store schema", slog.Any("error", err))
		} else {
			runner.SetKnowledgeStore(knowledgeStore)
			logging.WithComponent("knowledge").Debug("Knowledge store initialized for polling mode")
		}
	}

	// Create monitor and TUI program for dashboard mode
	var monitor *executor.Monitor
	var program *tea.Program
	var upgradeRequestCh chan struct{} // Channel for hot upgrade requests (GH-369)
	if dashboardMode {
		runner.SuppressProgressLogs(true)

		monitor = executor.NewMonitor()
		runner.SetMonitor(monitor)
		upgradeRequestCh = make(chan struct{}, 1)
		model := dashboard.NewModelWithOptions(version, store, autopilotController, upgradeRequestCh)
		program = tea.NewProgram(model,
			tea.WithAltScreen(),
			tea.WithInput(os.Stdin),
			tea.WithOutput(os.Stdout),
		)

		// Wire runner progress updates to dashboard using named callback
		// This uses AddProgressCallback instead of OnProgress to prevent Telegram handler
		// from overwriting the dashboard callback (GH-149 fix)
		runner.AddProgressCallback("dashboard", func(taskID, phase string, progress int, message string) {
			monitor.UpdateProgress(taskID, phase, progress, message)
			tasks := convertTaskStatesToDisplay(monitor.GetAll())
			program.Send(dashboard.UpdateTasks(tasks)())

			logMsg := fmt.Sprintf("[%s] %s: %s (%d%%)", taskID, phase, message, progress)
			program.Send(dashboard.AddLog(logMsg)())
		})

		// Wire token usage updates to dashboard (GH-156 fix)
		runner.AddTokenCallback("dashboard", func(taskID string, inputTokens, outputTokens int64) {
			program.Send(dashboard.UpdateTokens(int(inputTokens), int(outputTokens))())
		})
	}

	// Initialize Telegram handler if enabled
	var tgHandler *telegram.Handler
	if hasTelegram {
		var allowedIDs []int64
		// Include explicitly configured allowed IDs
		allowedIDs = append(allowedIDs, cfg.Adapters.Telegram.AllowedIDs...)
		// Also include ChatID so user can message their own bot
		if cfg.Adapters.Telegram.ChatID != "" {
			if id, err := parseInt64(cfg.Adapters.Telegram.ChatID); err == nil {
				allowedIDs = append(allowedIDs, id)
			}
		}

		tgConfig := &telegram.HandlerConfig{
			BotToken:      cfg.Adapters.Telegram.BotToken,
			ProjectPath:   projectPath,
			Projects:      config.NewProjectSource(cfg),
			AllowedIDs:    allowedIDs,
			Transcription: cfg.Adapters.Telegram.Transcription,
			RateLimit:     cfg.Adapters.Telegram.RateLimit,
			LLMClassifier: cfg.Adapters.Telegram.LLMClassifier,
		}
		// GH-634: Wire team member resolver if available (avoid nil interface trap)
		if teamAdapter != nil {
			tgConfig.MemberResolver = teamAdapter
		}
		tgHandler = telegram.NewHandler(tgConfig, runner)

		// Security warning if no allowed IDs configured
		if len(allowedIDs) == 0 {
			logging.WithComponent("telegram").Warn("SECURITY: allowed_ids is empty - ALL users can interact with the bot!")
		}

		// Check for existing instance
		if err := tgHandler.CheckSingleton(ctx); err != nil {
			if errors.Is(err, telegram.ErrConflict) {
				if replace {
					fmt.Println("🔄 Stopping existing bot instance...")
					if err := killExistingTelegramBot(); err != nil {
						return fmt.Errorf("failed to stop existing instance: %w", err)
					}
					fmt.Print("   Waiting for Telegram to release connection")
					maxRetries := 10
					var lastErr error
					for i := 0; i < maxRetries; i++ {
						delay := time.Duration(500+i*500) * time.Millisecond
						time.Sleep(delay)
						fmt.Print(".")
						if err := tgHandler.CheckSingleton(ctx); err == nil {
							fmt.Println(" ✓")
							fmt.Println("   ✓ Existing instance stopped")
							fmt.Println()
							lastErr = nil
							break
						} else {
							lastErr = err
						}
					}
					if lastErr != nil {
						fmt.Println(" ✗")
						return fmt.Errorf("timeout waiting for Telegram to release connection")
					}
				} else {
					fmt.Println()
					fmt.Println("❌ Another bot instance is already running")
					fmt.Println()
					fmt.Println("   Options:")
					fmt.Println("   • Kill it manually:  pkill -f 'pilot start'")
					fmt.Println("   • Auto-replace:      pilot start --replace")
					fmt.Println()
					return fmt.Errorf("conflict: another bot instance is running")
				}
			} else {
				return fmt.Errorf("singleton check failed: %w", err)
			}
		}
	}

	// Show startup banner (skip in dashboard mode to avoid corrupting TUI)
	if !dashboardMode {
		banner.StartupTelegram(version, projectPath, cfg.Adapters.Telegram.ChatID, cfg)
	}

	// Log autopilot status
	if cfg.Orchestrator.Autopilot != nil && cfg.Orchestrator.Autopilot.Enabled {
		logging.WithComponent("start").Info("autopilot enabled",
			slog.String("environment", string(cfg.Orchestrator.Autopilot.Environment)),
			slog.Bool("auto_merge", cfg.Orchestrator.Autopilot.AutoMerge),
			slog.Bool("auto_review", cfg.Orchestrator.Autopilot.AutoReview),
		)
	}

	// Initialize alerts engine for outbound notifications (GH-337)
	var alertsEngine *alerts.Engine
	alertsCfg := getAlertsConfig(cfg)
	if alertsCfg != nil && alertsCfg.Enabled {
		// Create dispatcher and register channels
		alertsDispatcher := alerts.NewDispatcher(alertsCfg)

		// Register Slack channel if configured
		if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled && cfg.Adapters.Slack.BotToken != "" {
			slackClient := slack.NewClient(cfg.Adapters.Slack.BotToken)
			for _, ch := range alertsCfg.Channels {
				if ch.Type == "slack" && ch.Slack != nil {
					slackChannel := alerts.NewSlackChannel(ch.Name, slackClient, ch.Slack.Channel)
					alertsDispatcher.RegisterChannel(slackChannel)
				}
			}
		}

		// Register Telegram channel if configured
		if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled && cfg.Adapters.Telegram.BotToken != "" {
			telegramClient := telegram.NewClient(cfg.Adapters.Telegram.BotToken)
			for _, ch := range alertsCfg.Channels {
				if ch.Type == "telegram" && ch.Telegram != nil {
					telegramChannel := alerts.NewTelegramChannel(ch.Name, telegramClient, ch.Telegram.ChatID)
					alertsDispatcher.RegisterChannel(telegramChannel)
				}
			}
		}

		alertsEngine = alerts.NewEngine(alertsCfg, alerts.WithDispatcher(alertsDispatcher))
		if err := alertsEngine.Start(ctx); err != nil {
			logging.WithComponent("start").Warn("failed to start alerts engine", slog.Any("error", err))
			alertsEngine = nil
		} else {
			logging.WithComponent("start").Info("alerts engine started",
				slog.Int("channels", len(alertsDispatcher.ListChannels())),
			)
		}
	}

	// Initialize dispatcher for task queue (uses store created earlier)
	var dispatcher *executor.Dispatcher
	if store != nil {
		dispatcher = executor.NewDispatcher(store, runner, nil)
		if err := dispatcher.Start(); err != nil {
			logging.WithComponent("start").Warn("Failed to start dispatcher", slog.Any("error", err))
			dispatcher = nil
		} else {
			logging.WithComponent("start").Info("Task dispatcher started")
		}
	}

	// GH-539: Create budget enforcer if configured
	var enforcer *budget.Enforcer
	if cfg.Budget != nil && cfg.Budget.Enabled && store != nil {
		enforcer = budget.NewEnforcer(cfg.Budget, store)
		// Wire alert callback to alerts engine
		if alertsEngine != nil {
			enforcer.OnAlert(func(alertType, message, severity string) {
				alertsEngine.ProcessEvent(alerts.Event{
					Type:      alerts.EventTypeBudgetWarning,
					Error:     message,
					Metadata:  map[string]string{"alert_type": alertType, "severity": severity},
					Timestamp: time.Now(),
				})
			})
		}
		logging.WithComponent("start").Info("budget enforcement enabled",
			slog.Float64("daily_limit", cfg.Budget.DailyLimit),
			slog.Float64("monthly_limit", cfg.Budget.MonthlyLimit),
		)

		// GH-539: Wire per-task token/duration limits into executor stream
		maxTokens, maxDuration := enforcer.GetPerTaskLimits()
		if maxTokens > 0 || maxDuration > 0 {
			var taskLimiters sync.Map // map[taskID]*budget.TaskLimiter
			runner.SetTokenLimitCheck(func(taskID string, deltaInput, deltaOutput int64) bool {
				// Get or create limiter for this task
				val, _ := taskLimiters.LoadOrStore(taskID, budget.NewTaskLimiter(maxTokens, maxDuration))
				limiter := val.(*budget.TaskLimiter)

				// Feed token deltas into the limiter
				totalDelta := deltaInput + deltaOutput
				if totalDelta > 0 {
					if !limiter.AddTokens(totalDelta) {
						return false
					}
				}

				// Also check duration on every event
				if !limiter.CheckDuration() {
					return false
				}

				return true
			})
			logging.WithComponent("start").Info("per-task budget limits enabled",
				slog.Int64("max_tokens", maxTokens),
				slog.Duration("max_duration", maxDuration),
			)
		}

		if !dashboardMode {
			fmt.Printf("💰 Budget enforcement enabled: $%.2f/day, $%.2f/month\n",
				cfg.Budget.DailyLimit, cfg.Budget.MonthlyLimit)
		}
	} else {
		// GH-1019: Log why budget is disabled for debugging
		logging.WithComponent("start").Debug("budget enforcement disabled",
			slog.Bool("config_nil", cfg.Budget == nil),
			slog.Bool("enabled", cfg.Budget != nil && cfg.Budget.Enabled),
			slog.Bool("store_nil", store == nil),
		)
	}

	// GH-929: Start GitHub polling for multiple repos if enabled
	var ghPollers []*github.Poller
	polledRepos := make(map[string]bool) // Track repos already polled to avoid duplicates

	if cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled &&
		cfg.Adapters.GitHub.Polling != nil && cfg.Adapters.GitHub.Polling.Enabled {

		token := cfg.Adapters.GitHub.Token
		if token == "" {
			token = os.Getenv("GITHUB_TOKEN")
		}

		if token != "" {
			client := github.NewClient(token)
			label := cfg.Adapters.GitHub.Polling.Label
			if label == "" {
				label = cfg.Adapters.GitHub.PilotLabel
			}
			interval := cfg.Adapters.GitHub.Polling.Interval
			if interval == 0 {
				interval = 30 * time.Second
			}

			// Determine execution mode from config
			execMode := github.ExecutionModeSequential // Default to sequential
			waitForMerge := true
			pollInterval := 30 * time.Second
			prTimeout := 1 * time.Hour

			if cfg.Orchestrator != nil && cfg.Orchestrator.Execution != nil {
				execCfg := cfg.Orchestrator.Execution
				if execCfg.Mode == "parallel" {
					execMode = github.ExecutionModeParallel
				}
				waitForMerge = execCfg.WaitForMerge
				if execCfg.PollInterval > 0 {
					pollInterval = execCfg.PollInterval
				}
				if execCfg.PRTimeout > 0 {
					prTimeout = execCfg.PRTimeout
				}
			}

			modeStr := "sequential"
			if execMode == github.ExecutionModeParallel {
				modeStr = "parallel"
			}

			// Helper to create poller for a repo with its project path
			createPollerForRepo := func(repoFullName, projPath string) (*github.Poller, error) {
				repoParts := strings.Split(repoFullName, "/")
				if len(repoParts) != 2 {
					return nil, fmt.Errorf("invalid repo format: %s", repoFullName)
				}
				repoOwner, repoName := repoParts[0], repoParts[1]

				// GH-386: Validate repo/project match at startup
				if err := executor.ValidateRepoProjectMatch(repoFullName, projPath); err != nil {
					logging.WithComponent("github").Warn("repo/project mismatch detected",
						slog.String("repo", repoFullName),
						slog.String("project_path", projPath),
						slog.String("expected_project", executor.ExtractRepoName(repoFullName)),
					)
				}

				var pollerOpts []github.PollerOption

				// Wire autopilot callback to the correct controller for this repo
				controller := autopilotControllers[repoFullName]
				if controller != nil {
					pollerOpts = append(pollerOpts,
						github.WithOnPRCreated(controller.OnPRCreated),
					)
				}

				// GH-726: Wire processed issue persistence
				if autopilotStateStore != nil {
					pollerOpts = append(pollerOpts, github.WithProcessedStore(autopilotStateStore))
				}

				// Capture variables for closures
				sourceRepo := repoFullName
				projPathCapture := projPath
				controllerCapture := controller

				// Create rate limit retry scheduler for this repo
				rateLimitScheduler := executor.NewScheduler(executor.DefaultSchedulerConfig(), nil)
				rateLimitScheduler.SetRetryCallback(func(retryCtx context.Context, pendingTask *executor.PendingTask) error {
					var issueNum int
					if _, err := fmt.Sscanf(pendingTask.Task.ID, "GH-%d", &issueNum); err != nil {
						return fmt.Errorf("invalid task ID format: %s", pendingTask.Task.ID)
					}

					issue, err := client.GetIssue(retryCtx, repoOwner, repoName, issueNum)
					if err != nil {
						return fmt.Errorf("failed to fetch issue for retry: %w", err)
					}

					logging.WithComponent("scheduler").Info("Retrying rate-limited issue",
						slog.String("repo", sourceRepo),
						slog.Int("issue", issueNum),
						slog.Int("attempt", pendingTask.Attempts),
					)

					result, err := handleGitHubIssueWithResult(retryCtx, cfg, client, issue, projPathCapture, sourceRepo, dispatcher, runner, monitor, program, alertsEngine, enforcer)

					if result != nil && result.PRNumber > 0 && controllerCapture != nil {
						controllerCapture.OnPRCreated(result.PRNumber, result.PRURL, issue.Number, result.HeadSHA, result.BranchName)
					}

					return err
				})
				rateLimitScheduler.SetExpiredCallback(func(expiredCtx context.Context, pendingTask *executor.PendingTask) {
					logging.WithComponent("scheduler").Error("Task exceeded max retry attempts",
						slog.String("task_id", pendingTask.Task.ID),
						slog.Int("attempts", pendingTask.Attempts),
					)
				})
				if err := rateLimitScheduler.Start(ctx); err != nil {
					logging.WithComponent("start").Warn("Failed to start rate limit scheduler",
						slog.String("repo", repoFullName),
						slog.Any("error", err))
				}

				// Configure based on execution mode
				if execMode == github.ExecutionModeSequential {
					pollerOpts = append(pollerOpts,
						github.WithExecutionMode(github.ExecutionModeSequential),
						github.WithSequentialConfig(waitForMerge, pollInterval, prTimeout),
						github.WithScheduler(rateLimitScheduler),
						github.WithOnIssueWithResult(func(issueCtx context.Context, issue *github.Issue) (*github.IssueResult, error) {
							return handleGitHubIssueWithResult(issueCtx, cfg, client, issue, projPathCapture, sourceRepo, dispatcher, runner, monitor, program, alertsEngine, enforcer)
						}),
					)
				} else {
					pollerOpts = append(pollerOpts,
						github.WithExecutionMode(github.ExecutionModeParallel),
						github.WithScheduler(rateLimitScheduler),
						github.WithMaxConcurrent(cfg.Orchestrator.MaxConcurrent),
						github.WithOnIssueWithResult(func(issueCtx context.Context, issue *github.Issue) (*github.IssueResult, error) {
							return handleGitHubIssueWithResult(issueCtx, cfg, client, issue, projPathCapture, sourceRepo, dispatcher, runner, monitor, program, alertsEngine, enforcer)
						}),
					)
				}

				return github.NewPoller(client, repoFullName, label, interval, pollerOpts...)
			}

			// Create poller for default repo (adapters.github.repo)
			if cfg.Adapters.GitHub.Repo != "" {
				polledRepos[cfg.Adapters.GitHub.Repo] = true
				poller, err := createPollerForRepo(cfg.Adapters.GitHub.Repo, projectPath)
				if err != nil {
					if !dashboardMode {
						fmt.Printf("⚠️  GitHub polling disabled for %s: %v\n", cfg.Adapters.GitHub.Repo, err)
					}
				} else {
					ghPollers = append(ghPollers, poller)
					if !dashboardMode {
						fmt.Printf("🐙 GitHub polling enabled: %s (every %s, mode: %s)\n", cfg.Adapters.GitHub.Repo, interval, modeStr)
					}
				}
			}

			// GH-929: Create pollers for each project with GitHub config
			for _, proj := range cfg.Projects {
				if proj.GitHub == nil || proj.GitHub.Owner == "" || proj.GitHub.Repo == "" {
					continue
				}
				repoFullName := fmt.Sprintf("%s/%s", proj.GitHub.Owner, proj.GitHub.Repo)
				if polledRepos[repoFullName] {
					continue // Skip duplicates
				}
				polledRepos[repoFullName] = true

				projPath := proj.Path
				if projPath == "" {
					projPath = projectPath // Fall back to default project path
				}

				poller, err := createPollerForRepo(repoFullName, projPath)
				if err != nil {
					logging.WithComponent("github").Warn("Failed to create poller for project",
						slog.String("project", proj.Name),
						slog.String("repo", repoFullName),
						slog.Any("error", err))
					continue
				}
				ghPollers = append(ghPollers, poller)
				if !dashboardMode {
					fmt.Printf("🐙 GitHub polling enabled: %s (project: %s, every %s, mode: %s)\n", repoFullName, proj.Name, interval, modeStr)
				}
			}

			// Start all pollers
			for _, poller := range ghPollers {
				go poller.Start(ctx)
			}

			if len(ghPollers) > 0 {
				if !dashboardMode && execMode == github.ExecutionModeSequential && waitForMerge {
					fmt.Printf("   ⏳ Sequential mode: waiting for PR merge before next issue (timeout: %s)\n", prTimeout)
				}

				// Start autopilot processing loops for all controllers
				for repoName, controller := range autopilotControllers {
					// Scan for existing PRs
					if err := controller.ScanExistingPRs(ctx); err != nil {
						logging.WithComponent("autopilot").Warn("failed to scan existing PRs",
							slog.String("repo", repoName),
							slog.Any("error", err),
						)
					}

					// Scan for recently merged PRs (GH-416)
					if err := controller.ScanRecentlyMergedPRs(ctx); err != nil {
						logging.WithComponent("autopilot").Warn("failed to scan merged PRs",
							slog.String("repo", repoName),
							slog.Any("error", err),
						)
					}

					// Start controller run loop
					go func(c *autopilot.Controller, repo string) {
						if err := c.Run(ctx); err != nil && err != context.Canceled {
							logging.WithComponent("autopilot").Error("autopilot controller stopped",
								slog.String("repo", repo),
								slog.Any("error", err),
							)
						}
					}(controller, repoName)
				}

				if len(autopilotControllers) > 0 && !dashboardMode {
					fmt.Printf("🤖 Autopilot enabled: %s environment (%d repos)\n", cfg.Orchestrator.Autopilot.Environment, len(autopilotControllers))
				}

				// Start metrics alerter for default controller (GH-728)
				if alertsEngine != nil && autopilotController != nil {
					metricsAlerter := autopilot.NewMetricsAlerter(autopilotController, alertsEngine)
					go metricsAlerter.Run(ctx)
				}

				// Start metrics persister for default controller (GH-728)
				if store != nil && autopilotController != nil {
					metricsPersister := autopilot.NewMetricsPersister(autopilotController, store)
					go metricsPersister.Run(ctx)
				}

				// Wire sub-issue PR callback for default controller (GH-594)
				if autopilotController != nil {
					runner.SetOnSubIssuePRCreated(autopilotController.OnPRCreated)
				}
			}

			// Start stale label cleanup for default repo if enabled
			if cfg.Adapters.GitHub.Repo != "" && cfg.Adapters.GitHub.StaleLabelCleanup != nil && cfg.Adapters.GitHub.StaleLabelCleanup.Enabled {
				if store != nil {
					cleanerOpts := []github.CleanerOption{}
					// Wire callback to clear processed map when pilot-failed labels are removed
					if len(ghPollers) > 0 {
						cleanerOpts = append(cleanerOpts, github.WithOnFailedCleaned(func(issueNumber int) {
							for _, p := range ghPollers {
								p.ClearProcessed(issueNumber)
							}
						}))
					}
					cleaner, cleanerErr := github.NewCleaner(client, store, cfg.Adapters.GitHub.Repo, cfg.Adapters.GitHub.StaleLabelCleanup, cleanerOpts...)
					if cleanerErr != nil {
						if !dashboardMode {
							fmt.Printf("⚠️  Stale label cleanup disabled: %v\n", cleanerErr)
						}
					} else {
						if !dashboardMode {
							fmt.Printf("🧹 Stale label cleanup enabled (every %s, in-progress: %s, failed: %s)\n",
								cfg.Adapters.GitHub.StaleLabelCleanup.Interval,
								cfg.Adapters.GitHub.StaleLabelCleanup.Threshold,
								cfg.Adapters.GitHub.StaleLabelCleanup.FailedThreshold)
						}
						go cleaner.Start(ctx)
					}
				}
			}
		}
	}

	// Start Linear polling if enabled (GH-393)
	if cfg.Adapters.Linear != nil && cfg.Adapters.Linear.Enabled &&
		cfg.Adapters.Linear.Polling != nil && cfg.Adapters.Linear.Polling.Enabled {

		workspaces := cfg.Adapters.Linear.GetWorkspaces()
		for _, ws := range workspaces {
			// Determine interval: workspace override > global > default
			interval := 30 * time.Second
			if ws.Polling != nil && ws.Polling.Interval > 0 {
				interval = ws.Polling.Interval
			} else if cfg.Adapters.Linear.Polling.Interval > 0 {
				interval = cfg.Adapters.Linear.Polling.Interval
			}

			// Check if workspace polling is explicitly disabled
			if ws.Polling != nil && !ws.Polling.Enabled {
				continue
			}

			linearClient := linear.NewClient(ws.APIKey)
			linearPoller := linear.NewPoller(linearClient, ws, interval,
				linear.WithOnLinearIssue(func(issueCtx context.Context, issue *linear.Issue) (*linear.IssueResult, error) {
					return handleLinearIssueWithResult(issueCtx, cfg, linearClient, issue, projectPath, dispatcher, runner, monitor, program, alertsEngine, enforcer)
				}),
			)

			if !dashboardMode {
				fmt.Printf("📊 Linear polling enabled: %s/%s (every %s)\n", ws.Name, ws.TeamID, interval)
			}
			go func(p *linear.Poller, name string) {
				if err := p.Start(ctx); err != nil {
					logging.WithComponent("linear").Error("Linear poller failed",
						slog.String("workspace", name),
						slog.Any("error", err),
					)
				}
			}(linearPoller, ws.Name)
		}
	}

	// Start Asana polling if enabled (GH-906)
	if cfg.Adapters.Asana != nil && cfg.Adapters.Asana.Enabled &&
		cfg.Adapters.Asana.Polling != nil && cfg.Adapters.Asana.Polling.Enabled {

		// Determine interval
		interval := 30 * time.Second
		if cfg.Adapters.Asana.Polling.Interval > 0 {
			interval = cfg.Adapters.Asana.Polling.Interval
		}

		asanaClient := asana.NewClient(
			cfg.Adapters.Asana.AccessToken,
			cfg.Adapters.Asana.WorkspaceID,
		)
		asanaPoller := asana.NewPoller(asanaClient, cfg.Adapters.Asana, interval,
			asana.WithOnAsanaTask(func(taskCtx context.Context, task *asana.Task) (*asana.TaskResult, error) {
				return handleAsanaTaskWithResult(taskCtx, cfg, asanaClient, task, projectPath, dispatcher, runner, monitor, program, alertsEngine, enforcer)
			}),
		)

		if !dashboardMode {
			fmt.Printf("📦 Asana polling enabled: workspace %s (every %s)\n", cfg.Adapters.Asana.WorkspaceID, interval)
		}
		go func(p *asana.Poller) {
			if err := p.Start(ctx); err != nil {
				logging.WithComponent("asana").Error("Asana poller failed",
					slog.Any("error", err),
				)
			}
		}(asanaPoller)
	}

	// Start Telegram polling if enabled
	if tgHandler != nil {
		if !dashboardMode {
			fmt.Println("📱 Telegram polling started")
		}
		tgHandler.StartPolling(ctx)
	}

	// Start Slack Socket Mode if enabled (GH-652: wire into polling mode)
	var slackHandler *slack.Handler
	if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled && cfg.Adapters.Slack.SocketMode &&
		cfg.Adapters.Slack.AppToken != "" && cfg.Adapters.Slack.BotToken != "" {
		slackHandler = slack.NewHandler(&slack.HandlerConfig{
			AppToken:        cfg.Adapters.Slack.AppToken,
			BotToken:        cfg.Adapters.Slack.BotToken,
			ProjectPath:     projectPath,
			Projects:        config.NewSlackProjectSource(cfg),
			AllowedChannels: cfg.Adapters.Slack.AllowedChannels,
			AllowedUsers:    cfg.Adapters.Slack.AllowedUsers,
			MemberResolver:  teamAdapter,
		}, runner)

		go func() {
			if err := slackHandler.StartListening(ctx); err != nil {
				logging.WithComponent("slack").Error("Slack Socket Mode error", slog.Any("error", err))
			}
		}()

		if !dashboardMode {
			fmt.Println("💬 Slack Socket Mode started")
		}
		logging.WithComponent("start").Info("Slack Socket Mode started in polling mode")
	}

	// Start brief scheduler if enabled
	var briefScheduler *briefs.Scheduler
	if cfg.Orchestrator.DailyBrief != nil && cfg.Orchestrator.DailyBrief.Enabled {
		briefCfg := cfg.Orchestrator.DailyBrief

		// Convert config to briefs.BriefConfig
		briefsConfig := &briefs.BriefConfig{
			Enabled:  briefCfg.Enabled,
			Schedule: briefCfg.Schedule,
			Timezone: briefCfg.Timezone,
			Content: briefs.ContentConfig{
				IncludeMetrics:     briefCfg.Content.IncludeMetrics,
				IncludeErrors:      briefCfg.Content.IncludeErrors,
				MaxItemsPerSection: briefCfg.Content.MaxItemsPerSection,
			},
			Filters: briefs.FilterConfig{
				Projects: briefCfg.Filters.Projects,
			},
		}

		// Convert channels
		for _, ch := range briefCfg.Channels {
			briefsConfig.Channels = append(briefsConfig.Channels, briefs.ChannelConfig{
				Type:       ch.Type,
				Channel:    ch.Channel,
				Recipients: ch.Recipients,
			})
		}

		// Create generator (requires store)
		if store != nil {
			generator := briefs.NewGenerator(store, briefsConfig)

			// Create delivery service with available clients
			var deliveryOpts []briefs.DeliveryOption
			if cfg.Adapters.Slack != nil && cfg.Adapters.Slack.Enabled {
				slackClient := slack.NewClient(cfg.Adapters.Slack.BotToken)
				deliveryOpts = append(deliveryOpts, briefs.WithSlackClient(slackClient))
			}
			if cfg.Adapters.Telegram != nil && cfg.Adapters.Telegram.Enabled {
				tgClient := telegram.NewClient(cfg.Adapters.Telegram.BotToken)
				deliveryOpts = append(deliveryOpts, briefs.WithTelegramSender(&telegramBriefAdapter{client: tgClient}))
			}
			deliveryOpts = append(deliveryOpts, briefs.WithLogger(slog.Default()))

			delivery := briefs.NewDeliveryService(briefsConfig, deliveryOpts...)

			// Create and start scheduler
			briefScheduler = briefs.NewScheduler(generator, delivery, briefsConfig, slog.Default(), store)
			if err := briefScheduler.Start(ctx); err != nil {
				logging.WithComponent("start").Warn("Failed to start brief scheduler", slog.Any("error", err))
				briefScheduler = nil
			} else {
				logging.WithComponent("start").Info("brief scheduler started",
					slog.String("schedule", briefCfg.Schedule),
					slog.String("timezone", briefCfg.Timezone),
				)
			}
		} else {
			logging.WithComponent("start").Warn("Brief scheduler requires memory store, skipping")
		}
	}

	// Dashboard mode: run TUI and handle shutdown via TUI quit
	if dashboardMode && program != nil {
		fmt.Println("\n🖥️  Starting TUI dashboard...")

		// Start background version checker for hot reload (GH-369)
		versionChecker := upgrade.NewVersionChecker(version, upgrade.DefaultCheckInterval)
		versionChecker.OnUpdate(func(info *upgrade.VersionInfo) {
			program.Send(dashboard.NotifyUpdateAvailable(info.Current, info.Latest, info.ReleaseNotes)())
			program.Send(dashboard.AddLog(fmt.Sprintf("⬆️ Update available: %s → %s", info.Current, info.Latest))())
		})
		versionChecker.Start(ctx)
		defer versionChecker.Stop()

		// Set up hot upgrade goroutine - listens for upgrade requests from 'u' key press
		// The channel is created above and passed to the dashboard model
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-upgradeRequestCh:
					info := versionChecker.GetLatestInfo()
					if info == nil || !info.UpdateAvail || info.LatestRelease == nil {
						program.Send(dashboard.NotifyUpgradeComplete(false, "No update available")())
						continue
					}

					// Drain pollers — stop accepting new issues before upgrade
					program.Send(dashboard.AddLog("⏳ Draining pollers — no new issues will be accepted...")())
					for _, p := range ghPollers {
						go p.Drain()
					}

					// Perform hot upgrade with monitor as TaskChecker
					// Monitor tracks running/queued tasks; upgrade waits for them to finish
					hotUpgrader, err := upgrade.NewHotUpgrader(version, monitor)
					if err != nil {
						program.Send(dashboard.NotifyUpgradeComplete(false, err.Error())())
						program.Send(dashboard.AddLog(fmt.Sprintf("❌ Upgrade failed: %v", err))())
						continue
					}

					upgradeCfg := &upgrade.HotUpgradeConfig{
						WaitForTasks: true,
						TaskTimeout:  30 * time.Minute,
						OnProgress: func(pct int, msg string) {
							program.Send(dashboard.NotifyUpgradeProgress(pct, msg)())
						},
						FlushSession: func() error {
							// Future: flush session state to SQLite here
							return nil
						},
					}

					if err := hotUpgrader.PerformHotUpgrade(ctx, info.LatestRelease, upgradeCfg); err != nil {
						program.Send(dashboard.NotifyUpgradeComplete(false, err.Error())())
						program.Send(dashboard.AddLog(fmt.Sprintf("❌ Upgrade failed: %v", err))())
					}
					// If upgrade succeeds, the process is replaced and this line is never reached
				}
			}
		}()

		// Periodic refresh to catch any missed updates
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if monitor != nil {
						tasks := convertTaskStatesToDisplay(monitor.GetAll())
						program.Send(dashboard.UpdateTasks(tasks)())
					}
				}
			}
		}()

		// Add startup logs after TUI starts (Send blocks if Run hasn't been called)
		go func() {
			time.Sleep(100 * time.Millisecond) // Wait for Run() to start
			program.Send(dashboard.AddLog(fmt.Sprintf("🚀 Pilot %s started - Polling mode", version))())
			if hasTelegram {
				program.Send(dashboard.AddLog("📱 Telegram polling active")())
			}
			hasGitHubPolling := cfg.Adapters.GitHub != nil && cfg.Adapters.GitHub.Enabled &&
				cfg.Adapters.GitHub.Polling != nil && cfg.Adapters.GitHub.Polling.Enabled
			if hasGitHubPolling {
				program.Send(dashboard.AddLog(fmt.Sprintf("🐙 GitHub polling: %s", cfg.Adapters.GitHub.Repo))())
			}
			hasLinearPolling := cfg.Adapters.Linear != nil && cfg.Adapters.Linear.Enabled &&
				cfg.Adapters.Linear.Polling != nil && cfg.Adapters.Linear.Polling.Enabled
			if hasLinearPolling {
				workspaces := cfg.Adapters.Linear.GetWorkspaces()
				for _, ws := range workspaces {
					program.Send(dashboard.AddLog(fmt.Sprintf("📊 Linear polling: %s/%s", ws.Name, ws.TeamID))())
				}
			}

			// Check for restart marker (set by hot upgrade)
			// GH-879: Config is automatically reloaded because syscall.Exec starts a fresh process
			if os.Getenv("PILOT_RESTARTED") == "1" {
				prevVersion := os.Getenv("PILOT_PREVIOUS_VERSION")
				if prevVersion != "" {
					program.Send(dashboard.AddLog(fmt.Sprintf("✅ Upgraded from %s to %s (config reloaded)", prevVersion, version))())
				} else {
					program.Send(dashboard.AddLog("✅ Pilot restarted (config reloaded)")())
				}
			}
		}()

		// Run TUI (blocks until quit via 'q' or Ctrl+C)
		// Note: The upgrade callback is handled via upgradeRequestCh above
		if _, err := program.Run(); err != nil {
			cancel() // Stop goroutines
			return fmt.Errorf("dashboard error: %w", err)
		}

		// Clean shutdown - cancel context to stop all goroutines
		cancel()

		// Terminate all running subprocesses (GH-883)
		runner.CancelAll()

		if tgHandler != nil {
			tgHandler.Stop()
		}
		// ghPoller stops via context cancellation (no explicit stop needed)
		if dispatcher != nil {
			dispatcher.Stop()
		}
		if briefScheduler != nil {
			briefScheduler.Stop()
		}
		return nil
	}

	// Non-dashboard mode: wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	fmt.Println("\n🛑 Shutting down...")

	// Terminate all running subprocesses (GH-883)
	runner.CancelAll()

	if tgHandler != nil {
		tgHandler.Stop()
	}
	if len(ghPollers) > 0 {
		fmt.Printf("🐙 Stopping GitHub pollers (%d)...\n", len(ghPollers))
	}
	if dispatcher != nil {
		fmt.Println("📋 Stopping task dispatcher...")
		dispatcher.Stop()
	}
	if briefScheduler != nil {
		briefScheduler.Stop()
	}

	return nil
}
