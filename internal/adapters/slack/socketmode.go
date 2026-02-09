package slack

import (
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
	// defaultReconnectMin is the minimum backoff duration for reconnect.
	defaultReconnectMin = 1 * time.Second
	// defaultReconnectMax is the maximum backoff duration for reconnect.
	defaultReconnectMax = 30 * time.Second
	// defaultPingInterval is the interval for WebSocket ping messages.
	defaultPingInterval = 30 * time.Second
)

// SocketModeClient connects to Slack via Socket Mode (WebSocket).
type SocketModeClient struct {
	appToken   string
	apiURL     string // base URL for apps.connections.open (overridable for tests)
	httpClient *http.Client
	log        *slog.Logger

	reconnectMin time.Duration
	reconnectMax time.Duration
	pingInterval time.Duration

	mu   sync.Mutex
	conn *websocket.Conn
}

// SocketModeOption configures SocketModeClient.
type SocketModeOption func(*SocketModeClient)

// WithAPIURL overrides the Slack API base URL (for testing).
func WithAPIURL(url string) SocketModeOption {
	return func(c *SocketModeClient) {
		c.apiURL = url
	}
}

// WithHTTPClient overrides the HTTP client (for testing).
func WithHTTPClient(hc *http.Client) SocketModeOption {
	return func(c *SocketModeClient) {
		c.httpClient = hc
	}
}

// WithReconnectBackoff configures reconnect backoff durations.
func WithReconnectBackoff(min, max time.Duration) SocketModeOption {
	return func(c *SocketModeClient) {
		c.reconnectMin = min
		c.reconnectMax = max
	}
}

// NewSocketModeClient creates a new Socket Mode client.
func NewSocketModeClient(appToken string, opts ...SocketModeOption) *SocketModeClient {
	c := &SocketModeClient{
		appToken:     appToken,
		apiURL:       slackAPIURL,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		log:          logging.WithComponent("slack.socketmode"),
		reconnectMin: defaultReconnectMin,
		reconnectMax: defaultReconnectMax,
		pingInterval: defaultPingInterval,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// connectionsOpenResponse is the response from apps.connections.open.
type connectionsOpenResponse struct {
	OK    bool   `json:"ok"`
	URL   string `json:"url"`
	Error string `json:"error,omitempty"`
}

// OpenConnection calls apps.connections.open to get a WebSocket URL.
func (c *SocketModeClient) OpenConnection(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/apps.connections.open", nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.appToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("connections.open request: %w", err)
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
		return "", fmt.Errorf("connections.open error: %s", result.Error)
	}

	return result.URL, nil
}

// Listen connects to the Socket Mode WebSocket and emits events.
// It automatically reconnects on disconnect with exponential backoff.
// The returned channel is closed when the context is cancelled.
func (c *SocketModeClient) Listen(ctx context.Context) (<-chan *SocketEvent, error) {
	ch := make(chan *SocketEvent, 64)

	go c.listenLoop(ctx, ch)

	return ch, nil
}

// listenLoop manages the connect→read→reconnect cycle.
func (c *SocketModeClient) listenLoop(ctx context.Context, ch chan<- *SocketEvent) {
	defer close(ch)

	backoff := c.reconnectMin

	for {
		if ctx.Err() != nil {
			return
		}

		wssURL, err := c.OpenConnection(ctx)
		if err != nil {
			c.log.Error("Failed to open connection", slog.Any("error", err))
			if !c.sleep(ctx, backoff) {
				return
			}
			backoff = c.nextBackoff(backoff)
			continue
		}

		// Reset backoff on successful connection open
		backoff = c.reconnectMin

		reconnect := c.readLoop(ctx, wssURL, ch)
		if !reconnect {
			return
		}

		c.log.Info("Reconnecting to Socket Mode")
	}
}

// readLoop connects to the WebSocket and reads events until disconnect.
// Returns true if reconnect should happen, false if context was cancelled.
func (c *SocketModeClient) readLoop(ctx context.Context, wssURL string, ch chan<- *SocketEvent) bool {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wssURL, nil)
	if err != nil {
		c.log.Error("WebSocket dial failed", slog.Any("error", err))
		return true // reconnect
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
		_ = conn.Close()
	}()

	// Set up ping/pong keepalive
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(c.pingInterval * 2))
	})

	// Start ping ticker
	pingDone := make(chan struct{})
	go func() {
		defer close(pingDone)
		ticker := time.NewTicker(c.pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.mu.Lock()
				if c.conn != nil {
					_ = c.conn.WriteMessage(websocket.PingMessage, nil)
				}
				c.mu.Unlock()
			}
		}
	}()

	for {
		if ctx.Err() != nil {
			return false
		}

		// Set read deadline to detect stale connections
		_ = conn.SetReadDeadline(time.Now().Add(c.pingInterval * 3))

		_, data, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return false
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				c.log.Info("WebSocket closed normally")
			} else {
				c.log.Error("WebSocket read error", slog.Any("error", err))
			}
			return true // reconnect
		}

		envelopeID, envType, event, err := parseEnvelope(data)
		if err != nil {
			c.log.Error("Failed to parse envelope", slog.Any("error", err))
			continue
		}

		// Acknowledge the envelope
		if envelopeID != "" {
			c.acknowledge(conn, envelopeID)
		}

		// Handle disconnect envelope
		if envType == "disconnect" {
			c.log.Info("Received disconnect envelope, reconnecting")
			return true
		}

		// Emit event if we got one
		if event != nil {
			select {
			case ch <- event:
			case <-ctx.Done():
				return false
			}
		}
	}
}

// acknowledge sends an acknowledgement for a Socket Mode envelope.
func (c *SocketModeClient) acknowledge(conn *websocket.Conn, envelopeID string) {
	ack := struct {
		EnvelopeID string `json:"envelope_id"`
	}{
		EnvelopeID: envelopeID,
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := conn.WriteJSON(ack); err != nil {
		c.log.Error("Failed to acknowledge", slog.String("envelope_id", envelopeID), slog.Any("error", err))
	}
}

// nextBackoff doubles the backoff duration up to the max.
func (c *SocketModeClient) nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > c.reconnectMax {
		return c.reconnectMax
	}
	return next
}

// sleep waits for the given duration or until context is cancelled.
// Returns false if context was cancelled.
func (c *SocketModeClient) sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
