package slack

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// SocketEvent represents a parsed event from Slack Socket Mode.
type SocketEvent struct {
	Type      string     // "message", "app_mention"
	ChannelID string     // channel where the event occurred
	UserID    string     // user who sent the message
	Text      string     // message text (mentions stripped for app_mention)
	ThreadTS  string     // parent thread timestamp
	Timestamp string     // message timestamp
	BotID     string     // bot_id if message was sent by a bot
	Files     []SlackFile // optional file attachments
}

// SlackFile represents a file attachment in a Slack message.
type SlackFile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MimeType string `json:"mimetype"`
	URL      string `json:"url_private"`
	Size     int    `json:"size"`
}

// socketEnvelope is the outer Socket Mode envelope.
type socketEnvelope struct {
	EnvelopeID string          `json:"envelope_id"`
	Type       string          `json:"type"`       // "events_api", "disconnect", "hello"
	Payload    json.RawMessage `json:"payload"`
	Reason     string          `json:"reason,omitempty"` // for disconnect
}

// eventsAPIPayload is the events_api wrapper inside the envelope.
type eventsAPIPayload struct {
	Type  string          `json:"type"` // "event_callback"
	Event json.RawMessage `json:"event"`
}

// innerEvent is the actual event inside the events_api payload.
type innerEvent struct {
	Type      string     `json:"type"` // "message", "app_mention"
	Channel   string     `json:"channel"`
	User      string     `json:"user"`
	Text      string     `json:"text"`
	TS        string     `json:"ts"`
	ThreadTS  string     `json:"thread_ts,omitempty"`
	BotID     string     `json:"bot_id,omitempty"`
	Subtype   string     `json:"subtype,omitempty"`
	Files     []SlackFile `json:"files,omitempty"`
}

// mentionRegex matches Slack user mentions like <@U12345678>.
var mentionRegex = regexp.MustCompile(`<@[A-Z0-9]+>`)

// parseEnvelope parses a raw Socket Mode envelope into a SocketEvent.
// Returns nil if the envelope should be ignored (e.g., hello, bot messages).
// Returns the envelope ID for acknowledgement regardless.
func parseEnvelope(data []byte) (envelopeID string, envelopeType string, event *SocketEvent, err error) {
	var env socketEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return "", "", nil, fmt.Errorf("unmarshal envelope: %w", err)
	}

	envelopeID = env.EnvelopeID
	envelopeType = env.Type

	switch env.Type {
	case "hello":
		return envelopeID, envelopeType, nil, nil
	case "disconnect":
		return envelopeID, envelopeType, nil, nil
	case "events_api":
		evt, err := parseEventsAPI(env.Payload)
		if err != nil {
			return envelopeID, envelopeType, nil, fmt.Errorf("parse events_api: %w", err)
		}
		return envelopeID, envelopeType, evt, nil
	default:
		return envelopeID, envelopeType, nil, nil
	}
}

// parseEventsAPI extracts a SocketEvent from an events_api payload.
func parseEventsAPI(data json.RawMessage) (*SocketEvent, error) {
	var payload eventsAPIPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal events_api payload: %w", err)
	}

	if payload.Type != "event_callback" {
		return nil, nil
	}

	var inner innerEvent
	if err := json.Unmarshal(payload.Event, &inner); err != nil {
		return nil, fmt.Errorf("unmarshal inner event: %w", err)
	}

	// Filter bot messages
	if inner.BotID != "" {
		return nil, nil
	}

	// Filter message subtypes (bot_message, message_changed, etc.)
	if inner.Subtype != "" {
		return nil, nil
	}

	// Only handle message and app_mention events
	switch inner.Type {
	case "message", "app_mention":
		// ok
	default:
		return nil, nil
	}

	text := inner.Text
	if inner.Type == "app_mention" {
		text = stripMentions(text)
	}

	return &SocketEvent{
		Type:      inner.Type,
		ChannelID: inner.Channel,
		UserID:    inner.User,
		Text:      text,
		ThreadTS:  inner.ThreadTS,
		Timestamp: inner.TS,
		BotID:     inner.BotID,
		Files:     inner.Files,
	}, nil
}

// stripMentions removes all <@USERID> mention patterns and trims whitespace.
func stripMentions(text string) string {
	cleaned := mentionRegex.ReplaceAllString(text, "")
	return strings.TrimSpace(cleaned)
}
