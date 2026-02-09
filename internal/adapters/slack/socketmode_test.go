package slack

import (
	"encoding/json"
	"testing"
)

func TestParseEnvelope(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *Envelope
		wantErr bool
	}{
		{
			name: "valid events_api envelope",
			input: `{
				"envelope_id": "env-123",
				"type": "events_api",
				"payload": {"event": {"type": "message"}}
			}`,
			want: &Envelope{
				EnvelopeID: "env-123",
				Type:       EnvelopeTypeEventCallback,
			},
		},
		{
			name: "valid disconnect envelope",
			input: `{
				"envelope_id": "env-456",
				"type": "disconnect",
				"reason": "link_disabled"
			}`,
			want: &Envelope{
				EnvelopeID: "env-456",
				Type:       EnvelopeTypeDisconnect,
				Reason:     "link_disabled",
			},
		},
		{
			name: "valid hello envelope",
			input: `{
				"envelope_id": "env-789",
				"type": "hello",
				"payload": {}
			}`,
			want: &Envelope{
				EnvelopeID: "env-789",
				Type:       EnvelopeTypeHello,
			},
		},
		{
			name:    "missing type field",
			input:   `{"envelope_id": "env-000"}`,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   `not json`,
			wantErr: true,
		},
		{
			name:    "empty object",
			input:   `{}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEnvelope([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.EnvelopeID != tt.want.EnvelopeID {
				t.Errorf("EnvelopeID = %q, want %q", got.EnvelopeID, tt.want.EnvelopeID)
			}
			if got.Type != tt.want.Type {
				t.Errorf("Type = %q, want %q", got.Type, tt.want.Type)
			}
			if tt.want.Reason != "" && got.Reason != tt.want.Reason {
				t.Errorf("Reason = %q, want %q", got.Reason, tt.want.Reason)
			}
		})
	}
}

func TestParseEventCallback(t *testing.T) {
	tests := []struct {
		name    string
		env     *Envelope
		want    *EventCallback
		wantErr bool
	}{
		{
			name: "valid event callback",
			env: &Envelope{
				EnvelopeID: "env-123",
				Type:       EnvelopeTypeEventCallback,
				Payload: json.RawMessage(`{
					"token": "tok-abc",
					"team_id": "T12345",
					"api_app_id": "A12345",
					"event": {"type": "message", "text": "hello"},
					"type": "event_callback",
					"event_id": "Ev12345",
					"event_time": "1234567890"
				}`),
			},
			want: &EventCallback{
				Token:    "tok-abc",
				TeamID:   "T12345",
				APIAppID: "A12345",
				Type:     "event_callback",
				EventID:  "Ev12345",
				EventTS:  "1234567890",
			},
		},
		{
			name: "wrong envelope type",
			env: &Envelope{
				Type:    EnvelopeTypeDisconnect,
				Payload: json.RawMessage(`{}`),
			},
			wantErr: true,
		},
		{
			name: "malformed payload",
			env: &Envelope{
				Type:    EnvelopeTypeEventCallback,
				Payload: json.RawMessage(`not json`),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEventCallback(tt.env)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Token != tt.want.Token {
				t.Errorf("Token = %q, want %q", got.Token, tt.want.Token)
			}
			if got.TeamID != tt.want.TeamID {
				t.Errorf("TeamID = %q, want %q", got.TeamID, tt.want.TeamID)
			}
			if got.APIAppID != tt.want.APIAppID {
				t.Errorf("APIAppID = %q, want %q", got.APIAppID, tt.want.APIAppID)
			}
			if got.EventID != tt.want.EventID {
				t.Errorf("EventID = %q, want %q", got.EventID, tt.want.EventID)
			}
		})
	}
}

func TestParseInnerEvent(t *testing.T) {
	tests := []struct {
		name    string
		event   json.RawMessage
		want    *InnerEvent
		wantErr bool
	}{
		{
			name: "message event",
			event: json.RawMessage(`{
				"type": "message",
				"channel": "C12345",
				"user": "U12345",
				"text": "hello world",
				"ts": "1234567890.123456"
			}`),
			want: &InnerEvent{
				Type:    EventTypeMessage,
				Channel: "C12345",
				User:    "U12345",
				Text:    "hello world",
				TS:      "1234567890.123456",
			},
		},
		{
			name: "app_mention event",
			event: json.RawMessage(`{
				"type": "app_mention",
				"channel": "C67890",
				"user": "U67890",
				"text": "<@U00BOT> deploy staging",
				"ts": "1234567890.654321"
			}`),
			want: &InnerEvent{
				Type:    EventTypeAppMention,
				Channel: "C67890",
				User:    "U67890",
				Text:    "<@U00BOT> deploy staging",
				TS:      "1234567890.654321",
			},
		},
		{
			name: "message event with thread",
			event: json.RawMessage(`{
				"type": "message",
				"channel": "C12345",
				"user": "U12345",
				"text": "reply in thread",
				"ts": "1234567890.123457",
				"thread_ts": "1234567890.123456"
			}`),
			want: &InnerEvent{
				Type:     EventTypeMessage,
				Channel:  "C12345",
				User:     "U12345",
				Text:     "reply in thread",
				TS:       "1234567890.123457",
				ThreadTS: "1234567890.123456",
			},
		},
		{
			name: "bot message",
			event: json.RawMessage(`{
				"type": "message",
				"channel": "C12345",
				"text": "I am a bot",
				"ts": "1234567890.999999",
				"bot_id": "B12345",
				"subtype": "bot_message"
			}`),
			want: &InnerEvent{
				Type:    EventTypeMessage,
				Channel: "C12345",
				Text:    "I am a bot",
				TS:      "1234567890.999999",
				BotID:   "B12345",
				SubType: "bot_message",
			},
		},
		{
			name: "event with channel_id instead of channel",
			event: json.RawMessage(`{
				"type": "app_mention",
				"channel_id": "C99999",
				"user": "U12345",
				"text": "<@UBOT> help",
				"ts": "1234567890.111111"
			}`),
			want: &InnerEvent{
				Type:      EventTypeAppMention,
				ChannelID: "C99999",
				User:      "U12345",
				Text:      "<@UBOT> help",
				TS:        "1234567890.111111",
			},
		},
		{
			name: "unsupported event type",
			event: json.RawMessage(`{
				"type": "channel_created",
				"channel": {"id": "C12345"}
			}`),
			wantErr: true,
		},
		{
			name:    "malformed JSON",
			event:   json.RawMessage(`{broken`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := &EventCallback{Event: tt.event}
			got, err := ParseInnerEvent(cb)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Type != tt.want.Type {
				t.Errorf("Type = %q, want %q", got.Type, tt.want.Type)
			}
			if got.Channel != tt.want.Channel {
				t.Errorf("Channel = %q, want %q", got.Channel, tt.want.Channel)
			}
			if got.User != tt.want.User {
				t.Errorf("User = %q, want %q", got.User, tt.want.User)
			}
			if got.Text != tt.want.Text {
				t.Errorf("Text = %q, want %q", got.Text, tt.want.Text)
			}
			if got.TS != tt.want.TS {
				t.Errorf("TS = %q, want %q", got.TS, tt.want.TS)
			}
			if got.ThreadTS != tt.want.ThreadTS {
				t.Errorf("ThreadTS = %q, want %q", got.ThreadTS, tt.want.ThreadTS)
			}
			if got.BotID != tt.want.BotID {
				t.Errorf("BotID = %q, want %q", got.BotID, tt.want.BotID)
			}
			if got.SubType != tt.want.SubType {
				t.Errorf("SubType = %q, want %q", got.SubType, tt.want.SubType)
			}
			if got.ChannelID != tt.want.ChannelID {
				t.Errorf("ChannelID = %q, want %q", got.ChannelID, tt.want.ChannelID)
			}
		})
	}
}

func TestStripMention(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "mention with space",
			input: "<@U12345678> deploy staging",
			want:  "deploy staging",
		},
		{
			name:  "mention without space",
			input: "<@U12345678>deploy staging",
			want:  "deploy staging",
		},
		{
			name:  "mention with extra spaces",
			input: "<@U12345678>   deploy staging",
			want:  "deploy staging",
		},
		{
			name:  "no mention",
			input: "just a regular message",
			want:  "just a regular message",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "mention only",
			input: "<@UBOT123>",
			want:  "",
		},
		{
			name:  "mention in middle (not stripped)",
			input: "hey <@U12345678> what's up",
			want:  "hey <@U12345678> what's up",
		},
		{
			name:  "alphanumeric bot ID",
			input: "<@U0ABC9XYZ> run tests",
			want:  "run tests",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripMention(tt.input)
			if got != tt.want {
				t.Errorf("StripMention(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsSelfMessage(t *testing.T) {
	tests := []struct {
		name      string
		event     *InnerEvent
		selfBotID string
		want      bool
	}{
		{
			name:      "matching bot_id",
			event:     &InnerEvent{BotID: "B12345"},
			selfBotID: "B12345",
			want:      true,
		},
		{
			name:      "different bot_id",
			event:     &InnerEvent{BotID: "B99999"},
			selfBotID: "B12345",
			want:      false,
		},
		{
			name:      "no bot_id on event",
			event:     &InnerEvent{User: "U12345"},
			selfBotID: "B12345",
			want:      false,
		},
		{
			name:      "empty self bot ID",
			event:     &InnerEvent{BotID: "B12345"},
			selfBotID: "",
			want:      false,
		},
		{
			name:      "both empty",
			event:     &InnerEvent{},
			selfBotID: "",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSelfMessage(tt.event, tt.selfBotID)
			if got != tt.want {
				t.Errorf("IsSelfMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsDisconnect(t *testing.T) {
	tests := []struct {
		name string
		env  *Envelope
		want bool
	}{
		{
			name: "disconnect envelope",
			env:  &Envelope{Type: EnvelopeTypeDisconnect, Reason: "link_disabled"},
			want: true,
		},
		{
			name: "events_api envelope",
			env:  &Envelope{Type: EnvelopeTypeEventCallback},
			want: false,
		},
		{
			name: "hello envelope",
			env:  &Envelope{Type: EnvelopeTypeHello},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsDisconnect(tt.env)
			if got != tt.want {
				t.Errorf("IsDisconnect() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInnerEvent_IsBot(t *testing.T) {
	tests := []struct {
		name  string
		event *InnerEvent
		want  bool
	}{
		{
			name:  "has bot_id",
			event: &InnerEvent{BotID: "B12345"},
			want:  true,
		},
		{
			name:  "has bot_message subtype",
			event: &InnerEvent{SubType: "bot_message"},
			want:  true,
		},
		{
			name:  "both bot_id and subtype",
			event: &InnerEvent{BotID: "B12345", SubType: "bot_message"},
			want:  true,
		},
		{
			name:  "regular user message",
			event: &InnerEvent{User: "U12345"},
			want:  false,
		},
		{
			name:  "empty event",
			event: &InnerEvent{},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.event.IsBot()
			if got != tt.want {
				t.Errorf("IsBot() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInnerEvent_EffectiveChannel(t *testing.T) {
	tests := []struct {
		name  string
		event *InnerEvent
		want  string
	}{
		{
			name:  "channel field set",
			event: &InnerEvent{Channel: "C12345"},
			want:  "C12345",
		},
		{
			name:  "channel_id field set",
			event: &InnerEvent{ChannelID: "C67890"},
			want:  "C67890",
		},
		{
			name:  "both set prefers channel",
			event: &InnerEvent{Channel: "C12345", ChannelID: "C67890"},
			want:  "C12345",
		},
		{
			name:  "neither set",
			event: &InnerEvent{},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.event.EffectiveChannel()
			if got != tt.want {
				t.Errorf("EffectiveChannel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewEnvelopeAck(t *testing.T) {
	ack := NewEnvelopeAck("env-abc-123")
	if ack.EnvelopeID != "env-abc-123" {
		t.Errorf("EnvelopeID = %q, want %q", ack.EnvelopeID, "env-abc-123")
	}
	if ack.Payload != nil {
		t.Errorf("Payload = %v, want nil", ack.Payload)
	}
}

func TestMarshalAck(t *testing.T) {
	ack := NewEnvelopeAck("env-123")
	data, err := MarshalAck(ack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed EnvelopeAck
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal ack: %v", err)
	}
	if parsed.EnvelopeID != "env-123" {
		t.Errorf("EnvelopeID = %q, want %q", parsed.EnvelopeID, "env-123")
	}
}

func TestEndToEnd_EventCallbackPipeline(t *testing.T) {
	// Simulate a full Socket Mode message event pipeline:
	// raw WebSocket data → envelope → event callback → inner event → strip mention
	raw := `{
		"envelope_id": "e2e-001",
		"type": "events_api",
		"payload": {
			"token": "test-token",
			"team_id": "T001",
			"api_app_id": "A001",
			"event": {
				"type": "app_mention",
				"channel": "C001",
				"user": "U001",
				"text": "<@UBOTID> deploy production",
				"ts": "1700000000.000001"
			},
			"type": "event_callback",
			"event_id": "Ev001",
			"event_time": "1700000000"
		}
	}`

	env, err := ParseEnvelope([]byte(raw))
	if err != nil {
		t.Fatalf("ParseEnvelope: %v", err)
	}

	if IsDisconnect(env) {
		t.Fatal("should not be a disconnect envelope")
	}

	cb, err := ParseEventCallback(env)
	if err != nil {
		t.Fatalf("ParseEventCallback: %v", err)
	}

	inner, err := ParseInnerEvent(cb)
	if err != nil {
		t.Fatalf("ParseInnerEvent: %v", err)
	}

	if inner.Type != EventTypeAppMention {
		t.Errorf("inner event type = %q, want %q", inner.Type, EventTypeAppMention)
	}

	if inner.EffectiveChannel() != "C001" {
		t.Errorf("channel = %q, want %q", inner.EffectiveChannel(), "C001")
	}

	stripped := StripMention(inner.Text)
	if stripped != "deploy production" {
		t.Errorf("stripped text = %q, want %q", stripped, "deploy production")
	}

	if IsSelfMessage(inner, "BOTHER") {
		t.Error("should not be a self message")
	}

	// Verify ack
	ack := NewEnvelopeAck(env.EnvelopeID)
	ackData, err := MarshalAck(ack)
	if err != nil {
		t.Fatalf("MarshalAck: %v", err)
	}
	if len(ackData) == 0 {
		t.Fatal("ack data should not be empty")
	}
}

func TestEndToEnd_DisconnectPipeline(t *testing.T) {
	raw := `{
		"envelope_id": "disc-001",
		"type": "disconnect",
		"reason": "warning",
		"payload": {}
	}`

	env, err := ParseEnvelope([]byte(raw))
	if err != nil {
		t.Fatalf("ParseEnvelope: %v", err)
	}

	if !IsDisconnect(env) {
		t.Fatal("should be a disconnect envelope")
	}

	if env.Reason != "warning" {
		t.Errorf("Reason = %q, want %q", env.Reason, "warning")
	}

	// Attempting to parse as event callback should fail
	_, err = ParseEventCallback(env)
	if err == nil {
		t.Fatal("expected error when parsing disconnect as event callback")
	}
}

func TestEndToEnd_BotSelfFilter(t *testing.T) {
	raw := `{
		"envelope_id": "bot-001",
		"type": "events_api",
		"payload": {
			"token": "test-token",
			"team_id": "T001",
			"api_app_id": "A001",
			"event": {
				"type": "message",
				"channel": "C001",
				"text": "I posted this",
				"ts": "1700000000.000002",
				"bot_id": "BMYBOT"
			},
			"type": "event_callback",
			"event_id": "Ev002",
			"event_time": "1700000000"
		}
	}`

	env, err := ParseEnvelope([]byte(raw))
	if err != nil {
		t.Fatalf("ParseEnvelope: %v", err)
	}

	cb, err := ParseEventCallback(env)
	if err != nil {
		t.Fatalf("ParseEventCallback: %v", err)
	}

	inner, err := ParseInnerEvent(cb)
	if err != nil {
		t.Fatalf("ParseInnerEvent: %v", err)
	}

	if !inner.IsBot() {
		t.Error("should be detected as bot message")
	}

	if !IsSelfMessage(inner, "BMYBOT") {
		t.Error("should be detected as self message")
	}

	if IsSelfMessage(inner, "BOTHER") {
		t.Error("should not match different bot ID")
	}
}
