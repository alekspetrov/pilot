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
	// Backoff parameters for reconnection.
	initialBackoff = 1 * time.Second
	maxBackoff     = 30 * time.Second
	backoffFactor  = 2

	// WebSocket keepalive deadlines.
	pongWait   = 30 * time.Second
	pingPeriod = 20 * time.Second // Must be < pongWait.
	writeWait  = 10 * time.Second
)

// SocketModeClient connects to Slack's Socket Mode API via WebSocket.
type SocketModeClient struct {
	appToken   string
	httpClient *http.Client
	dialer     *websocket.Dialer
	log        *slog.Logger

	// mu guards conn for concurrent write access (acknowledge + ping).
	mu   sync.Mutex
	conn *websocket.Conn
}

// NewSocketModeClient creates a Socket Mode client using the given app-level token.
func NewSocketModeClient(appToken string) *SocketModeClient {
	return &SocketModeClient{
		appToken: appToken,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		dialer: websocket.DefaultDialer,
		log:    logging.WithComponent("slack.socketmode"),
	}
}

// OpenConnection calls apps.connections.open and returns the WSS URL.
func (c *SocketModeClient) OpenConnection(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, slackAPIURL+"/apps.connections.open", nil)
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

	var result struct {
		OK    bool   `json:"ok"`
		URL   string `json:"url"`
		Error string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if !result.OK {
		return "", fmt.Errorf("slack API error: %s", result.Error)
	}
	return result.URL, nil
}

// Listen connects to Slack Socket Mode and returns a channel of parsed events.
// The channel stays open across reconnects — callers see a seamless event stream.
// The channel is closed only when ctx is cancelled.
func (c *SocketModeClient) Listen(ctx context.Context) (<-chan *SocketEvent, error) {
	ch := make(chan *SocketEvent, 64)

	go c.listenLoop(ctx, ch)

	return ch, nil
}

// listenLoop manages the WebSocket connection lifecycle with exponential backoff.
func (c *SocketModeClient) listenLoop(ctx context.Context, ch chan<- *SocketEvent) {
	defer close(ch)

	backoff := initialBackoff

	for {
		if ctx.Err() != nil {
			return
		}

		wssURL, err := c.OpenConnection(ctx)
		if err != nil {
			c.log.Warn("failed to open socket mode connection",
				slog.Any("error", err),
				slog.Duration("retry_in", backoff))

			if !c.sleep(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}

		err = c.readLoop(ctx, wssURL, ch)

		// Clean up previous connection.
		c.closeConn()

		if ctx.Err() != nil {
			return
		}

		if err != nil {
			c.log.Warn("socket mode connection lost",
				slog.Any("error", err),
				slog.Duration("retry_in", backoff))

			if !c.sleep(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
		} else {
			// Graceful disconnect — reconnect immediately, reset backoff.
			backoff = initialBackoff
			c.log.Warn("socket mode disconnect received, reconnecting immediately")
		}
	}
}

// readLoop dials the WSS URL and reads messages until an error or disconnect.
// Returns nil on a graceful disconnect envelope, error on connection failure.
func (c *SocketModeClient) readLoop(ctx context.Context, wssURL string, ch chan<- *SocketEvent) error {
	conn, _, err := c.dialer.DialContext(ctx, wssURL, nil)
	if err != nil {
		return fmt.Errorf("dial websocket: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	// Configure ping/pong keepalive.
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		return fmt.Errorf("set read deadline: %w", err)
	}

	// Start ping ticker in background.
	pingDone := make(chan struct{})
	go c.pingLoop(ctx, pingDone)
	defer close(pingDone)

	// Watch context and close connection to unblock ReadMessage.
	go func() {
		select {
		case <-ctx.Done():
			c.mu.Lock()
			if c.conn != nil {
				_ = c.conn.Close()
			}
			c.mu.Unlock()
		case <-pingDone:
		}
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("read message: %w", err)
		}

		env, err := parseEnvelope(data)
		if err != nil {
			c.log.Warn("failed to parse envelope", slog.Any("error", err))
			continue
		}

		// Acknowledge every envelope that has an ID.
		if env.EnvelopeID != "" {
			c.acknowledge(env.EnvelopeID)
		}

		switch env.Type {
		case "disconnect":
			c.log.Warn("received disconnect envelope",
				slog.String("reason", env.Reason))
			return nil // Triggers immediate reconnect.

		case "hello":
			c.log.Info("socket mode connected")
			// Reset backoff on successful connection — done by caller.

		case "events_api":
			event, err := parseEventsAPI(env.Payload)
			if err != nil {
				c.log.Warn("failed to parse event", slog.Any("error", err))
				continue
			}
			if event == nil {
				continue // Filtered (bot message, unsupported type).
			}
			select {
			case ch <- event:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

// acknowledge sends an envelope acknowledgement back on the WebSocket.
func (c *SocketModeClient) acknowledge(envelopeID string) {
	ack, _ := json.Marshal(map[string]string{"envelope_id": envelopeID})

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return
	}
	if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		c.log.Warn("set write deadline for ack", slog.Any("error", err))
		return
	}
	if err := c.conn.WriteMessage(websocket.TextMessage, ack); err != nil {
		c.log.Warn("failed to send ack", slog.Any("error", err))
	}
}

// pingLoop sends WebSocket ping frames at regular intervals.
func (c *SocketModeClient) pingLoop(ctx context.Context, done <-chan struct{}) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			c.mu.Lock()
			if c.conn != nil {
				if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
					c.mu.Unlock()
					c.log.Warn("set write deadline for ping", slog.Any("error", err))
					continue
				}
				if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					c.mu.Unlock()
					c.log.Warn("failed to send ping", slog.Any("error", err))
					continue
				}
			}
			c.mu.Unlock()
		}
	}
}

// closeConn safely closes the current WebSocket connection.
func (c *SocketModeClient) closeConn() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

// sleep waits for the given duration or until ctx is cancelled.
// Returns true if the sleep completed, false if ctx was cancelled.
func (c *SocketModeClient) sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// nextBackoff doubles the backoff duration up to maxBackoff.
func nextBackoff(current time.Duration) time.Duration {
	next := current * backoffFactor
	if next > maxBackoff {
		return maxBackoff
	}
	return next
}
