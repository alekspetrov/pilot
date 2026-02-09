package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/alekspetrov/pilot/internal/logging"
	"github.com/gorilla/websocket"
)

const (
	// connectionsOpenURL is the Slack API endpoint to open a Socket Mode connection.
	connectionsOpenURL = "https://slack.com/api/apps.connections.open"

	// Default backoff parameters for reconnection.
	initialBackoff = 1 * time.Second
	maxBackoff     = 30 * time.Second
	backoffFactor  = 2

	// WebSocket timeouts.
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = 50 * time.Second // Must be less than pongWait.
)

// SocketModeClient connects to Slack via Socket Mode WebSocket.
type SocketModeClient struct {
	appToken   string
	httpClient *http.Client
	dialer     *websocket.Dialer
	log        *slog.Logger

	// apiURL overrides the connections.open endpoint for testing.
	apiURL string
}

// NewSocketModeClient creates a new Socket Mode client.
func NewSocketModeClient(appToken string) *SocketModeClient {
	return &SocketModeClient{
		appToken: appToken,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		dialer: websocket.DefaultDialer,
		log:    logging.WithComponent("slack.socketmode"),
		apiURL: connectionsOpenURL,
	}
}

// connectionsOpenResponse is the JSON response from apps.connections.open.
type connectionsOpenResponse struct {
	OK    bool   `json:"ok"`
	URL   string `json:"url"`
	Error string `json:"error,omitempty"`
}

// OpenConnection calls apps.connections.open and returns the WSS URL.
func (c *SocketModeClient) OpenConnection(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.appToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("open connection: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var result connectionsOpenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if !result.OK {
		return "", fmt.Errorf("slack API error: %s", result.Error)
	}

	if result.URL == "" {
		return "", fmt.Errorf("empty WSS URL in response")
	}

	return result.URL, nil
}

// Listen opens a Socket Mode connection and streams parsed events.
// It automatically reconnects on disconnect with exponential backoff.
// The returned channel is closed when ctx is cancelled.
func (c *SocketModeClient) Listen(ctx context.Context) (<-chan *SocketEvent, error) {
	wssURL, err := c.OpenConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("initial connection: %w", err)
	}

	events := make(chan *SocketEvent, 64)

	go c.listenLoop(ctx, wssURL, events)

	return events, nil
}

// listenLoop manages the WebSocket connection lifecycle with auto-reconnect.
func (c *SocketModeClient) listenLoop(ctx context.Context, wssURL string, events chan<- *SocketEvent) {
	defer close(events)

	backoff := initialBackoff

	for {
		if ctx.Err() != nil {
			return
		}

		err := c.readLoop(ctx, wssURL, events)
		if ctx.Err() != nil {
			return
		}

		if err != nil {
			c.log.Warn("WebSocket disconnected, reconnecting",
				slog.String("error", err.Error()),
				slog.Duration("backoff", backoff))
		}

		// Wait before reconnecting
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		// Get a new WSS URL
		newURL, err := c.OpenConnection(ctx)
		if err != nil {
			c.log.Error("Failed to get new WSS URL",
				slog.String("error", err.Error()))

			// Increase backoff
			backoff = min(backoff*backoffFactor, maxBackoff)
			continue
		}

		wssURL = newURL
		backoff = initialBackoff
	}
}

// readLoop connects to a WSS URL, reads messages, and emits events.
// Returns when the connection is lost or ctx is cancelled.
func (c *SocketModeClient) readLoop(ctx context.Context, wssURL string, events chan<- *SocketEvent) error {
	conn, _, err := c.dialer.DialContext(ctx, wssURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	var closeOnce sync.Once
	closeConn := func() {
		closeOnce.Do(func() {
			_ = conn.Close()
		})
	}
	defer closeConn()

	// Set up pong handler for keepalive
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		return fmt.Errorf("set read deadline: %w", err)
	}

	// Start ping ticker in background
	pingDone := make(chan struct{})
	go func() {
		defer close(pingDone)
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := conn.WriteControl(
					websocket.PingMessage,
					nil,
					time.Now().Add(writeWait),
				); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Read messages
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read message: %w", err)
		}

		env, err := parseEnvelope(raw)
		if err != nil {
			c.log.Warn("Failed to parse envelope",
				slog.String("error", err.Error()))
			continue
		}

		// Always acknowledge
		if env.EnvelopeID != "" {
			if ackErr := c.acknowledge(conn, env.EnvelopeID); ackErr != nil {
				c.log.Warn("Failed to send acknowledgment",
					slog.String("envelope_id", env.EnvelopeID),
					slog.String("error", ackErr.Error()))
			}
		}

		switch env.Type {
		case "hello":
			c.log.Info("Socket Mode connected")

		case "disconnect":
			c.log.Info("Received disconnect",
				slog.String("reason", env.Reason))
			return fmt.Errorf("server requested disconnect: %s", env.Reason)

		case "events_api":
			ev, err := parseSocketEvent(env.Payload)
			if err != nil {
				c.log.Warn("Failed to parse event",
					slog.String("error", err.Error()))
				continue
			}
			if ev == nil {
				continue // Filtered event (bot message, unsupported type)
			}

			select {
			case events <- ev:
			case <-ctx.Done():
				return ctx.Err()
			}

		default:
			c.log.Debug("Ignoring envelope type",
				slog.String("type", env.Type))
		}
	}
}

// acknowledge sends an acknowledgment frame back for an envelope.
func (c *SocketModeClient) acknowledge(conn *websocket.Conn, envelopeID string) error {
	ack := struct {
		EnvelopeID string `json:"envelope_id"`
	}{
		EnvelopeID: envelopeID,
	}

	data, err := json.Marshal(ack)
	if err != nil {
		return fmt.Errorf("marshal ack: %w", err)
	}

	if err := conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}

	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("write ack: %w", err)
	}

	return nil
}

// acknowledgePayload is used in tests to verify ack messages.
type acknowledgePayload struct {
	EnvelopeID string `json:"envelope_id"`
}

// parseAcknowledge parses an acknowledgment message.
func parseAcknowledge(data []byte) (*acknowledgePayload, error) {
	var ack acknowledgePayload
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&ack); err != nil {
		return nil, err
	}
	return &ack, nil
}
