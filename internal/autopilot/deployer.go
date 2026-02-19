package autopilot

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/alekspetrov/pilot/internal/adapters/github"
)

// Deployer executes post-merge deployment actions (webhook, branch-push, tag).
type Deployer struct {
	ghClient   *github.Client
	releaser   *Releaser
	owner      string
	repo       string
	log        *slog.Logger
	httpClient *http.Client
}

// NewDeployer creates a new post-merge deployer.
func NewDeployer(ghClient *github.Client, owner, repo string, log *slog.Logger) *Deployer {
	return &Deployer{
		ghClient: ghClient,
		owner:    owner,
		repo:     repo,
		log:      log,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetReleaser sets the releaser for tag action delegation.
func (d *Deployer) SetReleaser(r *Releaser) {
	d.releaser = r
}

// WebhookPayload is the JSON body sent to webhook URLs on post-merge.
type WebhookPayload struct {
	PRNumber    int    `json:"pr_number"`
	Branch      string `json:"branch"`
	SHA         string `json:"sha"`
	Environment string `json:"environment"`
}

// Execute runs the post-merge action for the given environment config.
func (d *Deployer) Execute(ctx context.Context, envName string, envCfg *EnvironmentConfig, prState *PRState) error {
	if envCfg.PostMerge == nil || envCfg.PostMerge.Action == "none" {
		return nil
	}

	d.log.Info("executing post-merge action",
		"action", envCfg.PostMerge.Action,
		"pr", prState.PRNumber,
		"env", envName,
	)

	switch envCfg.PostMerge.Action {
	case "tag":
		return d.createTag(ctx, prState)
	case "webhook":
		return d.callWebhook(ctx, envName, envCfg.PostMerge, prState)
	case "branch-push":
		return d.pushToBranch(ctx, envCfg.PostMerge, prState)
	default:
		return fmt.Errorf("unknown post-merge action: %s", envCfg.PostMerge.Action)
	}
}

// createTag delegates to the releaser to create a version tag.
func (d *Deployer) createTag(ctx context.Context, prState *PRState) error {
	if d.releaser == nil {
		return fmt.Errorf("releaser not configured for tag action")
	}

	currentVersion, err := d.releaser.GetCurrentVersion(ctx)
	if err != nil {
		d.log.Warn("failed to get current version, defaulting to 0.0.0", "error", err)
		currentVersion = SemVer{}
	}

	// Get PR commits for bump type detection
	commits, err := d.ghClient.GetPRCommits(ctx, d.owner, d.repo, prState.PRNumber)
	if err != nil {
		return fmt.Errorf("failed to get PR commits: %w", err)
	}

	bumpType := DetectBumpType(commits)
	if bumpType == BumpNone {
		bumpType = BumpPatch // Default to patch if no conventional commits
	}

	newVersion := currentVersion.Bump(bumpType)
	tagName, err := d.releaser.CreateTag(ctx, prState, newVersion)
	if err != nil {
		return fmt.Errorf("failed to create tag: %w", err)
	}

	d.log.Info("post-merge tag created", "tag", tagName, "pr", prState.PRNumber)
	return nil
}

// callWebhook sends an HTTP POST to the configured webhook URL.
func (d *Deployer) callWebhook(ctx context.Context, envName string, cfg *PostMergeConfig, prState *PRState) error {
	payload := WebhookPayload{
		PRNumber:    prState.PRNumber,
		Branch:      prState.BranchName,
		SHA:         prState.HeadSHA,
		Environment: envName,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add custom headers
	for k, v := range cfg.WebhookHeaders {
		req.Header.Set(k, v)
	}

	// Add HMAC signature if secret is configured
	if cfg.WebhookSecret != "" {
		mac := hmac.New(sha256.New, []byte(cfg.WebhookSecret))
		mac.Write(body)
		sig := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Hub-Signature-256", "sha256="+sig)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}

	d.log.Info("post-merge webhook called", "url", cfg.WebhookURL, "status", resp.StatusCode, "pr", prState.PRNumber)
	return nil
}

// pushToBranch creates or updates a deploy branch to point at the merged PR's SHA.
func (d *Deployer) pushToBranch(ctx context.Context, cfg *PostMergeConfig, prState *PRState) error {
	if cfg.DeployBranch == "" {
		return fmt.Errorf("deploy_branch not configured for branch-push action")
	}

	if err := d.ghClient.UpdateRef(ctx, d.owner, d.repo, cfg.DeployBranch, prState.HeadSHA); err != nil {
		return fmt.Errorf("failed to push to branch %s: %w", cfg.DeployBranch, err)
	}

	d.log.Info("post-merge branch push", "branch", cfg.DeployBranch, "sha", ShortSHA(prState.HeadSHA), "pr", prState.PRNumber)
	return nil
}
