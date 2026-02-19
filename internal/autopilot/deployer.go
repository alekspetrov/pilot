package autopilot

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/alekspetrov/pilot/internal/adapters/github"
)

// Deployer executes post-merge deployment actions based on environment config.
type Deployer struct {
	ghClient   *github.Client
	owner      string
	repo       string
	log        *slog.Logger
	httpClient *http.Client
}

// NewDeployer creates a new post-merge deployer.
func NewDeployer(ghClient *github.Client, owner, repo string, log *slog.Logger) *Deployer {
	return &Deployer{
		ghClient:   ghClient,
		owner:      owner,
		repo:       repo,
		log:        log,
		httpClient: &http.Client{},
	}
}

// WebhookPayload is the JSON body sent to webhook endpoints.
type WebhookPayload struct {
	PRNumber    int    `json:"pr_number"`
	Branch      string `json:"branch"`
	SHA         string `json:"sha"`
	Environment string `json:"environment"`
}

// Execute runs the post-merge action for the given environment config.
func (d *Deployer) Execute(ctx context.Context, envCfg *EnvironmentConfig, prState *PRState) error {
	if envCfg.PostMerge == nil || envCfg.PostMerge.Action == "none" {
		return nil
	}

	switch envCfg.PostMerge.Action {
	case "tag":
		// Tag action is handled by the Releaser; deployer is a no-op for tags.
		d.log.Info("post-merge action: tag (handled by releaser)", "pr", prState.PRNumber)
		return nil
	case "webhook":
		return d.callWebhook(ctx, envCfg.PostMerge, prState)
	case "branch-push":
		return d.pushToBranch(ctx, envCfg.PostMerge, prState)
	default:
		return fmt.Errorf("unknown post-merge action: %s", envCfg.PostMerge.Action)
	}
}

// callWebhook sends an HTTP POST to the configured webhook URL.
func (d *Deployer) callWebhook(ctx context.Context, cfg *PostMergeConfig, prState *PRState) error {
	payload := WebhookPayload{
		PRNumber:    prState.PRNumber,
		Branch:      prState.BranchName,
		SHA:         prState.HeadSHA,
		Environment: prState.EnvironmentName,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add custom headers
	for k, v := range cfg.WebhookHeaders {
		req.Header.Set(k, v)
	}

	// HMAC signature if secret is configured
	if cfg.WebhookSecret != "" {
		mac := hmac.New(sha256.New, []byte(cfg.WebhookSecret))
		mac.Write(body)
		sig := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Signature-256", "sha256="+sig)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	d.log.Info("post-merge webhook delivered",
		"pr", prState.PRNumber,
		"url", cfg.WebhookURL,
		"status", resp.StatusCode,
	)
	return nil
}

// pushToBranch creates or updates a deploy branch ref via the GitHub API.
func (d *Deployer) pushToBranch(ctx context.Context, cfg *PostMergeConfig, prState *PRState) error {
	if cfg.DeployBranch == "" {
		return fmt.Errorf("deploy_branch not configured for branch-push action")
	}

	err := d.ghClient.UpdateRef(ctx, d.owner, d.repo, cfg.DeployBranch, prState.HeadSHA)
	if err != nil {
		return fmt.Errorf("push to branch %s: %w", cfg.DeployBranch, err)
	}

	d.log.Info("post-merge branch-push complete",
		"pr", prState.PRNumber,
		"branch", cfg.DeployBranch,
		"sha", ShortSHA(prState.HeadSHA),
	)
	return nil
}
