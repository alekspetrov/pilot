package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alekspetrov/pilot/internal/adapters/github"
	"github.com/alekspetrov/pilot/internal/autopilot"
	"github.com/alekspetrov/pilot/internal/config"
	"github.com/spf13/cobra"
)

func newAutopilotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "autopilot",
		Short: "Autopilot status and management",
		Long: `View and manage autopilot state for PR lifecycle automation.

Autopilot monitors PRs through stages: PRCreated → WaitingCI → CIPassed → Merging → Merged
In prod environment, an approval stage is added before merging.`,
	}

	cmd.AddCommand(
		newAutopilotStatusCmd(),
	)

	return cmd
}

// PRStatusInfo holds PR info for display/JSON output
type PRStatusInfo struct {
	PRNumber        int           `json:"pr_number"`
	PRURL           string        `json:"pr_url"`
	IssueNumber     int           `json:"issue_number,omitempty"`
	HeadSHA         string        `json:"head_sha"`
	Stage           string        `json:"stage"`
	CIStatus        string        `json:"ci_status"`
	TimeInStage     string        `json:"time_in_stage"`
	TimeInStageMs   int64         `json:"time_in_stage_ms"`
	CreatedAt       string        `json:"created_at"`
	LastChecked     time.Time     `json:"last_checked,omitempty"`
	MergeAttempts   int           `json:"merge_attempts,omitempty"`
	Error           string        `json:"error,omitempty"`
	ReleaseVersion  string        `json:"release_version,omitempty"`
	ReleaseBumpType string        `json:"release_bump_type,omitempty"`
	CIChecks        []CICheckInfo `json:"ci_checks,omitempty"`
}

// CICheckInfo holds CI check run info
type CICheckInfo struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion,omitempty"`
}

// AutopilotStatusOutput is the full status output
type AutopilotStatusOutput struct {
	Environment         string         `json:"environment"`
	Enabled             bool           `json:"enabled"`
	AutoMerge           bool           `json:"auto_merge"`
	MergeMethod         string         `json:"merge_method"`
	CIPollInterval      string         `json:"ci_poll_interval"`
	CIWaitTimeout       string         `json:"ci_wait_timeout"`
	ReleaseEnabled      bool           `json:"release_enabled"`
	ReleaseTrigger      string         `json:"release_trigger,omitempty"`
	CircuitBreakerOpen  bool           `json:"circuit_breaker_open"`
	ConsecutiveFailures int            `json:"consecutive_failures"`
	MaxFailures         int            `json:"max_failures"`
	ActivePRs           []PRStatusInfo `json:"active_prs"`
}

