package config

import (
	"strings"
	"testing"
	"time"

	"github.com/alekspetrov/pilot/internal/adapters/github"
	"github.com/alekspetrov/pilot/internal/adapters/linear"
	"github.com/alekspetrov/pilot/internal/adapters/slack"
	"github.com/alekspetrov/pilot/internal/adapters/telegram"
	"github.com/alekspetrov/pilot/internal/budget"
	"github.com/alekspetrov/pilot/internal/executor"
	"github.com/alekspetrov/pilot/internal/gateway"
	"github.com/alekspetrov/pilot/internal/quality"
)

// baseValidConfig returns a minimal valid config for testing
func baseValidConfig() *Config {
	return &Config{
		Gateway: &gateway.Config{
			Host: "127.0.0.1",
			Port: 9091,
		},
		Projects: []*ProjectConfig{
			{Name: "test", Path: "/tmp/test"},
		},
	}
}

func TestConfig_Validate_EffortRouting(t *testing.T) {
	tests := []struct {
		name      string
		effort    *executor.EffortRoutingConfig
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "nil config is valid",
			effort:  nil,
			wantErr: false,
		},
		{
			name: "disabled routing skips validation",
			effort: &executor.EffortRoutingConfig{
				Enabled: false,
				Complex: "max", // Invalid but disabled
			},
			wantErr: false,
		},
		{
			name: "valid effort levels",
			effort: &executor.EffortRoutingConfig{
				Enabled: true,
				Trivial: "low",
				Simple:  "medium",
				Medium:  "high",
				Complex: "high",
			},
			wantErr: false,
		},
		{
			name: "empty values are valid",
			effort: &executor.EffortRoutingConfig{
				Enabled: true,
				Trivial: "",
				Simple:  "",
				Medium:  "",
				Complex: "",
			},
			wantErr: false,
		},
		{
			name: "max is invalid",
			effort: &executor.EffortRoutingConfig{
				Enabled: true,
				Trivial: "low",
				Simple:  "medium",
				Medium:  "high",
				Complex: "max",
			},
			wantErr:   true,
			errSubstr: "effort_routing.complex",
		},
		{
			name: "invalid trivial",
			effort: &executor.EffortRoutingConfig{
				Enabled: true,
				Trivial: "super",
			},
			wantErr:   true,
			errSubstr: "effort_routing.trivial",
		},
		{
			name: "case insensitive",
			effort: &executor.EffortRoutingConfig{
				Enabled: true,
				Trivial: "LOW",
				Simple:  "Medium",
				Medium:  "HIGH",
				Complex: "high",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValidConfig()
			cfg.Executor = &executor.BackendConfig{
				EffortRouting: tt.effort,
			}

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errSubstr)
				} else if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestConfig_Validate_Projects(t *testing.T) {
	tests := []struct {
		name           string
		projects       []*ProjectConfig
		defaultProject string
		wantErr        bool
		errSubstr      string
	}{
		{
			name: "valid projects",
			projects: []*ProjectConfig{
				{Name: "pilot", Path: "/home/user/pilot"},
			},
			defaultProject: "pilot",
			wantErr:        false,
		},
		{
			name:           "no projects is allowed",
			projects:       nil,
			defaultProject: "",
			wantErr:        false,
		},
		{
			name: "default project not found",
			projects: []*ProjectConfig{
				{Name: "pilot", Path: "/home/user/pilot"},
			},
			defaultProject: "other",
			wantErr:        true,
			errSubstr:      "default_project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValidConfig()
			cfg.Projects = tt.projects
			cfg.DefaultProject = tt.defaultProject

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errSubstr)
				} else if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidEffortLevels(t *testing.T) {
	valid := []string{"low", "medium", "high", ""}
	invalid := []string{"max", "super", "extreme", "none", "default"}

	for _, v := range valid {
		if !validEffortLevels[v] {
			t.Errorf("expected %q to be valid", v)
		}
	}

	for _, v := range invalid {
		if validEffortLevels[v] {
			t.Errorf("expected %q to be invalid", v)
		}
	}
}

// GH-1124: Test bounds and orchestrator validation
func TestConfig_Validate_OrchestratorBounds(t *testing.T) {
	tests := []struct {
		name         string
		orchestrator *OrchestratorConfig
		wantErr      bool
		errSubstr    string
	}{
		{
			name:         "nil orchestrator is valid",
			orchestrator: nil,
			wantErr:      false,
		},
		{
			name: "max_concurrent = 1 is valid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 1,
			},
			wantErr: false,
		},
		{
			name: "max_concurrent > 1 is valid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 5,
			},
			wantErr: false,
		},
		{
			name: "max_concurrent = 0 is invalid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 0,
			},
			wantErr:   true,
			errSubstr: "orchestrator.max_concurrent must be >= 1",
		},
		{
			name: "max_concurrent < 0 is invalid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: -1,
			},
			wantErr:   true,
			errSubstr: "orchestrator.max_concurrent must be >= 1",
		},
		{
			name: "sequential execution mode is valid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 2,
				Execution: &ExecutionConfig{
					Mode:         "sequential",
					PollInterval: 30 * time.Second,
				},
			},
			wantErr: false,
		},
		{
			name: "parallel execution mode is valid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 2,
				Execution: &ExecutionConfig{
					Mode:         "parallel",
					PollInterval: 30 * time.Second,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid execution mode",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 2,
				Execution: &ExecutionConfig{
					Mode:         "invalid",
					PollInterval: 30 * time.Second,
				},
			},
			wantErr:   true,
			errSubstr: "orchestrator.execution.mode must be 'sequential' or 'parallel'",
		},
		{
			name: "empty execution mode is invalid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 2,
				Execution: &ExecutionConfig{
					Mode:         "",
					PollInterval: 30 * time.Second,
				},
			},
			wantErr:   true,
			errSubstr: "orchestrator.execution.mode must be 'sequential' or 'parallel'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValidConfig()
			cfg.Orchestrator = tt.orchestrator

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errSubstr)
				} else if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestConfig_Validate_QualityBounds(t *testing.T) {
	tests := []struct {
		name      string
		quality   *quality.Config
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "nil quality config is valid",
			quality: nil,
			wantErr: false,
		},
		{
			name: "max_retries = 0 is valid",
			quality: &quality.Config{
				OnFailure: quality.FailureConfig{
					MaxRetries: 0,
				},
			},
			wantErr: false,
		},
		{
			name: "max_retries = 10 is valid",
			quality: &quality.Config{
				OnFailure: quality.FailureConfig{
					MaxRetries: 10,
				},
			},
			wantErr: false,
		},
		{
			name: "max_retries = 5 is valid",
			quality: &quality.Config{
				OnFailure: quality.FailureConfig{
					MaxRetries: 5,
				},
			},
			wantErr: false,
		},
		{
			name: "max_retries = 11 is invalid",
			quality: &quality.Config{
				OnFailure: quality.FailureConfig{
					MaxRetries: 11,
				},
			},
			wantErr:   true,
			errSubstr: "quality.on_failure.max_retries must be in range [0, 10]",
		},
		{
			name: "max_retries = -1 is invalid",
			quality: &quality.Config{
				OnFailure: quality.FailureConfig{
					MaxRetries: -1,
				},
			},
			wantErr:   true,
			errSubstr: "quality.on_failure.max_retries must be in range [0, 10]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValidConfig()
			cfg.Quality = tt.quality

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errSubstr)
				} else if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestConfig_Validate_BudgetBounds(t *testing.T) {
	tests := []struct {
		name      string
		budget    *budget.Config
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "nil budget config is valid",
			budget:  nil,
			wantErr: false,
		},
		{
			name: "disabled budget with zero daily_limit is valid",
			budget: &budget.Config{
				Enabled:    false,
				DailyLimit: 0,
			},
			wantErr: false,
		},
		{
			name: "disabled budget with negative daily_limit is valid",
			budget: &budget.Config{
				Enabled:    false,
				DailyLimit: -10,
			},
			wantErr: false,
		},
		{
			name: "enabled budget with positive daily_limit is valid",
			budget: &budget.Config{
				Enabled:    true,
				DailyLimit: 50.0,
			},
			wantErr: false,
		},
		{
			name: "enabled budget with zero daily_limit is invalid",
			budget: &budget.Config{
				Enabled:    true,
				DailyLimit: 0,
			},
			wantErr:   true,
			errSubstr: "budget.daily_limit must be > 0 when budget is enabled",
		},
		{
			name: "enabled budget with negative daily_limit is invalid",
			budget: &budget.Config{
				Enabled:    true,
				DailyLimit: -10.5,
			},
			wantErr:   true,
			errSubstr: "budget.daily_limit must be > 0 when budget is enabled",
		},
		{
			name: "enabled budget with very small positive daily_limit is valid",
			budget: &budget.Config{
				Enabled:    true,
				DailyLimit: 0.01,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValidConfig()
			cfg.Budget = tt.budget

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errSubstr)
				} else if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// GH-1126: Test adapter critical field validation
func TestConfig_Validate_AdapterCriticalFields(t *testing.T) {
	tests := []struct {
		name      string
		adapters  *AdaptersConfig
		wantErr   bool
		errSubstr string
	}{
		{
			name:     "nil adapters is valid",
			adapters: nil,
			wantErr:  false,
		},
		{
			name: "disabled GitHub adapter with empty token is valid",
			adapters: &AdaptersConfig{
				GitHub: &github.Config{
					Enabled: false,
					Token:   "",
				},
			},
			wantErr: false,
		},
		{
			name: "enabled GitHub adapter with token is valid",
			adapters: &AdaptersConfig{
				GitHub: &github.Config{
					Enabled: true,
					Token:   "test-github-token",
				},
			},
			wantErr: false,
		},
		{
			name: "enabled GitHub adapter with empty token is invalid",
			adapters: &AdaptersConfig{
				GitHub: &github.Config{
					Enabled: true,
					Token:   "",
				},
			},
			wantErr:   true,
			errSubstr: "adapters.github.token is required",
		},
		{
			name: "disabled Slack adapter with empty bot_token is valid",
			adapters: &AdaptersConfig{
				Slack: &slack.Config{
					Enabled:  false,
					BotToken: "",
				},
			},
			wantErr: false,
		},
		{
			name: "enabled Slack adapter with bot_token is valid",
			adapters: &AdaptersConfig{
				Slack: &slack.Config{
					Enabled:  true,
					BotToken: "xoxb-test-token",
				},
			},
			wantErr: false,
		},
		{
			name: "enabled Slack adapter with empty bot_token is invalid",
			adapters: &AdaptersConfig{
				Slack: &slack.Config{
					Enabled:  true,
					BotToken: "",
				},
			},
			wantErr:   true,
			errSubstr: "adapters.slack.bot_token is required",
		},
		{
			name: "enabled Slack adapter with non-xoxb token logs warning",
			adapters: &AdaptersConfig{
				Slack: &slack.Config{
					Enabled:  true,
					BotToken: "fake-token",
				},
			},
			wantErr: false,
		},
		{
			name: "disabled Telegram adapter with empty fields is valid",
			adapters: &AdaptersConfig{
				Telegram: &telegram.Config{
					Enabled:  false,
					BotToken: "",
					ChatID:   "",
				},
			},
			wantErr: false,
		},
		{
			name: "enabled Telegram adapter with both fields is valid",
			adapters: &AdaptersConfig{
				Telegram: &telegram.Config{
					Enabled:  true,
					BotToken: "test-bot-token",
					ChatID:   "123456",
				},
			},
			wantErr: false,
		},
		{
			name: "enabled Telegram adapter with empty bot_token is invalid",
			adapters: &AdaptersConfig{
				Telegram: &telegram.Config{
					Enabled:  true,
					BotToken: "",
					ChatID:   "123456",
				},
			},
			wantErr:   true,
			errSubstr: "adapters.telegram.bot_token is required",
		},
		{
			name: "enabled Telegram adapter with empty chat_id is invalid",
			adapters: &AdaptersConfig{
				Telegram: &telegram.Config{
					Enabled:  true,
					BotToken: "test-bot-token",
					ChatID:   "",
				},
			},
			wantErr:   true,
			errSubstr: "adapters.telegram.chat_id is required",
		},
		{
			name: "disabled Linear adapter is valid",
			adapters: &AdaptersConfig{
				Linear: &linear.Config{
					Enabled: false,
				},
			},
			wantErr: false,
		},
		{
			name: "enabled Linear adapter with api_key and team_id is valid",
			adapters: &AdaptersConfig{
				Linear: &linear.Config{
					Enabled: true,
					APIKey:  "test-api-key",
					TeamID:  "test-team-id",
				},
			},
			wantErr: false,
		},
		{
			name: "enabled Linear adapter with workspaces is valid",
			adapters: &AdaptersConfig{
				Linear: &linear.Config{
					Enabled: true,
					Workspaces: []*linear.WorkspaceConfig{
						{
							Name:   "test",
							APIKey: "test-api-key",
							TeamID: "test-team-id",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "enabled Linear adapter with no config is invalid",
			adapters: &AdaptersConfig{
				Linear: &linear.Config{
					Enabled: true,
				},
			},
			wantErr:   true,
			errSubstr: "adapters.linear must have either api_key+team_id or workspaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValidConfig()
			cfg.Adapters = tt.adapters

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errSubstr)
				} else if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// GH-1127: Test polling interval bounds checks
func TestConfig_Validate_PollingIntervalBounds(t *testing.T) {
	tests := []struct {
		name      string
		adapters  *AdaptersConfig
		wantErr   bool
		errSubstr string
	}{
		{
			name:     "nil adapters is valid",
			adapters: nil,
			wantErr:  false,
		},
		{
			name: "nil GitHub adapter is valid",
			adapters: &AdaptersConfig{
				GitHub: nil,
			},
			wantErr: false,
		},
		{
			name: "disabled GitHub adapter with any interval is valid",
			adapters: &AdaptersConfig{
				GitHub: &github.Config{
					Enabled: false,
					Polling: &github.PollingConfig{
						Enabled:  true,
						Interval: 1 * time.Second, // Below minimum but adapter disabled
					},
				},
			},
			wantErr: false,
		},
		{
			name: "enabled GitHub adapter with polling disabled is valid",
			adapters: &AdaptersConfig{
				GitHub: &github.Config{
					Enabled: true,
					Token:   "test-token",
					Polling: &github.PollingConfig{
						Enabled:  false,
						Interval: 1 * time.Second, // Below minimum but polling disabled
					},
				},
			},
			wantErr: false,
		},
		{
			name: "enabled GitHub adapter with nil polling config is valid",
			adapters: &AdaptersConfig{
				GitHub: &github.Config{
					Enabled: true,
					Token:   "test-token",
					Polling: nil,
				},
			},
			wantErr: false,
		},
		{
			name: "enabled GitHub adapter with polling interval = 10s is valid (boundary)",
			adapters: &AdaptersConfig{
				GitHub: &github.Config{
					Enabled: true,
					Token:   "test-token",
					Polling: &github.PollingConfig{
						Enabled:  true,
						Interval: 10 * time.Second,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "enabled GitHub adapter with polling interval > 10s is valid",
			adapters: &AdaptersConfig{
				GitHub: &github.Config{
					Enabled: true,
					Token:   "test-token",
					Polling: &github.PollingConfig{
						Enabled:  true,
						Interval: 30 * time.Second,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "enabled GitHub adapter with polling interval < 10s is invalid",
			adapters: &AdaptersConfig{
				GitHub: &github.Config{
					Enabled: true,
					Token:   "test-token",
					Polling: &github.PollingConfig{
						Enabled:  true,
						Interval: 9 * time.Second,
					},
				},
			},
			wantErr:   true,
			errSubstr: "adapters.github.polling.interval must be >= 10s",
		},
		{
			name: "enabled GitHub adapter with zero polling interval is invalid",
			adapters: &AdaptersConfig{
				GitHub: &github.Config{
					Enabled: true,
					Token:   "test-token",
					Polling: &github.PollingConfig{
						Enabled:  true,
						Interval: 0,
					},
				},
			},
			wantErr:   true,
			errSubstr: "adapters.github.polling.interval must be >= 10s",
		},
		{
			name: "enabled GitHub adapter with 1s polling interval is invalid",
			adapters: &AdaptersConfig{
				GitHub: &github.Config{
					Enabled: true,
					Token:   "test-token",
					Polling: &github.PollingConfig{
						Enabled:  true,
						Interval: 1 * time.Second,
					},
				},
			},
			wantErr:   true,
			errSubstr: "adapters.github.polling.interval must be >= 10s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValidConfig()
			cfg.Adapters = tt.adapters

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errSubstr)
				} else if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestConfig_Validate_OrchestratorPollIntervalBounds(t *testing.T) {
	tests := []struct {
		name         string
		orchestrator *OrchestratorConfig
		wantErr      bool
		errSubstr    string
	}{
		{
			name:         "nil orchestrator is valid",
			orchestrator: nil,
			wantErr:      false,
		},
		{
			name: "nil execution config is valid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 2,
				Execution:     nil,
			},
			wantErr: false,
		},
		{
			name: "orchestrator execution poll_interval = 10s is valid (boundary)",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 2,
				Execution: &ExecutionConfig{
					Mode:         "sequential",
					PollInterval: 10 * time.Second,
				},
			},
			wantErr: false,
		},
		{
			name: "orchestrator execution poll_interval > 10s is valid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 2,
				Execution: &ExecutionConfig{
					Mode:         "sequential",
					PollInterval: 30 * time.Second,
				},
			},
			wantErr: false,
		},
		{
			name: "orchestrator execution poll_interval = 1m is valid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 2,
				Execution: &ExecutionConfig{
					Mode:         "parallel",
					PollInterval: 1 * time.Minute,
				},
			},
			wantErr: false,
		},
		{
			name: "orchestrator execution poll_interval < 10s is invalid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 2,
				Execution: &ExecutionConfig{
					Mode:         "sequential",
					PollInterval: 9 * time.Second,
				},
			},
			wantErr:   true,
			errSubstr: "orchestrator.execution.poll_interval must be >= 10s",
		},
		{
			name: "orchestrator execution poll_interval = 0 is invalid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 2,
				Execution: &ExecutionConfig{
					Mode:         "sequential",
					PollInterval: 0,
				},
			},
			wantErr:   true,
			errSubstr: "orchestrator.execution.poll_interval must be >= 10s",
		},
		{
			name: "orchestrator execution poll_interval = 5s is invalid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 2,
				Execution: &ExecutionConfig{
					Mode:         "sequential",
					PollInterval: 5 * time.Second,
				},
			},
			wantErr:   true,
			errSubstr: "orchestrator.execution.poll_interval must be >= 10s",
		},
		{
			name: "orchestrator execution poll_interval = 1s is invalid",
			orchestrator: &OrchestratorConfig{
				MaxConcurrent: 2,
				Execution: &ExecutionConfig{
					Mode:         "sequential",
					PollInterval: 1 * time.Second,
				},
			},
			wantErr:   true,
			errSubstr: "orchestrator.execution.poll_interval must be >= 10s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValidConfig()
			cfg.Orchestrator = tt.orchestrator

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errSubstr)
				} else if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// GH-1128: Integration test that DefaultConfig() passes validation,
// then systematically mutates each validated field to verify errors trigger.
func TestValidate_FullConfig(t *testing.T) {
	// Part 1: DefaultConfig() must pass validation
	t.Run("DefaultConfig passes validation", func(t *testing.T) {
		cfg := DefaultConfig()
		if err := cfg.Validate(); err != nil {
			t.Errorf("DefaultConfig() should pass validation, got error: %v", err)
		}
	})

	// Part 2: Systematically mutate each validated field to verify errors trigger
	tests := []struct {
		name      string
		mutate    func(cfg *Config)
		errSubstr string
	}{
		// Gateway validations
		{
			name: "nil gateway",
			mutate: func(cfg *Config) {
				cfg.Gateway = nil
			},
			errSubstr: "gateway configuration is required",
		},
		{
			name: "gateway port too low",
			mutate: func(cfg *Config) {
				cfg.Gateway.Port = 0
			},
			errSubstr: "invalid gateway port",
		},
		{
			name: "gateway port too high",
			mutate: func(cfg *Config) {
				cfg.Gateway.Port = 70000
			},
			errSubstr: "invalid gateway port",
		},

		// Auth validations
		{
			name: "api-token auth without token",
			mutate: func(cfg *Config) {
				cfg.Auth = &gateway.AuthConfig{
					Type:  gateway.AuthTypeAPIToken,
					Token: "",
				}
			},
			errSubstr: "API token is required when auth type is api-token",
		},

		// EffortRouting validations
		{
			name: "invalid effort routing level",
			mutate: func(cfg *Config) {
				cfg.Executor.EffortRouting = &executor.EffortRoutingConfig{
					Enabled: true,
					Complex: "max", // Invalid - "max" is not supported
				}
			},
			errSubstr: "effort_routing.complex",
		},

		// DefaultProject validation
		{
			name: "default_project not found in projects list",
			mutate: func(cfg *Config) {
				cfg.Projects = []*ProjectConfig{
					{Name: "existing", Path: "/tmp/existing"},
				}
				cfg.DefaultProject = "missing"
			},
			errSubstr: "default_project",
		},

		// Orchestrator validations
		{
			name: "orchestrator max_concurrent < 1",
			mutate: func(cfg *Config) {
				cfg.Orchestrator.MaxConcurrent = 0
			},
			errSubstr: "orchestrator.max_concurrent must be >= 1",
		},
		{
			name: "orchestrator invalid execution mode",
			mutate: func(cfg *Config) {
				cfg.Orchestrator.Execution.Mode = "invalid"
			},
			errSubstr: "orchestrator.execution.mode must be 'sequential' or 'parallel'",
		},
		{
			name: "orchestrator poll_interval too low",
			mutate: func(cfg *Config) {
				cfg.Orchestrator.Execution.PollInterval = 5 * time.Second
			},
			errSubstr: "orchestrator.execution.poll_interval must be >= 10s",
		},

		// Quality validations
		{
			name: "quality max_retries > 10",
			mutate: func(cfg *Config) {
				cfg.Quality.OnFailure.MaxRetries = 11
			},
			errSubstr: "quality.on_failure.max_retries must be in range [0, 10]",
		},
		{
			name: "quality max_retries < 0",
			mutate: func(cfg *Config) {
				cfg.Quality.OnFailure.MaxRetries = -1
			},
			errSubstr: "quality.on_failure.max_retries must be in range [0, 10]",
		},
		{
			name: "invalid quality gate type",
			mutate: func(cfg *Config) {
				cfg.Quality.Gates = []*quality.Gate{
					{Type: "unknown"},
				}
			},
			errSubstr: "quality.gates[0].type must be one of",
		},

		// Budget validations
		{
			name: "budget enabled with zero daily_limit",
			mutate: func(cfg *Config) {
				cfg.Budget.Enabled = true
				cfg.Budget.DailyLimit = 0
			},
			errSubstr: "budget.daily_limit must be > 0 when budget is enabled",
		},
		{
			name: "budget enabled with negative daily_limit",
			mutate: func(cfg *Config) {
				cfg.Budget.Enabled = true
				cfg.Budget.DailyLimit = -10
			},
			errSubstr: "budget.daily_limit must be > 0 when budget is enabled",
		},

		// Alerts validations
		{
			name: "invalid alert channel type",
			mutate: func(cfg *Config) {
				cfg.Alerts.Channels = []AlertChannelConfig{
					{Type: "invalid"},
				}
			},
			errSubstr: "alerts.channels[0].type must be one of",
		},

		// GitHub adapter validations
		{
			name: "GitHub enabled without token",
			mutate: func(cfg *Config) {
				cfg.Adapters.GitHub.Enabled = true
				cfg.Adapters.GitHub.Token = ""
			},
			errSubstr: "adapters.github.token is required",
		},
		{
			name: "GitHub polling interval too low",
			mutate: func(cfg *Config) {
				cfg.Adapters.GitHub.Enabled = true
				cfg.Adapters.GitHub.Token = "test-token"
				cfg.Adapters.GitHub.Polling = &github.PollingConfig{
					Enabled:  true,
					Interval: 5 * time.Second,
				}
			},
			errSubstr: "adapters.github.polling.interval must be >= 10s",
		},

		// Slack adapter validations
		{
			name: "Slack enabled without bot_token",
			mutate: func(cfg *Config) {
				cfg.Adapters.Slack.Enabled = true
				cfg.Adapters.Slack.BotToken = ""
			},
			errSubstr: "adapters.slack.bot_token is required",
		},

		// Telegram adapter validations
		{
			name: "Telegram enabled without bot_token",
			mutate: func(cfg *Config) {
				cfg.Adapters.Telegram.Enabled = true
				cfg.Adapters.Telegram.BotToken = ""
				cfg.Adapters.Telegram.ChatID = "123"
			},
			errSubstr: "adapters.telegram.bot_token is required",
		},
		{
			name: "Telegram enabled without chat_id",
			mutate: func(cfg *Config) {
				cfg.Adapters.Telegram.Enabled = true
				cfg.Adapters.Telegram.BotToken = "test-token"
				cfg.Adapters.Telegram.ChatID = ""
			},
			errSubstr: "adapters.telegram.chat_id is required",
		},

		// Linear adapter validations
		{
			name: "Linear enabled without config",
			mutate: func(cfg *Config) {
				cfg.Adapters.Linear.Enabled = true
				cfg.Adapters.Linear.APIKey = ""
				cfg.Adapters.Linear.TeamID = ""
				cfg.Adapters.Linear.Workspaces = nil
			},
			errSubstr: "adapters.linear must have either api_key+team_id or workspaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Start with a fresh DefaultConfig for each test
			cfg := DefaultConfig()

			// Apply mutation
			tt.mutate(cfg)

			// Validate and expect error
			err := cfg.Validate()
			if err == nil {
				t.Errorf("expected error containing %q, got nil", tt.errSubstr)
			} else if !strings.Contains(err.Error(), tt.errSubstr) {
				t.Errorf("expected error containing %q, got %q", tt.errSubstr, err.Error())
			}
		})
	}
}
