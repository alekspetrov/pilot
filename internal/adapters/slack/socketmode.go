package slack

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrDisconnect is returned when the server sends a disconnect event.
var ErrDisconnect = errors.New("slack: disconnect requested")

// SocketEvent represents a parsed Slack event received via Socket Mode.
type SocketEvent struct {
	Type      string      // "message" or "app_mention"
	UserID    string      // Slack user ID who sent the message
	BotID     string      // Non-empty if sent by a bot
	Text      string      // Raw message text (may contain <@UBOT> mention)
	ChannelID string      // Channel or DM where the event occurred
	ThreadTS  string      // Thread timestamp (empty if top-level)
	TS        string      // Message timestamp
	Files     []SlackFile // Attached files (images, docs, etc.)
}

// SlackFile represents a file attached to a Slack message.
type SlackFile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MimeType string `json:"mimetype"`
	URL      string `json:"url_private_download"`
	Size     int    `json:"size"`
}

// SocketEnvelope is the top-level Socket Mode JSON wrapper.
type SocketEnvelope struct {
	Type       string          `json:"type"`
	EnvelopeID string          `json:"envelope_id"`
	Payload    json.RawMessage `json:"payload"`
	// disconnect envelopes carry reason + debug_info at top level
	Reason    string `json:"reason,omitempty"`
	DebugInfo *struct {
		Host string `json:"host,omitempty"`
	} `json:"debug_info,omitempty"`
}

// EventPayload is the events_api payload inside the envelope.
type EventPayload struct {
	Token    string     `json:"token"`
	TeamID   string     `json:"team_id"`
	APIAppID string     `json:"api_app_id"`
	Event    InnerEvent `json:"event"`
	Type     string     `json:"type"`
	EventID  string     `json:"event_id"`
}

// InnerEvent is the actual event inside the events_api payload.
type InnerEvent struct {
	Type      string      `json:"type"`
	User      string      `json:"user"`
	BotID     string      `json:"bot_id,omitempty"`
	Text      string      `json:"text"`
	Channel   string      `json:"channel"`
	TS        string      `json:"ts"`
	ThreadTS  string      `json:"thread_ts,omitempty"`
	EventTS   string      `json:"event_ts,omitempty"`
	ChannelType string    `json:"channel_type,omitempty"`
	Files     []SlackFile `json:"files,omitempty"`
}

// parseEnvelope unmarshals a Socket Mode JSON frame and returns:
//   - a *SocketEvent for message/app_mention events (nil for other types)
//   - the envelope_id (for acknowledgement)
//   - an error (ErrDisconnect for disconnect envelopes, parse errors otherwise)
func parseEnvelope(raw []byte) (*SocketEvent, string, error) {
	var env SocketEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, "", fmt.Errorf("unmarshal envelope: %w", err)
	}

	switch env.Type {
	case "disconnect":
		return nil, env.EnvelopeID, ErrDisconnect

	case "events_api":
		var payload EventPayload
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			return nil, env.EnvelopeID, fmt.Errorf("unmarshal events_api payload: %w", err)
		}

		inner := payload.Event

		// Only handle message and app_mention events
		switch inner.Type {
		case "message", "app_mention":
			// Filter bot self-messages
			if inner.BotID != "" {
				return nil, env.EnvelopeID, nil
			}

			evt := &SocketEvent{
				Type:      inner.Type,
				UserID:    inner.User,
				BotID:     inner.BotID,
				Text:      inner.Text,
				ChannelID: inner.Channel,
				ThreadTS:  inner.ThreadTS,
				TS:        inner.TS,
				Files:     inner.Files,
			}
			return evt, env.EnvelopeID, nil

		default:
			// Unhandled event subtype — ack but ignore
			return nil, env.EnvelopeID, nil
		}

	default:
		// hello, interactive, slash_commands, etc. — ack but ignore
		return nil, env.EnvelopeID, nil
	}
}

// mentionRe matches Slack user mentions like <@U12345> or <@U12345|username>.
var mentionRe = regexp.MustCompile(`<@([A-Z0-9]+)(?:\|[^>]*)?>`)

// stripMention removes the bot's @mention from the message text and trims whitespace.
// If botID is empty, all mentions are stripped.
func stripMention(text, botID string) string {
	if botID == "" {
		return strings.TrimSpace(mentionRe.ReplaceAllString(text, ""))
	}

	result := mentionRe.ReplaceAllStringFunc(text, func(match string) string {
		subs := mentionRe.FindStringSubmatch(match)
		if len(subs) >= 2 && subs[1] == botID {
			return ""
		}
		return match
	})
	return strings.TrimSpace(result)
}
