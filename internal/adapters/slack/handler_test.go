package slack

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockMemberResolver implements MemberResolver for testing.
type mockMemberResolver struct {
	mappings map[string]string // slackUserID or email -> memberID
}

func (m *mockMemberResolver) ResolveSlackIdentity(slackUserID, email string) (string, error) {
	if m.mappings == nil {
		return "", nil
	}
	if slackUserID != "" {
		if id, ok := m.mappings[slackUserID]; ok {
			return id, nil
		}
	}
	if email != "" {
		if id, ok := m.mappings[email]; ok {
			return id, nil
		}
	}
	return "", nil
}

// mockTeamChecker implements TeamChecker for testing.
type mockTeamChecker struct {
	permissions map[string]map[string]bool // memberID -> perm -> allowed
}

func (m *mockTeamChecker) CheckPermission(memberID string, perm string) error {
	if m.permissions == nil {
		return errors.New("permission denied")
	}
	if perms, ok := m.permissions[memberID]; ok {
		if perms[perm] {
			return nil
		}
	}
	return errors.New("permission denied")
}

func TestHandler_TrackSender(t *testing.T) {
	h := NewHandler(&HandlerConfig{
		AppToken: "xapp-test-token",
	})

	// Track a sender
	h.TrackSender("C12345", "U67890")

	// Verify tracking
	got := h.GetLastSender("C12345")
	if got != "U67890" {
		t.Errorf("GetLastSender() = %q, want %q", got, "U67890")
	}

	// Empty channel/user should not track
	h.TrackSender("", "U11111")
	h.TrackSender("C22222", "")

	if h.GetLastSender("") != "" {
		t.Error("empty channel should not be tracked")
	}
	if h.GetLastSender("C22222") != "" {
		t.Error("empty user should not be tracked")
	}
}

func TestHandler_TrackSender_OverwritesPrevious(t *testing.T) {
	h := NewHandler(&HandlerConfig{
		AppToken: "xapp-test-token",
	})

	h.TrackSender("C12345", "U11111")
	h.TrackSender("C12345", "U22222")

	got := h.GetLastSender("C12345")
	if got != "U22222" {
		t.Errorf("GetLastSender() = %q, want %q (should overwrite)", got, "U22222")
	}
}

func TestHandler_ResolveMemberID_NoResolver(t *testing.T) {
	h := NewHandler(&HandlerConfig{
		AppToken: "xapp-test-token",
		// No MemberResolver
	})

	h.TrackSender("C12345", "U67890")

	// Without resolver, should return empty string
	got := h.resolveMemberID("C12345")
	if got != "" {
		t.Errorf("resolveMemberID() without resolver = %q, want empty", got)
	}
}

func TestHandler_ResolveMemberID_WithResolver(t *testing.T) {
	resolver := &mockMemberResolver{
		mappings: map[string]string{
			"U67890": "member-alice",
			"U11111": "member-bob",
		},
	}

	h := NewHandler(&HandlerConfig{
		AppToken:       "xapp-test-token",
		MemberResolver: resolver,
	})

	// Track and resolve
	h.TrackSender("C12345", "U67890")
	got := h.resolveMemberID("C12345")
	if got != "member-alice" {
		t.Errorf("resolveMemberID() = %q, want %q", got, "member-alice")
	}

	// Different channel, different user
	h.TrackSender("C99999", "U11111")
	got = h.resolveMemberID("C99999")
	if got != "member-bob" {
		t.Errorf("resolveMemberID() = %q, want %q", got, "member-bob")
	}
}

func TestHandler_ResolveMemberID_UnknownUser(t *testing.T) {
	resolver := &mockMemberResolver{
		mappings: map[string]string{
			"U67890": "member-alice",
		},
	}

	h := NewHandler(&HandlerConfig{
		AppToken:       "xapp-test-token",
		MemberResolver: resolver,
	})

	h.TrackSender("C12345", "U99999") // Unknown user
	got := h.resolveMemberID("C12345")
	if got != "" {
		t.Errorf("resolveMemberID() for unknown user = %q, want empty", got)
	}
}

func TestHandler_ResolveMemberID_NoSenderTracked(t *testing.T) {
	resolver := &mockMemberResolver{
		mappings: map[string]string{
			"U67890": "member-alice",
		},
	}

	h := NewHandler(&HandlerConfig{
		AppToken:       "xapp-test-token",
		MemberResolver: resolver,
	})

	// No sender tracked for this channel
	got := h.resolveMemberID("C12345")
	if got != "" {
		t.Errorf("resolveMemberID() with no sender = %q, want empty", got)
	}
}

func TestNewHandler(t *testing.T) {
	resolver := &mockMemberResolver{}

	h := NewHandler(&HandlerConfig{
		AppToken:       "xapp-test-token",
		MemberResolver: resolver,
	})

	if h.client == nil {
		t.Error("NewHandler() should initialize client")
	}
	if h.memberResolver != resolver {
		t.Error("NewHandler() should set memberResolver from config")
	}
	if h.lastSender == nil {
		t.Error("NewHandler() should initialize lastSender map")
	}
	if h.log == nil {
		t.Error("NewHandler() should initialize logger")
	}
}

