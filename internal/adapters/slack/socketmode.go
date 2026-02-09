package slack

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Socket Mode envelope types
const (
	EnvelopeTypeEventCallback  = "events_api"
	EnvelopeTypeInteractive    = "interactive"
	EnvelopeTypeSlashCommands  = "slash_commands"
	EnvelopeTypeDisconnect     = "disconnect"
	EnvelopeTypeHello          = "hello"
)

// Inner event types
const (
	EventTypeMessage    = "message"
	EventTypeAppMention = "app_mention"
)

// mentionRegex matches a Slack bot mention prefix like "<@U12345678> "
var mentionRegex = regexp.MustCompile(`^<@[A-Z0-9]+>\s*`)

// Envelope represents a Socket Mode envelope received over WebSocket.
// All Socket Mode messages share this top-level structure.
type Envelope struct {
	EnvelopeID string          `json:"envelope_id"`
	Type       string          `json:"type"`
	Payload    json.RawMessage `json:"payload"`

	// Disconnect-specific fields (present when Type == "disconnect")
	Reason string `json:"reason,omitempty"`
}

// EventCallback represents the payload inside an events_api envelope.
type EventCallback struct {
	Token    string          `json:"token"`
	TeamID   string          `json:"team_id"`
	APIAppID string          `json:"api_app_id"`
	Event    json.RawMessage `json:"event"`
	Type     string          `json:"type"`
	EventID  string          `json:"event_id"`
	EventTS  string          `json:"event_time"`
}

// InnerEvent represents a message or app_mention event extracted from EventCallback.
type InnerEvent struct {
	Type      string `json:"type"`
	Channel   string `json:"channel"`
	User      string `json:"user"`
	Text      string `json:"text"`
	TS        string `json:"ts"`
	ThreadTS  string `json:"thread_ts,omitempty"`
	BotID     string `json:"bot_id,omitempty"`
	SubType   string `json:"subtype,omitempty"`
	ChannelID string `json:"channel_id,omitempty"` // Some events use channel_id instead of channel
}

// EffectiveChannel returns the channel identifier, checking both Channel and ChannelID fields.
func (e *InnerEvent) EffectiveChannel() string {
	if e.Channel != "" {
		return e.Channel
	}
	return e.ChannelID
}

// IsBot returns true if the event was sent by a bot.
func (e *InnerEvent) IsBot() bool {
	return e.BotID != "" || e.SubType == "bot_message"
}

// ParseEnvelope parses a raw WebSocket message into an Envelope.
func ParseEnvelope(data []byte) (*Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("failed to parse envelope: %w", err)
	}
	if env.Type == "" {
		return nil, fmt.Errorf("envelope missing type field")
	}
	return &env, nil
}

// ParseEventCallback extracts an EventCallback from an Envelope payload.
// Returns an error if the envelope type is not events_api.
func ParseEventCallback(env *Envelope) (*EventCallback, error) {
	if env.Type != EnvelopeTypeEventCallback {
		return nil, fmt.Errorf("expected envelope type %q, got %q", EnvelopeTypeEventCallback, env.Type)
	}
	var cb EventCallback
	if err := json.Unmarshal(env.Payload, &cb); err != nil {
		return nil, fmt.Errorf("failed to parse event callback: %w", err)
	}
	return &cb, nil
}

// ParseInnerEvent extracts an InnerEvent from an EventCallback's raw event JSON.
// Supports "message" and "app_mention" event types.
func ParseInnerEvent(cb *EventCallback) (*InnerEvent, error) {
	var inner InnerEvent
	if err := json.Unmarshal(cb.Event, &inner); err != nil {
		return nil, fmt.Errorf("failed to parse inner event: %w", err)
	}
	if inner.Type != EventTypeMessage && inner.Type != EventTypeAppMention {
		return nil, fmt.Errorf("unsupported inner event type: %q", inner.Type)
	}
	return &inner, nil
}

// StripMention removes the leading <@BOTID> mention prefix from text.
// If no mention prefix is present, the original text is returned unchanged.
func StripMention(text string) string {
	return strings.TrimSpace(mentionRegex.ReplaceAllString(text, ""))
}

// IsSelfMessage checks whether an event was produced by the given bot ID.
// Use this to filter out messages the bot itself sent to avoid infinite loops.
func IsSelfMessage(event *InnerEvent, selfBotID string) bool {
	if event.BotID != "" && event.BotID == selfBotID {
		return true
	}
	return false
}

// IsDisconnect returns true if the envelope is a disconnect signal.
func IsDisconnect(env *Envelope) bool {
	return env.Type == EnvelopeTypeDisconnect
}

// EnvelopeAck is the acknowledgement payload sent back over the WebSocket
// to confirm receipt of an envelope.
type EnvelopeAck struct {
	EnvelopeID string          `json:"envelope_id"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// NewEnvelopeAck creates an acknowledgement for the given envelope.
func NewEnvelopeAck(envelopeID string) *EnvelopeAck {
	return &EnvelopeAck{
		EnvelopeID: envelopeID,
	}
}

// MarshalAck serializes an EnvelopeAck to JSON bytes.
func MarshalAck(ack *EnvelopeAck) ([]byte, error) {
	return json.Marshal(ack)
}
