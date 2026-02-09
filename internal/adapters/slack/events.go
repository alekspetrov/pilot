package slack

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// SocketEvent represents a parsed Slack event received via Socket Mode.
type SocketEvent struct {
	Type      string     // "message", "app_mention"
	ChannelID string     // Channel where the event occurred
	UserID    string     // User who triggered the event
	Text      string     // Message text (bot mention prefix stripped for app_mention)
	ThreadTS  string     // Parent thread timestamp (for threaded replies)
	Timestamp string     // Message timestamp
	Files     []SlackFile // Optional file attachments
}

// SlackFile represents a file attachment in a Slack message.
type SlackFile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MimeType string `json:"mimetype"`
	URL      string `json:"url_private"`
}

// envelope is the outer wrapper for all Socket Mode messages.
type envelope struct {
	EnvelopeID string          `json:"envelope_id"`
	Type       string          `json:"type"`       // "events_api", "disconnect", "hello"
	Payload    json.RawMessage `json:"payload"`
	Reason     string          `json:"reason,omitempty"` // For disconnect envelopes
}

// eventsAPIPayload is the payload for type="events_api" envelopes.
type eventsAPIPayload struct {
	Event innerEvent `json:"event"`
}

// innerEvent is the actual Slack event inside an events_api payload.
type innerEvent struct {
	Type      string     `json:"type"`       // "message", "app_mention"
	Channel   string     `json:"channel"`
	User      string     `json:"user"`
	Text      string     `json:"text"`
	ThreadTS  string     `json:"thread_ts"`
	TS        string     `json:"ts"`
	BotID     string     `json:"bot_id,omitempty"`
	Subtype   string     `json:"subtype,omitempty"` // "bot_message", etc.
	Files     []SlackFile `json:"files,omitempty"`
}

// botMentionRegex matches <@UBOTID> prefix (with optional trailing whitespace).
var botMentionRegex = regexp.MustCompile(`^<@[A-Z0-9]+>\s*`)

// parseEnvelope parses a raw WebSocket message into an envelope.
func parseEnvelope(data []byte) (*envelope, error) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("unmarshal envelope: %w", err)
	}
	return &env, nil
}

// parseSocketEvent extracts a SocketEvent from an events_api envelope payload.
// Returns nil if the event should be ignored (bot's own message, unsupported type).
func parseSocketEvent(payload json.RawMessage) (*SocketEvent, error) {
	var p eventsAPIPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("unmarshal events_api payload: %w", err)
	}

	ev := p.Event

	// Ignore bot messages
	if ev.BotID != "" || ev.Subtype == "bot_message" {
		return nil, nil
	}

	// Only handle message and app_mention events
	switch ev.Type {
	case "message", "app_mention":
		// ok
	default:
		return nil, nil
	}

	text := ev.Text
	// Strip <@BOTID> mention prefix from app_mention text
	if ev.Type == "app_mention" {
		text = botMentionRegex.ReplaceAllString(text, "")
	}

	return &SocketEvent{
		Type:      ev.Type,
		ChannelID: ev.Channel,
		UserID:    ev.User,
		Text:      text,
		ThreadTS:  ev.ThreadTS,
		Timestamp: ev.TS,
		Files:     ev.Files,
	}, nil
}
