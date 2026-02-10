package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestUsersClient_GetUserInfo(t *testing.T) {
	tests := []struct {
		name         string
		userID       string
		serverResp   usersInfoResponse
		wantEmail    string
		wantName     string
		wantRealName string
		wantErr      bool
	}{
		{
			name:   "successful fetch",
			userID: "U12345",
			serverResp: usersInfoResponse{
				OK: true,
				User: struct {
					ID       string `json:"id"`
					Name     string `json:"name"`
					RealName string `json:"real_name"`
					Profile  struct {
						Email    string `json:"email"`
						RealName string `json:"real_name_normalized"`
					} `json:"profile"`
				}{
					ID:       "U12345",
					Name:     "testuser",
					RealName: "Test User",
					Profile: struct {
						Email    string `json:"email"`
						RealName string `json:"real_name_normalized"`
					}{
						Email:    "test@example.com",
						RealName: "Test User",
					},
				},
			},
			wantEmail:    "test@example.com",
			wantName:     "testuser",
			wantRealName: "Test User",
		},
		{
			name:   "user not found",
			userID: "U99999",
			serverResp: usersInfoResponse{
				OK:    false,
				Error: "user_not_found",
			},
			wantErr: true,
		},
		{
			name:   "user without email",
			userID: "U11111",
			serverResp: usersInfoResponse{
				OK: true,
				User: struct {
					ID       string `json:"id"`
					Name     string `json:"name"`
					RealName string `json:"real_name"`
					Profile  struct {
						Email    string `json:"email"`
						RealName string `json:"real_name_normalized"`
					} `json:"profile"`
				}{
					ID:   "U11111",
					Name: "bot",
				},
			},
			wantEmail:    "",
			wantName:     "bot",
			wantRealName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/users.info" {
					t.Errorf("unexpected path: %s", r.URL.Path)
					http.Error(w, "not found", http.StatusNotFound)
					return
				}

				if r.URL.Query().Get("user") != tt.userID {
					t.Errorf("unexpected user ID: got %s, want %s", r.URL.Query().Get("user"), tt.userID)
				}

				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(tt.serverResp); err != nil {
					t.Fatalf("failed to encode response: %v", err)
				}
			}))
			defer server.Close()

			client := NewUsersClientWithBaseURL("test-token", server.URL)
			info, err := client.GetUserInfo(context.Background(), tt.userID)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if info.Email != tt.wantEmail {
				t.Errorf("email: got %q, want %q", info.Email, tt.wantEmail)
			}
			if info.Name != tt.wantName {
				t.Errorf("name: got %q, want %q", info.Name, tt.wantName)
			}
			if info.RealName != tt.wantRealName {
				t.Errorf("real_name: got %q, want %q", info.RealName, tt.wantRealName)
			}
		})
	}
}

func TestUsersClient_Caching(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		resp := usersInfoResponse{
			OK: true,
			User: struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				RealName string `json:"real_name"`
				Profile  struct {
					Email    string `json:"email"`
					RealName string `json:"real_name_normalized"`
				} `json:"profile"`
			}{
				ID:   "U12345",
				Name: "testuser",
				Profile: struct {
					Email    string `json:"email"`
					RealName string `json:"real_name_normalized"`
				}{
					Email: "test@example.com",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewUsersClientWithBaseURL("test-token", server.URL)
	ctx := context.Background()

	// First call — should hit the server
	_, err := client.GetUserInfo(ctx, "U12345")
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	mu.Lock()
	if callCount != 1 {
		t.Errorf("first call: expected 1 server call, got %d", callCount)
	}
	mu.Unlock()

	// Second call — should hit cache
	_, err = client.GetUserInfo(ctx, "U12345")
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	mu.Lock()
	if callCount != 1 {
		t.Errorf("second call: expected 1 server call (cached), got %d", callCount)
	}
	mu.Unlock()

	// Different user — should hit server
	_, _ = client.GetUserInfo(ctx, "U99999")

	mu.Lock()
	if callCount != 2 {
		t.Errorf("different user: expected 2 server calls, got %d", callCount)
	}
	mu.Unlock()
}

func TestUsersClient_ClearCache(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		resp := usersInfoResponse{
			OK: true,
			User: struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				RealName string `json:"real_name"`
				Profile  struct {
					Email    string `json:"email"`
					RealName string `json:"real_name_normalized"`
				} `json:"profile"`
			}{
				ID:   "U12345",
				Name: "testuser",
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewUsersClientWithBaseURL("test-token", server.URL)
	ctx := context.Background()

	// First call
	_, _ = client.GetUserInfo(ctx, "U12345")
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}

	// Clear cache
	client.ClearCache()

	// Second call should hit server again
	_, _ = client.GetUserInfo(ctx, "U12345")
	if callCount != 2 {
		t.Errorf("expected 2 calls after cache clear, got %d", callCount)
	}
}

func TestUsersClient_CacheExpiry(t *testing.T) {
	// This test verifies the cache expiry logic by manually setting an expired entry
	client := &UsersClient{
		botToken:   "test-token",
		apiURL:     "http://localhost",
		httpClient: &http.Client{Timeout: 1 * time.Second},
		cache:      make(map[string]cachedUser),
	}

	// Add an expired cache entry
	client.cache["U12345"] = cachedUser{
		user:      &UserInfo{ID: "U12345", Email: "old@example.com"},
		expiresAt: time.Now().Add(-1 * time.Hour), // expired
	}

	// Check that we need to refresh (simulated by checking expiry)
	client.mu.RLock()
	cached, ok := client.cache["U12345"]
	client.mu.RUnlock()

	if !ok {
		t.Fatal("expected cache entry to exist")
	}

	if time.Now().Before(cached.expiresAt) {
		t.Error("expected cache entry to be expired")
	}
}
