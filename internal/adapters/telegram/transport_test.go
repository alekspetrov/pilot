package telegram

import (
	"testing"
)

func TestTransportConfig(t *testing.T) {
	cfg := &TransportConfig{
		AllowedIDs: []int64{111, 222, 333},
	}

	transport := &Transport{
		allowedIDs: make(map[int64]bool),
	}
	for _, id := range cfg.AllowedIDs {
		transport.allowedIDs[id] = true
	}

	if !transport.allowedIDs[111] {
		t.Error("expected 111 to be allowed")
	}
	if !transport.allowedIDs[222] {
		t.Error("expected 222 to be allowed")
	}
	if transport.allowedIDs[999] {
		t.Error("expected 999 to NOT be allowed")
	}
}

func TestTransport_isAllowed(t *testing.T) {
	tests := []struct {
		name       string
		allowedIDs map[int64]bool
		msg        *Message
		want       bool
	}{
		{
			name:       "no restrictions",
			allowedIDs: map[int64]bool{},
			msg:        &Message{Chat: &Chat{ID: 123}},
			want:       true,
		},
		{
			name:       "chat allowed",
			allowedIDs: map[int64]bool{123: true},
			msg:        &Message{Chat: &Chat{ID: 123}},
			want:       true,
		},
		{
			name:       "sender allowed",
			allowedIDs: map[int64]bool{456: true},
			msg:        &Message{Chat: &Chat{ID: 999}, From: &User{ID: 456}},
			want:       true,
		},
		{
			name:       "neither allowed",
			allowedIDs: map[int64]bool{789: true},
			msg:        &Message{Chat: &Chat{ID: 111}, From: &User{ID: 222}},
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &Transport{allowedIDs: tt.allowedIDs}
			if got := transport.isAllowed(tt.msg); got != tt.want {
				t.Errorf("isAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetFileExtension(t *testing.T) {
	tests := []struct {
		path     string
		fallback string
		want     string
	}{
		{"file.oga", ".mp3", ".oga"},
		{"photos/image.jpg", ".png", ".jpg"},
		{"noext", ".default", ".default"},
		{"", ".fallback", ".fallback"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := getFileExtension(tt.path, tt.fallback)
			if got != tt.want {
				t.Errorf("getFileExtension(%q, %q) = %q, want %q", tt.path, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestTelegramMemberResolverAdapter(t *testing.T) {
	t.Run("nil resolver", func(t *testing.T) {
		adapter := NewTelegramMemberResolverAdapter(nil)
		id, err := adapter.ResolveIdentity("123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "" {
			t.Errorf("expected empty ID, got %q", id)
		}
	})

	t.Run("empty sender", func(t *testing.T) {
		adapter := NewTelegramMemberResolverAdapter(&mockTelegramResolver{})
		id, err := adapter.ResolveIdentity("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "" {
			t.Errorf("expected empty ID, got %q", id)
		}
	})

	t.Run("invalid sender ID", func(t *testing.T) {
		adapter := NewTelegramMemberResolverAdapter(&mockTelegramResolver{})
		id, err := adapter.ResolveIdentity("not-a-number")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "" {
			t.Errorf("expected empty ID, got %q", id)
		}
	})

	t.Run("valid resolution", func(t *testing.T) {
		resolver := &mockTelegramResolver{
			resolveFunc: func(telegramID int64, email string) (string, error) {
				if telegramID == 42 {
					return "member-42", nil
				}
				return "", nil
			},
		}
		adapter := NewTelegramMemberResolverAdapter(resolver)
		id, err := adapter.ResolveIdentity("42")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "member-42" {
			t.Errorf("expected 'member-42', got %q", id)
		}
	})
}

type mockTelegramResolver struct {
	resolveFunc func(telegramID int64, email string) (string, error)
}

func (m *mockTelegramResolver) ResolveTelegramIdentity(telegramID int64, email string) (string, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(telegramID, email)
	}
	return "", nil
}
