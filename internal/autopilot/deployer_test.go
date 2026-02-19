package autopilot

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alekspetrov/pilot/internal/adapters/github"
	"github.com/alekspetrov/pilot/internal/testutil"
)

func TestDeployer_None(t *testing.T) {
	deployer := NewDeployer(nil, "owner", "repo", slog.Default())

	tests := []struct {
		name   string
		envCfg *EnvironmentConfig
	}{
		{
			name:   "nil PostMerge",
			envCfg: &EnvironmentConfig{PostMerge: nil},
		},
		{
			name:   "action none",
			envCfg: &EnvironmentConfig{PostMerge: &PostMergeConfig{Action: "none"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := deployer.Execute(context.Background(), "dev", tt.envCfg, &PRState{PRNumber: 1})
			if err != nil {
				t.Fatalf("expected nil error, got: %v", err)
			}
		})
	}
}

func TestDeployer_Webhook(t *testing.T) {
	var (
		receivedBody    []byte
		receivedHeaders http.Header
		receivedMethod  string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedHeaders = r.Header.Clone()
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	secret := testutil.FakeWebhookSecret

	deployer := NewDeployer(nil, "owner", "repo", slog.Default())

	envCfg := &EnvironmentConfig{
		PostMerge: &PostMergeConfig{
			Action:     "webhook",
			WebhookURL: server.URL,
			WebhookHeaders: map[string]string{
				"X-Deploy-Env": "staging",
			},
			WebhookSecret: secret,
		},
	}

	prState := &PRState{
		PRNumber:   42,
		BranchName: "pilot/GH-42",
		HeadSHA:    "abc123def456",
	}

	err := deployer.Execute(context.Background(), "stage", envCfg, prState)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// Verify method
	if receivedMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", receivedMethod)
	}

	// Verify body
	var payload WebhookPayload
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if payload.PRNumber != 42 {
		t.Errorf("expected PRNumber 42, got %d", payload.PRNumber)
	}
	if payload.Branch != "pilot/GH-42" {
		t.Errorf("expected branch pilot/GH-42, got %s", payload.Branch)
	}
	if payload.SHA != "abc123def456" {
		t.Errorf("expected SHA abc123def456, got %s", payload.SHA)
	}
	if payload.Environment != "stage" {
		t.Errorf("expected environment stage, got %s", payload.Environment)
	}

	// Verify custom headers
	if receivedHeaders.Get("X-Deploy-Env") != "staging" {
		t.Errorf("expected X-Deploy-Env=staging, got %s", receivedHeaders.Get("X-Deploy-Env"))
	}
	if receivedHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type=application/json, got %s", receivedHeaders.Get("Content-Type"))
	}

	// Verify HMAC signature
	sigHeader := receivedHeaders.Get("X-Hub-Signature-256")
	if sigHeader == "" {
		t.Fatal("expected X-Hub-Signature-256 header")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(receivedBody)
	expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if sigHeader != expectedSig {
		t.Errorf("HMAC mismatch: got %s, expected %s", sigHeader, expectedSig)
	}
}

func TestDeployer_BranchPush(t *testing.T) {
	var (
		requestPath   string
		requestMethod string
		requestBody   map[string]interface{}
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		requestMethod = r.Method

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &requestBody)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	deployer := NewDeployer(client, "owner", "repo", slog.Default())

	envCfg := &EnvironmentConfig{
		PostMerge: &PostMergeConfig{
			Action:       "branch-push",
			DeployBranch: "deploy/production",
		},
	}

	prState := &PRState{
		PRNumber: 99,
		HeadSHA:  "deadbeef1234",
	}

	err := deployer.Execute(context.Background(), "prod", envCfg, prState)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// UpdateRef tries PATCH first
	if requestMethod != http.MethodPatch {
		t.Errorf("expected PATCH, got %s", requestMethod)
	}
	expectedPath := "/repos/owner/repo/git/refs/heads/deploy/production"
	if requestPath != expectedPath {
		t.Errorf("expected path %s, got %s", expectedPath, requestPath)
	}
	if requestBody["sha"] != "deadbeef1234" {
		t.Errorf("expected sha deadbeef1234, got %v", requestBody["sha"])
	}
}

func TestDeployer_Tag(t *testing.T) {
	apiCalls := make(map[string]string) // path -> method

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls[r.URL.Path] = r.Method

		switch {
		case r.URL.Path == "/repos/owner/repo/releases/latest":
			// Return 404 — no releases exist → version starts at 0.0.0
			w.WriteHeader(http.StatusNotFound)
		case r.URL.Path == "/repos/owner/repo/pulls/10/commits":
			// Return a feat commit for minor bump
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"sha":"abc","commit":{"message":"feat(deploy): add auto-deploy"}}]`))
		case r.URL.Path == "/repos/owner/repo/git/refs":
			// Tag creation
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := github.NewClientWithBaseURL(testutil.FakeGitHubToken, server.URL)
	releaser := NewReleaser(client, "owner", "repo", DefaultReleaseConfig())

	deployer := NewDeployer(client, "owner", "repo", slog.Default())
	deployer.SetReleaser(releaser)

	envCfg := &EnvironmentConfig{
		PostMerge: &PostMergeConfig{Action: "tag"},
	}
	prState := &PRState{
		PRNumber: 10,
		HeadSHA:  "abc123",
	}

	err := deployer.Execute(context.Background(), "prod", envCfg, prState)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// Verify tag creation API was called
	if _, ok := apiCalls["/repos/owner/repo/git/refs"]; !ok {
		t.Error("expected tag creation API call to /repos/owner/repo/git/refs")
	}
	// Verify PR commits were fetched for bump detection
	if _, ok := apiCalls["/repos/owner/repo/pulls/10/commits"]; !ok {
		t.Error("expected PR commits API call")
	}
}

func TestDeployer_Tag_NoReleaser(t *testing.T) {
	deployer := NewDeployer(nil, "owner", "repo", slog.Default())

	envCfg := &EnvironmentConfig{
		PostMerge: &PostMergeConfig{Action: "tag"},
	}

	err := deployer.Execute(context.Background(), "prod", envCfg, &PRState{PRNumber: 1})
	if err == nil {
		t.Fatal("expected error when releaser is nil")
	}
	if err.Error() != "releaser not configured for tag action" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDeployer_UnknownAction(t *testing.T) {
	deployer := NewDeployer(nil, "owner", "repo", slog.Default())

	envCfg := &EnvironmentConfig{
		PostMerge: &PostMergeConfig{Action: "deploy-spaceship"},
	}

	err := deployer.Execute(context.Background(), "prod", envCfg, &PRState{PRNumber: 1})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	expected := "unknown post-merge action: deploy-spaceship"
	if err.Error() != expected {
		t.Errorf("expected error %q, got %q", expected, err.Error())
	}
}
