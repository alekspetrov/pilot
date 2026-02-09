package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/alekspetrov/pilot/internal/logging"
	"github.com/gorilla/websocket"
)

// Reconnect backoff constants.
const (
	initialBackoff = 1 * time.Second
	maxBackoff     = 30 * time.Second
	backoffFactor  = 2
)

// SocketModeClient connects to Slack's Socket Mode API using an app-level token.
// It handles the HTTP handshake, WebSocket lifecycle, and auto-reconnect.
type SocketModeClient struct {
	appToken   string
	apiURL     string
	httpClient *http.Client
	log        *slog.Logger
}

// NewSocketModeClient creates a new Socket Mode client with the given app-level token.
// The token must be an xapp-... app-level token (not a bot token).
func NewSocketModeClient(appToken string) *SocketModeClient {
	return &SocketModeClient{
		appToken:   appToken,
		apiURL:     slackAPIURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		log:        logging.WithComponent("slack.socketmode"),
	}
}

// NewSocketModeClientWithBaseURL creates a Socket Mode client with a custom API base URL.
// Used for testing with httptest.NewServer.
func NewSocketModeClientWithBaseURL(appToken, baseURL string) *SocketModeClient {
	return &SocketModeClient{
		appToken:   appToken,
		apiURL:     baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		log:        logging.WithComponent("slack.socketmode"),
	}
}

// connectionsOpenResponse is the JSON response from apps.connections.open.
type connectionsOpenResponse struct {
	OK    bool   `json:"ok"`
	URL   string `json:"url,omitempty"`
	Error string `json:"error,omitempty"`
}

// ErrAuthFailure indicates the app-level token was rejected by Slack.
var ErrAuthFailure = fmt.Errorf("slack socket mode: authentication failed")

// ErrConnectionOpen indicates a non-auth failure when opening a connection.
var ErrConnectionOpen = fmt.Errorf("slack socket mode: failed to open connection")

// OpenConnection calls apps.connections.open with the app-level token
// and returns the WebSocket URL for event streaming.
func (s *SocketModeClient) OpenConnection(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiURL+"/apps.connections.open", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+s.appToken)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrConnectionOpen, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("%w: failed to read response: %w", ErrConnectionOpen, err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: HTTP %d: %s", ErrConnectionOpen, resp.StatusCode, string(body))
	}

	var result connectionsOpenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("%w: failed to parse response: %w", ErrConnectionOpen, err)
	}

	if !result.OK {
		switch result.Error {
		case "invalid_auth", "not_authed", "account_inactive", "token_revoked":
			return "", fmt.Errorf("%w: %s", ErrAuthFailure, result.Error)
		default:
			return "", fmt.Errorf("%w: %s", ErrConnectionOpen, result.Error)
		}
	}

	if result.URL == "" {
		return "", fmt.Errorf("%w: empty WebSocket URL in response", ErrConnectionOpen)
	}

	return result.URL, nil
}

// Listen connects to Slack Socket Mode and emits parsed SocketEvents on the
// returned channel. On connection error or disconnect envelope, it closes the
// socket, calls OpenConnection for a fresh WSS URL, and reconnects with
// exponential backoff (1s → 2s → 4s → max 30s). Backoff resets on successful
// connection. Context cancellation stops the reconnect loop and closes the channel.
func (s *SocketModeClient) Listen(ctx context.Context) (<-chan *SocketEvent, error) {
	// Validate we can connect at least once before returning the channel.
	wssURL, err := s.OpenConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("initial connection: %w", err)
	}

	ch := make(chan *SocketEvent, 64)

	go s.listenLoop(ctx, wssURL, ch)

	return ch, nil
}

