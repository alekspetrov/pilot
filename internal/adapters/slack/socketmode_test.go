package slack

import (
	"errors"
	"testing"
)

func TestParseEnvelope_Message(t *testing.T) {
	raw := []byte(`{
		"type": "events_api",
		"envelope_id": "env-123",
		"payload": {
			"token": "tok",
			"team_id": "T123",
			"api_app_id": "A123",
			"event": {
				"type": "message",
				"user": "U999",
				"text": "hello pilot",
				"channel": "C456",
				"ts": "1234567890.123456",
				"thread_ts": "1234567890.000001"
			},
			"type": "event_callback",
			"event_id": "Ev123"
		}
	}`)

	evt, envID, err := parseEnvelope(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if envID != "env-123" {
		t.Errorf("envelope_id = %q, want %q", envID, "env-123")
	}
	if evt == nil {
		t.Fatal("expected non-nil event")
	}
	if evt.Type != "message" {
		t.Errorf("Type = %q, want %q", evt.Type, "message")
	}
	if evt.UserID != "U999" {
		t.Errorf("UserID = %q, want %q", evt.UserID, "U999")
	}
	if evt.Text != "hello pilot" {
		t.Errorf("Text = %q, want %q", evt.Text, "hello pilot")
	}
	if evt.ChannelID != "C456" {
		t.Errorf("ChannelID = %q, want %q", evt.ChannelID, "C456")
	}
	if evt.TS != "1234567890.123456" {
		t.Errorf("TS = %q, want %q", evt.TS, "1234567890.123456")
	}
	if evt.ThreadTS != "1234567890.000001" {
		t.Errorf("ThreadTS = %q, want %q", evt.ThreadTS, "1234567890.000001")
	}
}

func TestParseEnvelope_AppMention(t *testing.T) {
	raw := []byte(`{
		"type": "events_api",
		"envelope_id": "env-456",
		"payload": {
			"token": "tok",
			"team_id": "T123",
			"api_app_id": "A123",
			"event": {
				"type": "app_mention",
				"user": "U111",
				"text": "<@UBOT> deploy staging",
				"channel": "C789",
				"ts": "1234567890.654321",
				"channel_type": "channel"
			},
			"type": "event_callback",
			"event_id": "Ev456"
		}
	}`)

	evt, envID, err := parseEnvelope(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if envID != "env-456" {
		t.Errorf("envelope_id = %q, want %q", envID, "env-456")
	}
	if evt == nil {
		t.Fatal("expected non-nil event")
	}
	if evt.Type != "app_mention" {
		t.Errorf("Type = %q, want %q", evt.Type, "app_mention")
	}
	if evt.UserID != "U111" {
		t.Errorf("UserID = %q, want %q", evt.UserID, "U111")
	}
	if evt.Text != "<@UBOT> deploy staging" {
		t.Errorf("Text = %q, want %q", evt.Text, "<@UBOT> deploy staging")
	}
}

func TestParseEnvelope_BotSelfMessage(t *testing.T) {
	raw := []byte(`{
		"type": "events_api",
		"envelope_id": "env-bot",
		"payload": {
			"token": "tok",
			"team_id": "T123",
			"api_app_id": "A123",
			"event": {
				"type": "message",
				"bot_id": "B999",
				"text": "I am a bot",
				"channel": "C456",
				"ts": "1234567890.111111"
			},
			"type": "event_callback",
			"event_id": "Ev789"
		}
	}`)

	evt, envID, err := parseEnvelope(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if envID != "env-bot" {
		t.Errorf("envelope_id = %q, want %q", envID, "env-bot")
	}
	if evt != nil {
		t.Errorf("expected nil event for bot message, got %+v", evt)
	}
}

func TestParseEnvelope_Disconnect(t *testing.T) {
	raw := []byte(`{
		"type": "disconnect",
		"envelope_id": "env-dc",
		"reason": "link_disabled",
		"debug_info": {
			"host": "wss-primary-1234.slack.com"
		}
	}`)

	evt, envID, err := parseEnvelope(raw)
	if !errors.Is(err, ErrDisconnect) {
		t.Fatalf("expected ErrDisconnect, got: %v", err)
	}
	if envID != "env-dc" {
		t.Errorf("envelope_id = %q, want %q", envID, "env-dc")
	}
	if evt != nil {
		t.Errorf("expected nil event for disconnect, got %+v", evt)
	}
}

func TestParseEnvelope_HelloIgnored(t *testing.T) {
	raw := []byte(`{
		"type": "hello",
		"envelope_id": "",
		"num_connections": 1
	}`)

	evt, envID, err := parseEnvelope(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if envID != "" {
		t.Errorf("envelope_id = %q, want empty", envID)
	}
	if evt != nil {
		t.Errorf("expected nil event for hello, got %+v", evt)
	}
}

func TestParseEnvelope_UnhandledEventType(t *testing.T) {
	raw := []byte(`{
		"type": "events_api",
		"envelope_id": "env-other",
		"payload": {
			"token": "tok",
			"team_id": "T123",
			"api_app_id": "A123",
			"event": {
				"type": "reaction_added",
				"user": "U111",
				"item": {"type": "message"}
			},
			"type": "event_callback",
			"event_id": "Ev999"
		}
	}`)

	evt, envID, err := parseEnvelope(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if envID != "env-other" {
		t.Errorf("envelope_id = %q, want %q", envID, "env-other")
	}
	if evt != nil {
		t.Errorf("expected nil event for reaction_added, got %+v", evt)
	}
}

func TestParseEnvelope_InvalidJSON(t *testing.T) {
	raw := []byte(`{not json}`)

	_, _, err := parseEnvelope(raw)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseEnvelope_InvalidPayloadJSON(t *testing.T) {
	raw := []byte(`{
		"type": "events_api",
		"envelope_id": "env-bad",
		"payload": "not-an-object"
	}`)

	_, envID, err := parseEnvelope(raw)
	if err == nil {
		t.Fatal("expected error for invalid payload JSON")
	}
	if envID != "env-bad" {
		t.Errorf("envelope_id = %q, want %q", envID, "env-bad")
	}
}

func TestParseEnvelope_WithFiles(t *testing.T) {
	raw := []byte(`{
		"type": "events_api",
		"envelope_id": "env-files",
		"payload": {
			"token": "tok",
			"team_id": "T123",
			"api_app_id": "A123",
			"event": {
				"type": "message",
				"user": "U222",
				"text": "check this screenshot",
				"channel": "C456",
				"ts": "1234567890.222222",
				"files": [
					{
						"id": "F001",
						"name": "screenshot.png",
						"mimetype": "image/png",
						"url_private_download": "https://files.slack.com/files-pri/T123-F001/screenshot.png",
						"size": 102400
					}
				]
			},
			"type": "event_callback",
			"event_id": "Ev222"
		}
	}`)

	evt, envID, err := parseEnvelope(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if envID != "env-files" {
		t.Errorf("envelope_id = %q, want %q", envID, "env-files")
	}
	if evt == nil {
		t.Fatal("expected non-nil event")
	}
	if len(evt.Files) != 1 {
		t.Fatalf("Files count = %d, want 1", len(evt.Files))
	}
	f := evt.Files[0]
	if f.ID != "F001" {
		t.Errorf("File.ID = %q, want %q", f.ID, "F001")
	}
	if f.Name != "screenshot.png" {
		t.Errorf("File.Name = %q, want %q", f.Name, "screenshot.png")
	}
	if f.MimeType != "image/png" {
		t.Errorf("File.MimeType = %q, want %q", f.MimeType, "image/png")
	}
	if f.Size != 102400 {
		t.Errorf("File.Size = %d, want %d", f.Size, 102400)
	}
}

func TestParseEnvelope_NoThreadTS(t *testing.T) {
	raw := []byte(`{
		"type": "events_api",
		"envelope_id": "env-nothrd",
		"payload": {
			"token": "tok",
			"team_id": "T123",
			"api_app_id": "A123",
			"event": {
				"type": "message",
				"user": "U333",
				"text": "top-level message",
				"channel": "C456",
				"ts": "1234567890.333333"
			},
			"type": "event_callback",
			"event_id": "Ev333"
		}
	}`)

	evt, _, err := parseEnvelope(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt == nil {
		t.Fatal("expected non-nil event")
	}
	if evt.ThreadTS != "" {
		t.Errorf("ThreadTS = %q, want empty for top-level message", evt.ThreadTS)
	}
}

func TestStripMention(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		botID string
		want  string
	}{
		{
			name:  "simple mention at start",
			text:  "<@UBOT123> deploy staging",
			botID: "UBOT123",
			want:  "deploy staging",
		},
		{
			name:  "mention with display name",
			text:  "<@UBOT123|pilot> run tests",
			botID: "UBOT123",
			want:  "run tests",
		},
		{
			name:  "mention in middle",
			text:  "hey <@UBOT123> can you deploy?",
			botID: "UBOT123",
			want:  "hey  can you deploy?",
		},
		{
			name:  "different bot mention preserved",
			text:  "<@UOTHER> deploy staging",
			botID: "UBOT123",
			want:  "<@UOTHER> deploy staging",
		},
		{
			name:  "multiple mentions mixed",
			text:  "<@UBOT123> <@UOTHER> deploy",
			botID: "UBOT123",
			want:  "<@UOTHER> deploy",
		},
		{
			name:  "no mention",
			text:  "deploy staging",
			botID: "UBOT123",
			want:  "deploy staging",
		},
		{
			name:  "empty botID strips all",
			text:  "<@UBOT123> <@UOTHER> deploy",
			botID: "",
			want:  "deploy",
		},
		{
			name:  "empty text",
			text:  "",
			botID: "UBOT123",
			want:  "",
		},
		{
			name:  "only mention",
			text:  "<@UBOT123>",
			botID: "UBOT123",
			want:  "",
		},
		{
			name:  "mention with extra whitespace",
			text:  "  <@UBOT123>   hello  ",
			botID: "UBOT123",
			want:  "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripMention(tt.text, tt.botID)
			if got != tt.want {
				t.Errorf("stripMention(%q, %q) = %q, want %q", tt.text, tt.botID, got, tt.want)
			}
		})
	}
}
