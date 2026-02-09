package slack

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// SocketEvent represents a parsed Slack event from Socket Mode.
type SocketEvent struct {
	Type      string     // "message", "app_mention"
	ChannelID string     // Channel where event occurred
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
	Size     int    `json:"size"`
}

// envelope is the raw Socket Mode wrapper sent by Slack over WebSocket.
type envelope struct {
	EnvelopeID string          `json:"envelope_id"`
	Type       string          `json:"type"`       // "events_api", "disconnect", "hello"
	Payload    json.RawMessage `json:"payload"`
	Reason     string          `json:"reason,omitempty"` // present on "disconnect"
}

// eventsAPIPayload is the inner payload for type=="events_api".
type eventsAPIPayload struct {
	Event innerEvent `json:"event"`
}

// innerEvent is the actual Slack event inside the events_api payload.
type innerEvent struct {
	Type      string     `json:"type"`                // "message", "app_mention"
	Channel   string     `json:"channel"`
	User      string     `json:"user"`
	Text      string     `json:"text"`
	ThreadTS  string     `json:"thread_ts,omitempty"`
	TS        string     `json:"ts"`
	BotID     string     `json:"bot_id,omitempty"`
	Files     []SlackFile `json:"files,omitempty"`
}

// botMentionRegex matches <@UBOTID> prefix in app_mention text.
var botMentionRegex = regexp.MustCompile(`^<@[A-Z0-9]+>\s*`)

// parseEnvelope decodes a raw WebSocket message into an envelope.
func parseEnvelope(data []byte) (*envelope, error) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse envelope: %w", err)
	}
	return &env, nil
}

// parseEventsAPI extracts a SocketEvent from an events_api envelope payload.
// Returns nil if the event should be ignored (bot's own message, unsupported type).
func parseEventsAPI(raw json.RawMessage) (*SocketEvent, error) {
	var p eventsAPIPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("parse events_api payload: %w", err)
	}

	ev := p.Event

	// Ignore bot's own messages.
	if ev.BotID != "" {
		return nil, nil
	}

	// Only handle message and app_mention events.
	switch ev.Type {
	case "message", "app_mention":
	default:
		return nil, nil
	}

	text := ev.Text
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