func newAutopilotStatusCmd() *cobra.Command {
	var (
		outputJSON bool
		verbose    bool
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show autopilot status and tracked PRs",
		Long: `Display the current status of the autopilot controller.

Shows:
- Environment and configuration
- Circuit breaker status
- All tracked PRs and their current stage
- CI status for each PR

Use --verbose to fetch live CI check details from GitHub.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			configPath := cfgFile
			if configPath == "" {
				configPath = config.DefaultConfigPath()
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Check if autopilot is configured
			if cfg.Orchestrator.Autopilot == nil || !cfg.Orchestrator.Autopilot.Enabled {
				if outputJSON {
					output := AutopilotStatusOutput{Enabled: false}
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(output)
				}
				fmt.Println("⚠️  Autopilot is not enabled in configuration")
				fmt.Println("   Enable with: --autopilot=<env> flag or config file")
				return nil
			}

			autoCfg := cfg.Orchestrator.Autopilot

			// For verbose mode, we need GitHub client to fetch CI details
			var ghClient *github.Client
			var owner, repo string
			if verbose || outputJSON {
				if cfg.Adapters.GitHub != nil {
					ghToken := cfg.Adapters.GitHub.Token
					if ghToken == "" {
						ghToken = os.Getenv("GITHUB_TOKEN")
					}
					if ghToken != "" && cfg.Adapters.GitHub.Repo != "" {
						parts := strings.SplitN(cfg.Adapters.GitHub.Repo, "/", 2)
						if len(parts) == 2 {
							ghClient = github.NewClient(ghToken)
							owner = parts[0]
							repo = parts[1]
						}
					}
				}
			}

			// Build output structure
			output := AutopilotStatusOutput{
				Environment:         string(autoCfg.Environment),
				Enabled:             autoCfg.Enabled,
				AutoMerge:           autoCfg.AutoMerge,
				MergeMethod:         autoCfg.MergeMethod,
				CIPollInterval:      autoCfg.CIPollInterval.String(),
				CIWaitTimeout:       autoCfg.CIWaitTimeout.String(),
				ReleaseEnabled:      autoCfg.Release != nil && autoCfg.Release.Enabled,
				CircuitBreakerOpen:  false, // Can't determine without running controller
				ConsecutiveFailures: 0,
				MaxFailures:         autoCfg.MaxFailures,
				ActivePRs:           []PRStatusInfo{},
			}

			if autoCfg.Release != nil && autoCfg.Release.Enabled {
				output.ReleaseTrigger = autoCfg.Release.Trigger
			}

			// If GitHub client available, scan for tracked PRs
			if ghClient != nil {
				prs, err := ghClient.ListPullRequests(cmd.Context(), owner, repo, "open")
				if err != nil {
					if !outputJSON {
						fmt.Printf("⚠️  Warning: Could not fetch PRs from GitHub: %v\n", err)
					}
				} else {
					for _, pr := range prs {
						// Filter for Pilot branches
						if !strings.HasPrefix(pr.Head.Ref, "pilot/GH-") {
							continue
						}

						// Extract issue number from branch name
						var issueNum int
						if _, err := fmt.Sscanf(pr.Head.Ref, "pilot/GH-%d", &issueNum); err != nil {
							continue
						}

						prInfo := PRStatusInfo{
							PRNumber:    pr.Number,
							PRURL:       pr.HTMLURL,
							IssueNumber: issueNum,
							HeadSHA:     pr.Head.SHA,
							Stage:       determineStage(pr),
							CIStatus:    "unknown",
							CreatedAt:   pr.CreatedAt,
						}

						// Fetch CI status if verbose
						if verbose || outputJSON {
							checkRuns, err := ghClient.ListCheckRuns(cmd.Context(), owner, repo, pr.Head.SHA)
							if err == nil {
								prInfo.CIStatus = aggregateCIStatus(checkRuns)
								for _, check := range checkRuns.CheckRuns {
									prInfo.CIChecks = append(prInfo.CIChecks, CICheckInfo{
										Name:       check.Name,
										Status:     check.Status,
										Conclusion: check.Conclusion,
									})
								}
							}
						}

						output.ActivePRs = append(output.ActivePRs, prInfo)
					}
				}
			}

			// Output
			if outputJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(output)
			}

			// Pretty print
			fmt.Println()
			fmt.Println("🤖 Autopilot Status")
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Println()

			fmt.Println("📋 Configuration")
			fmt.Printf("   Environment ......... %s\n", output.Environment)
			fmt.Printf("   Auto-Merge .......... %v\n", output.AutoMerge)
			fmt.Printf("   Merge Method ........ %s\n", output.MergeMethod)
			fmt.Printf("   CI Poll Interval .... %s\n", output.CIPollInterval)
			fmt.Printf("   CI Timeout .......... %s\n", output.CIWaitTimeout)
			fmt.Println()

			if output.ReleaseEnabled {
				fmt.Println("📦 Release")
				fmt.Printf("   Enabled ............. true\n")
				fmt.Printf("   Trigger ............. %s\n", output.ReleaseTrigger)
				fmt.Println()
			}

			fmt.Println("🔒 Circuit Breaker")
			fmt.Printf("   Max Failures ........ %d\n", output.MaxFailures)
			fmt.Println()

			fmt.Println("📊 Active PRs")
			if len(output.ActivePRs) == 0 {
				fmt.Println("   No active Pilot PRs found")
			} else {
				fmt.Printf("   Count ............... %d\n\n", len(output.ActivePRs))
				for _, pr := range output.ActivePRs {
					icon := stageIcon(pr.Stage)
					fmt.Printf("   %s #%d: %s\n", icon, pr.PRNumber, pr.Stage)
					fmt.Printf("      Issue: #%d | SHA: %s\n", pr.IssueNumber, pr.HeadSHA[:7])
					fmt.Printf("      CI: %s | Created: %s\n", pr.CIStatus, pr.CreatedAt)
					if verbose && len(pr.CIChecks) > 0 {
						fmt.Println("      Checks:")
						for _, check := range pr.CIChecks {
							checkIcon := "⏳"
							if check.Status == "completed" {
								if check.Conclusion == "success" {
									checkIcon = "✅"
								} else if check.Conclusion == "failure" {
									checkIcon = "❌"
								} else if check.Conclusion == "skipped" {
									checkIcon = "⏭️"
								}
							}
							fmt.Printf("         %s %s: %s/%s\n", checkIcon, check.Name, check.Status, check.Conclusion)
						}
					}
					fmt.Println()
				}
			}

			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&outputJSON, "json", "j", false, "Output in JSON format")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed CI check information")

	return cmd
}

// determineStage determines the autopilot stage based on PR state
func determineStage(pr *github.PullRequest) string {
	if pr.Merged {
		return string(autopilot.StageMerged)
	}
	if pr.State == "closed" {
		return "closed"
	}
	// Can't fully determine stage without CI info, default to waiting
	return string(autopilot.StageWaitingCI)
}

// aggregateCIStatus determines overall CI status from check runs
func aggregateCIStatus(checkRuns *github.CheckRunsResponse) string {
	if checkRuns.TotalCount == 0 {
		return string(autopilot.CIPending)
	}

	hasFailure := false
	hasPending := false

	for _, run := range checkRuns.CheckRuns {
		switch run.Status {
		case github.CheckRunQueued, github.CheckRunInProgress:
			hasPending = true
		case github.CheckRunCompleted:
			switch run.Conclusion {
			case github.ConclusionFailure, github.ConclusionCancelled, github.ConclusionTimedOut:
				hasFailure = true
			}
		}
	}

	if hasFailure {
		return string(autopilot.CIFailure)
	}
	if hasPending {
		return string(autopilot.CIRunning)
	}
	return string(autopilot.CISuccess)
}

// stageIcon returns an emoji icon for the PR stage
func stageIcon(stage string) string {
	switch autopilot.PRStage(stage) {
	case autopilot.StagePRCreated:
		return "📝"
	case autopilot.StageWaitingCI:
		return "⏳"
	case autopilot.StageCIPassed:
		return "✅"
	case autopilot.StageCIFailed:
		return "❌"
	case autopilot.StageAwaitApproval:
		return "👤"
	case autopilot.StageMerging:
		return "🔀"
	case autopilot.StageMerged:
		return "✅"
	case autopilot.StagePostMergeCI:
		return "🚀"
	case autopilot.StageFailed:
		return "💥"
	default:
		return "❓"
	}
}
