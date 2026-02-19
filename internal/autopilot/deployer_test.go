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
)

func TestDeployer_None(t *testing.T) {
	d := NewDeployer(nil, "owner", "repo", slog.Default())
	pr := &PRState{PRNumber: 1}

	// nil PostMerge
	err := d.Execute(context.Background(), &EnvironmentConfig{}, pr)
	if err != nil {
		t.Fatalf("expected nil error for nil PostMerge, got %v", err)
	}

	// action "none"
	err = d.Execute(context.Background(), &EnvironmentConfig{
		PostMerge: &PostMergeConfig{Action: "none"},
	}, pr)
	if err != nil {
		t.Fatalf("expected nil error for action none, got %v", err)
	}
}

func TestDeployer_Webhook(t *testing.T) {
	var received WebhookPayload
	var headers http.Header
	var sigHeader string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header
		sigHeader = r.Header.Get("X-Signature-256")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDeployer(nil, "owner", "repo", slog.Default())

	pr := &PRState{
		PRNumber:        42,
		BranchName:      "feat/test",
		HeadSHA:         "abc123",
		EnvironmentName: "staging",
	}

	secret := "test-secret"
	env := &EnvironmentConfig{
		PostMerge: &PostMergeConfig{
			Action:         "webhook",
			WebhookURL:     srv.URL,
			WebhookSecret:  secret,
			WebhookHeaders: map[string]string{"X-Custom": "value"},
		},
	}

	err := d.Execute(context.Background(), env, pr)
	if err != nil {
		t.Fatalf("webhook execute failed: %v", err)
	}

	// Verify payload
	if received.PRNumber != 42 {
		t.Errorf("expected PRNumber 42, got %d", received.PRNumber)
	}
	if received.Branch != "feat/test" {
		t.Errorf("expected branch feat/test, got %s", received.Branch)
	}
	if received.SHA != "abc123" {
		t.Errorf("expected SHA abc123, got %s", received.SHA)
	}
	if received.Environment != "staging" {
		t.Errorf("expected environment staging, got %s", received.Environment)
	}

	// Verify custom header
	if headers.Get("X-Custom") != "value" {
		t.Errorf("expected X-Custom header, got %q", headers.Get("X-Custom"))
	}

	// Verify HMAC signature
	payload, _ := json.Marshal(WebhookPayload{
		PRNumber:    42,
		Branch:      "feat/test",
		SHA:         "abc123",
		Environment: "staging",
	})
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if sigHeader != expectedSig {
		t.Errorf("HMAC mismatch: got %q, want %q", sigHeader, expectedSig)
	}
}

func TestDeployer_BranchPush(t *testing.T) {
	var patchCalled, postCalled bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/git/refs/heads/deploy":
			patchCalled = true
			w.WriteHeader(http.StatusOK)
		case "/repos/owner/repo/git/refs":
			postCalled = true
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	ghClient := github.NewClientWithBaseURL("fake-token", srv.URL)
	d := NewDeployer(ghClient, "owner", "repo", slog.Default())

	pr := &PRState{
		PRNumber:        10,
		HeadSHA:         "def456",
		EnvironmentName: "prod",
	}

	env := &EnvironmentConfig{
		PostMerge: &PostMergeConfig{
			Action:       "branch-push",
			DeployBranch: "deploy",
		},
	}

	err := d.Execute(context.Background(), env, pr)
	if err != nil {
		t.Fatalf("branch-push execute failed: %v", err)
	}

	if !patchCalled && !postCalled {
		t.Error("expected either PATCH or POST to be called")
	}
}

func TestDeployer_Tag(t *testing.T) {
	d := NewDeployer(nil, "owner", "repo", slog.Default())
	pr := &PRState{PRNumber: 5}

	env := &EnvironmentConfig{
		PostMerge: &PostMergeConfig{Action: "tag"},
	}

	// Tag action is a no-op in deployer (handled by releaser)
	err := d.Execute(context.Background(), env, pr)
	if err != nil {
		t.Fatalf("tag action should be no-op, got %v", err)
	}
}

func TestDeployer_UnknownAction(t *testing.T) {
	d := NewDeployer(nil, "owner", "repo", slog.Default())
	pr := &PRState{PRNumber: 1}

	env := &EnvironmentConfig{
		PostMerge: &PostMergeConfig{Action: "invalid"},
	}

	err := d.Execute(context.Background(), env, pr)
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if err.Error() != "unknown post-merge action: invalid" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDeployer_BranchPush_NoBranch(t *testing.T) {
	d := NewDeployer(nil, "owner", "repo", slog.Default())
	pr := &PRState{PRNumber: 1}

	env := &EnvironmentConfig{
		PostMerge: &PostMergeConfig{
			Action:       "branch-push",
			DeployBranch: "",
		},
	}

	err := d.Execute(context.Background(), env, pr)
	if err == nil {
		t.Fatal("expected error for empty deploy branch")
	}
}
