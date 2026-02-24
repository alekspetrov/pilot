package slack

import (
	"testing"
)

func TestTransport_isAllowed(t *testing.T) {
	tests := []struct {
		name            string
		allowedChannels map[string]bool
		allowedUsers    map[string]bool
		channelID       string
		userID          string
		want            bool
	}{
		{
			name:            "no restrictions",
			allowedChannels: map[string]bool{},
			allowedUsers:    map[string]bool{},
			channelID:       "C123",
			userID:          "U456",
			want:            true,
		},
		{
			name:            "channel allowed",
			allowedChannels: map[string]bool{"C123": true},
			allowedUsers:    map[string]bool{},
			channelID:       "C123",
			userID:          "U999",
			want:            true,
		},
		{
			name:            "user allowed",
			allowedChannels: map[string]bool{},
			allowedUsers:    map[string]bool{"U456": true},
			channelID:       "C999",
			userID:          "U456",
			want:            true,
		},
		{
			name:            "neither allowed",
			allowedChannels: map[string]bool{"C789": true},
			allowedUsers:    map[string]bool{"U789": true},
			channelID:       "C111",
			userID:          "U222",
			want:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &Transport{
				allowedChannels: tt.allowedChannels,
				allowedUsers:    tt.allowedUsers,
			}
			if got := transport.isAllowed(tt.channelID, tt.userID); got != tt.want {
				t.Errorf("isAllowed(%s, %s) = %v, want %v", tt.channelID, tt.userID, got, tt.want)
			}
		})
	}
}

func TestSlackMemberResolverAdapter(t *testing.T) {
	t.Run("nil resolver", func(t *testing.T) {
		adapter := NewSlackMemberResolverAdapter(nil)
		id, err := adapter.ResolveIdentity("U123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "" {
			t.Errorf("expected empty ID, got %q", id)
		}
	})

	t.Run("empty sender", func(t *testing.T) {
		adapter := NewSlackMemberResolverAdapter(&mockSlackResolver{})
		id, err := adapter.ResolveIdentity("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "" {
			t.Errorf("expected empty ID, got %q", id)
		}
	})

	t.Run("valid resolution", func(t *testing.T) {
		resolver := &mockSlackResolver{
			resolveFunc: func(slackUserID, email string) (string, error) {
				if slackUserID == "U42" {
					return "member-42", nil
				}
				return "", nil
			},
		}
		adapter := NewSlackMemberResolverAdapter(resolver)
		id, err := adapter.ResolveIdentity("U42")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "member-42" {
			t.Errorf("expected 'member-42', got %q", id)
		}
	})
}

type mockSlackResolver struct {
	resolveFunc func(slackUserID, email string) (string, error)
}

func (m *mockSlackResolver) ResolveSlackIdentity(slackUserID, email string) (string, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(slackUserID, email)
	}
	return "", nil
}
