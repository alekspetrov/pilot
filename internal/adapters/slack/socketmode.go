package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// socketModeURL is the Slack API endpoint to open a Socket Mode connection.
	socketModeURL = "https://slack.com/api/apps.connections.open"

	// defaultPingInterval is the interval between WebSocket pings.
	defaultPingInterval = 30 * time.Second
)

// SocketClient manages a Slack Socket Mode WebSocket connection.
type SocketClient struct {
	appToken     string
	botID        string
	httpClient   *http.Client
	log          *slog.Logger
	onMessage    func(channel, user, text string)
	conn         *websocket.Conn
	mu           sync.Mutex
	pingInterval time.Duration
	dialer       *websocket.Dialer

	// overrideConnectURL replaces socketModeURL for testing.
	overrideConnectURL string

	// reconnectDelay overrides the default 2s pause between reconnects (for testing).
	reconnectDelay time.Duration
}

// SocketOption configures the SocketClient.
type SocketOption func(*SocketClient)

// WithLogger sets the logger for the socket client.
func WithLogger(log *slog.Logger) SocketOption {
	return func(sc *SocketClient) {
		sc.log = log
	}
}

// WithBotID sets the bot user ID for self-message filtering.
func WithBotID(botID string) SocketOption {
	return func(sc *SocketClient) {
		sc.botID = botID
	}
}

// WithOnMessage sets the callback for incoming messages.
func WithOnMessage(fn func(channel, user, text string)) SocketOption {
	return func(sc *SocketClient) {
		sc.onMessage = fn
	}
}

// WithPingInterval sets the WebSocket ping interval.
func WithPingInterval(d time.Duration) SocketOption {
	return func(sc *SocketClient) {
		sc.pingInterval = d
	}
}

// withDialer sets a custom WebSocket dialer (for testing).
func withDialer(d *websocket.Dialer) SocketOption {
	return func(sc *SocketClient) {
		sc.dialer = d
	}
}

// withConnectURL overrides the Slack API URL (for testing).
func withConnectURL(url string) SocketOption {
	return func(sc *SocketClient) {
		sc.overrideConnectURL = url
	}
}

// withReconnectDelay overrides the reconnect pause duration (for testing).
func withReconnectDelay(d time.Duration) SocketOption {
	return func(sc *SocketClient) {
		sc.reconnectDelay = d
	}
}

// NewSocketClient creates a new Socket Mode client.
func NewSocketClient(appToken string, opts ...SocketOption) *SocketClient {
	sc := &SocketClient{
		appToken:     appToken,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		log:          slog.Default(),
		pingInterval: defaultPingInterval,
		dialer:       websocket.DefaultDialer,
	}
	for _, opt := range opts {
		opt(sc)
	}
	return sc
}

// connectResponse is the JSON returned by apps.connections.open.
type connectResponse struct {
	OK    bool   `json:"ok"`
	URL   string `json:"url"`
	Error string `json:"error,omitempty"`
}

// envelope is the outer wrapper for every Socket Mode message.
type envelope struct {
	EnvelopeID string          `json:"envelope_id"`
	Type       string          `json:"type"`
	Payload    json.RawMessage `json:"payload"`
}

// eventPayload is the payload for events_api type envelopes.
type eventPayload struct {
	Event eventInner `json:"event"`
}

// eventInner represents the inner event object.
type eventInner struct {
	Type    string `json:"type"`
	User    string `json:"user"`
	Text    string `json:"text"`
	Channel string `json:"channel"`
	BotID   string `json:"bot_id,omitempty"`
}

// acknowledge sends an envelope acknowledgement back over the WebSocket.
func (sc *SocketClient) acknowledge(conn *websocket.Conn, envelopeID string) error {
	ack := struct {
		EnvelopeID string `json:"envelope_id"`
	}{EnvelopeID: envelopeID}
	return conn.WriteJSON(ack)
}