// listenLoop is the reconnect wrapper around readLoop. It runs until ctx is cancelled.
func (s *SocketModeClient) listenLoop(ctx context.Context, wssURL string, ch chan<- *SocketEvent) {
	defer close(ch)

	backoff := initialBackoff

	for {
		if ctx.Err() != nil {
			return
		}

		conn, _, err := websocket.DefaultDialer.DialContext(ctx, wssURL, nil)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.log.Warn("websocket dial failed, retrying",
				slog.Any("error", err),
				slog.Duration("backoff", backoff))

			if !s.sleepWithContext(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)

			// Get fresh URL on dial failure.
			newURL, openErr := s.OpenConnection(ctx)
			if openErr != nil {
				if ctx.Err() != nil {
					return
				}
				s.log.Warn("OpenConnection failed, retrying",
					slog.Any("error", openErr),
					slog.Duration("backoff", backoff))
				if !s.sleepWithContext(ctx, backoff) {
					return
				}
				backoff = nextBackoff(backoff)
				continue
			}
			wssURL = newURL
			continue
		}

		// Connected — reset backoff.
		backoff = initialBackoff
		s.log.Info("socket mode connected")

		// readLoop blocks until the connection drops or ctx is cancelled.
		s.readLoop(ctx, conn, ch)

		// Connection ended — close it and prepare to reconnect.
		_ = conn.Close()

		if ctx.Err() != nil {
			return
		}

		s.log.Info("connection lost, reconnecting",
			slog.Duration("backoff", backoff))

		if !s.sleepWithContext(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff)

		// Get fresh WSS URL for reconnect.
		newURL, openErr := s.OpenConnection(ctx)
		if openErr != nil {
			if ctx.Err() != nil {
				return
			}
			s.log.Warn("OpenConnection failed during reconnect",
				slog.Any("error", openErr),
				slog.Duration("backoff", backoff))
			if !s.sleepWithContext(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}
		wssURL = newURL
		backoff = initialBackoff // fresh URL obtained — reset backoff
	}
}

// readLoop reads envelopes from the WebSocket, acknowledges them, and emits
// parsed events. Returns when the connection drops or ctx is cancelled.
func (s *SocketModeClient) readLoop(ctx context.Context, conn *websocket.Conn, ch chan<- *SocketEvent) {
	// Close the connection when ctx is cancelled to unblock ReadMessage.
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseNormalClosure,
				websocket.CloseGoingAway) {
				s.log.Warn("websocket read error", slog.Any("error", err))
			}
			return
		}

		envelopeID, envelopeType, evt, parseErr := parseEnvelope(data)
		if parseErr != nil {
			s.log.Error("failed to parse envelope", slog.Any("error", parseErr))
			continue
		}

		// Acknowledge every envelope with an envelope_id.
		if envelopeID != "" {
			if ackErr := s.acknowledge(conn, envelopeID); ackErr != nil {
				s.log.Error("failed to acknowledge envelope",
					slog.String("envelope_id", envelopeID),
					slog.Any("error", ackErr))
			}
		}

		// Disconnect envelope → return to trigger reconnect.
		if envelopeType == EnvelopeTypeDisconnect {
			s.log.Info("disconnect envelope received, will reconnect")
			return
		}

		// Emit non-nil events.
		if evt != nil && !evt.IsBotMessage() {
			select {
			case ch <- evt:
			case <-ctx.Done():
				return
			}
		}
	}
}

// acknowledge sends an envelope acknowledgment back over the WebSocket.
func (s *SocketModeClient) acknowledge(conn *websocket.Conn, envelopeID string) error {
	ack := struct {
		EnvelopeID string `json:"envelope_id"`
	}{EnvelopeID: envelopeID}
	data, err := json.Marshal(ack)
	if err != nil {
		return fmt.Errorf("marshal ack: %w", err)
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

// sleepWithContext waits for the given duration or until ctx is cancelled.
// Returns true if the sleep completed, false if ctx was cancelled.
func (s *SocketModeClient) sleepWithContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// nextBackoff doubles the backoff duration, capping at maxBackoff.
func nextBackoff(current time.Duration) time.Duration {
	next := current * backoffFactor
	if next > maxBackoff {
		return maxBackoff
	}
	return next
}