func TestNewHandler_WithBotToken(t *testing.T) {
	h := NewHandler(&HandlerConfig{
		AppToken: "xapp-test-token",
		BotToken: "xoxb-test-token",
	})

	if h.usersClient == nil {
		t.Error("NewHandler() with BotToken should initialize usersClient")
	}
	if h.slackClient == nil {
		t.Error("NewHandler() with BotToken should initialize slackClient")
	}
}

func TestHandler_CheckTaskPermission(t *testing.T) {
	tests := []struct {
		name        string
		resolver    MemberResolver
		checker     TeamChecker
		channelID   string
		senderID    string
		wantErr     bool
		description string
	}{
		{
			name: "admin can execute",
			resolver: &mockMemberResolver{
				mappings: map[string]string{"U_ADMIN": "member-admin"},
			},
			checker: &mockTeamChecker{
				permissions: map[string]map[string]bool{
					"member-admin": {"execute_tasks": true},
				},
			},
			channelID:   "C001",
			senderID:    "U_ADMIN",
			wantErr:     false,
			description: "Admin with execute_tasks permission should be allowed",
		},
		{
			name: "viewer cannot execute",
			resolver: &mockMemberResolver{
				mappings: map[string]string{"U_VIEWER": "member-viewer"},
			},
			checker: &mockTeamChecker{
				permissions: map[string]map[string]bool{
					"member-viewer": {"view_tasks": true},
				},
			},
			channelID:   "C002",
			senderID:    "U_VIEWER",
			wantErr:     true,
			description: "Viewer without execute_tasks permission should be denied",
		},
		{
			name: "unknown user allowed (soft RBAC)",
			resolver: &mockMemberResolver{
				mappings: map[string]string{}, // user not mapped
			},
			checker: &mockTeamChecker{
				permissions: map[string]map[string]bool{},
			},
			channelID:   "C003",
			senderID:    "U_UNMAPPED",
			wantErr:     false,
			description: "Unmapped users should be allowed (soft RBAC)",
		},
		{
			name:        "no resolver configured",
			resolver:    nil,
			checker:     &mockTeamChecker{},
			channelID:   "C004",
			senderID:    "U_ANY",
			wantErr:     false,
			description: "Without resolver, all should be allowed",
		},
		{
			name: "no checker configured",
			resolver: &mockMemberResolver{
				mappings: map[string]string{"U_ANY": "member-any"},
			},
			checker:     nil,
			channelID:   "C005",
			senderID:    "U_ANY",
			wantErr:     false,
			description: "Without checker, all should be allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{
				memberResolver: tt.resolver,
				teamChecker:    tt.checker,
				lastSender:     make(map[string]string),
				log:            slog.Default(),
			}

			if tt.senderID != "" {
				h.TrackSender(tt.channelID, tt.senderID)
			}

			err := h.CheckTaskPermission(context.Background(), tt.channelID, "")
			if tt.wantErr && err == nil {
				t.Errorf("expected error: %s", tt.description)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error (%s): %v", tt.description, err)
			}
		})
	}
}

func TestHandler_ResolveMemberIDWithEmailFallback(t *testing.T) {
	// Create a mock server that returns user info with email
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users.info" {
			userID := r.URL.Query().Get("user")
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
					ID:   userID,
					Name: "testuser",
					Profile: struct {
						Email    string `json:"email"`
						RealName string `json:"real_name_normalized"`
					}{
						Email: "dev@example.com",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	// Resolver that only knows about email, not slack user ID
	resolver := &mockMemberResolver{
		mappings: map[string]string{
			"dev@example.com": "member-via-email",
		},
	}

	h := &Handler{
		memberResolver: resolver,
		usersClient:    NewUsersClientWithBaseURL("test-token", server.URL),
		lastSender:     make(map[string]string),
		log:            slog.Default(),
	}

	h.TrackSender("C_TEST", "U_NEW_USER")

	got := h.ResolveMemberIDForChannel("C_TEST")
	if got != "member-via-email" {
		t.Errorf("expected member-via-email, got %q", got)
	}
}

func TestHandler_ResolveMemberIDForChannel(t *testing.T) {
	resolver := &mockMemberResolver{
		mappings: map[string]string{
			"U12345": "member-123",
		},
	}

	h := &Handler{
		memberResolver: resolver,
		lastSender:     make(map[string]string),
		log:            slog.Default(),
	}

	h.TrackSender("C001", "U12345")

	got := h.ResolveMemberIDForChannel("C001")
	if got != "member-123" {
		t.Errorf("ResolveMemberIDForChannel() = %q, want %q", got, "member-123")
	}

	// Unknown channel should return empty
	got = h.ResolveMemberIDForChannel("C_UNKNOWN")
	if got != "" {
		t.Errorf("ResolveMemberIDForChannel() for unknown channel = %q, want empty", got)
	}
}