// connect calls apps.connections.open and returns the WebSocket URL.
func (sc *SocketClient) connect(ctx context.Context) (string, error) {
	url := socketModeURL
	if sc.overrideConnectURL != "" {
		url = sc.overrideConnectURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create connect request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+sc.appToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := sc.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call apps.connections.open: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read connect response: %w", err)
	}

	var cr connectResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return "", fmt.Errorf("failed to parse connect response: %w", err)
	}

	if !cr.OK {
		return "", fmt.Errorf("socket mode connect failed: %s", cr.Error)
	}

	return cr.URL, nil
}

// Run connects to Slack Socket Mode and processes events until ctx is cancelled.
// On disconnect, it automatically reconnects.
func (sc *SocketClient) Run(ctx context.Context) error {
	for {
		if err := sc.runOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			sc.log.Warn("socket mode disconnected, reconnecting", "error", err)
			// Brief pause before reconnect to avoid tight loop.
			delay := sc.reconnectDelay
			if delay == 0 {
				delay = 2 * time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			continue
		}
		return nil
	}
}

// runOnce handles a single WebSocket connection lifecycle.
func (sc *SocketClient) runOnce(ctx context.Context) error {
	wsURL, err := sc.connect(ctx)
	if err != nil {
		return err
	}

	conn, _, err := sc.dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("websocket dial failed: %w", err)
	}

	sc.mu.Lock()
	sc.conn = conn
	sc.mu.Unlock()

	defer func() {
		sc.mu.Lock()
		sc.conn = nil
		sc.mu.Unlock()
		_ = conn.Close()
	}()

	sc.log.Info("socket mode connected", "url", wsURL)

	// Read messages until error or context cancel.
	for {
		if ctx.Err() != nil {
			return nil
		}

		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			sc.log.Warn("failed to parse envelope", "error", err)
			continue
		}

		// Always acknowledge.
		if env.EnvelopeID != "" {
			if ackErr := sc.acknowledge(conn, env.EnvelopeID); ackErr != nil {
				sc.log.Warn("failed to acknowledge", "envelope_id", env.EnvelopeID, "error", ackErr)
			}
		}

		sc.handleEnvelope(env)
	}
}

// handleEnvelope dispatches parsed envelopes to the appropriate handler.
func (sc *SocketClient) handleEnvelope(env envelope) {
	switch env.Type {
	case "events_api":
		sc.handleEvent(env.Payload)
	case "hello":
		sc.log.Info("socket mode hello received")
	default:
		sc.log.Debug("unhandled envelope type", "type", env.Type)
	}
}

// handleEvent processes events_api payloads.
func (sc *SocketClient) handleEvent(raw json.RawMessage) {
	var ep eventPayload
	if err := json.Unmarshal(raw, &ep); err != nil {
		sc.log.Warn("failed to parse event payload", "error", err)
		return
	}

	switch ep.Event.Type {
	case "app_mention":
		sc.handleAppMention(ep.Event)
	case "message":
		sc.handleMessage(ep.Event)
	default:
		sc.log.Debug("unhandled event type", "type", ep.Event.Type)
	}
}

// handleAppMention handles app_mention events, stripping the bot mention prefix.
func (sc *SocketClient) handleAppMention(ev eventInner) {
	if sc.onMessage == nil {
		return
	}

	// Filter self-messages.
	if sc.botID != "" && ev.User == sc.botID {
		return
	}

	text := sc.stripBotMention(ev.Text)
	sc.onMessage(ev.Channel, ev.User, text)
}

// handleMessage handles plain message events.
func (sc *SocketClient) handleMessage(ev eventInner) {
	if sc.onMessage == nil {
		return
	}

	// Filter bot self-messages (by bot_id or user matching botID).
	if sc.botID != "" {
		if ev.BotID != "" || ev.User == sc.botID {
			return
		}
	}

	sc.onMessage(ev.Channel, ev.User, ev.Text)
}

// stripBotMention removes the leading <@BOTID> mention from text.
func (sc *SocketClient) stripBotMention(text string) string {
	if sc.botID == "" {
		return text
	}
	prefix := "<@" + sc.botID + ">"
	text = strings.TrimPrefix(text, prefix)
	return strings.TrimSpace(text)
}
