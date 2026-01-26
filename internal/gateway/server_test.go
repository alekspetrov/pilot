package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewServer(t *testing.T) {
	config := &Config{
		Host: "127.0.0.1",
		Port: 9090,
	}

	server := NewServer(config)

	if server == nil {
		t.Fatal("NewServer returned nil")
	}
	if server.config != config {
		t.Error("Server config not set correctly")
	}
	if server.sessions == nil {
		t.Error("Sessions manager not initialized")
	}
	if server.router == nil {
		t.Error("Router not initialized")
	}
}

func TestHealthEndpoint(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]string
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["status"] != "healthy" {
		t.Errorf("Expected status 'healthy', got '%s'", response["status"])
	}
}

func TestStatusEndpoint(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	w := httptest.NewRecorder()

	server.handleStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["version"] != "0.1.0" {
		t.Errorf("Expected version '0.1.0', got '%v'", response["version"])
	}
}

func TestLinearWebhook(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	// Test invalid method
	req := httptest.NewRequest(http.MethodGet, "/webhooks/linear", nil)
	w := httptest.NewRecorder()
	server.handleLinearWebhook(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 for GET, got %d", w.Code)
	}

	// Test valid POST
	payload := `{"action": "create", "type": "Issue", "data": {}}`
	req = httptest.NewRequest(http.MethodPost, "/webhooks/linear", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.handleLinearWebhook(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for POST, got %d", w.Code)
	}

	// Test invalid JSON
	req = httptest.NewRequest(http.MethodPost, "/webhooks/linear", strings.NewReader("invalid"))
	w = httptest.NewRecorder()
	server.handleLinearWebhook(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestServerStartStop(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 19090} // Use different port for test
	server := NewServer(config)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(ctx)
	}()

	// Wait for context cancellation
	<-ctx.Done()

	// Server should shutdown gracefully
	select {
	case err := <-errCh:
		if err != nil {
			t.Logf("Server returned: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Server did not shut down in time")
	}
}

func TestRouter(t *testing.T) {
	config := &Config{Host: "127.0.0.1", Port: 9090}
	server := NewServer(config)

	router := server.Router()
	if router == nil {
		t.Error("Router() returned nil")
	}
}
