package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/alekspetrov/pilot/internal/logging"
	"github.com/gorilla/websocket"
)

// GatewayClient connects to Discord Gateway and handles event streaming.
type GatewayClient struct {
	botToken      string
	intents       int
	conn          *websocket.Conn
	sessionID     string
	botUserID     string // populated from READY event payload
	seq           *int
	heartbeatTick *time.Ticker
	stopCh        chan struct{}
	closeOnce     sync.Once // guards Close() against double-call
	mu            sync.Mutex
	log           *slog.Logger
}

// NewGatewayClient creates a new Discord Gateway client.
func NewGatewayClient(botToken string, intents int) *GatewayClient {
	return &GatewayClient{
		botToken: botToken,
		intents:  intents,
		stopCh:   make(chan struct{}),
		log:      logging.WithComponent("discord.gateway"),
	}
}

// Connect establishes a WebSocket connection to Discord Gateway.
func (g *GatewayClient) Connect(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Get gateway URL
	client := NewClient(g.botToken)
	gatewayURL, err := client.GetGatewayURL(ctx)
	if err != nil {
		return fmt.Errorf("get gateway url: %w", err)
	}

	// Connect to WebSocket
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.DialContext(ctx, gatewayURL+"?v=10&encoding=json", nil)
	if err != nil {
		return fmt.Errorf("dial gateway: %w", err)
	}

	g.conn = conn
	g.log.Info("Connected to Discord Gateway")

	// Wait for HELLO and send IDENTIFY
	if err := g.doHandleHello(ctx, false); err != nil {
		_ = g.conn.Close()
		g.conn = nil
		return fmt.Errorf("handle hello: %w", err)
	}

	return nil
}

// reconnect closes the existing connection, re-dials, and handles HELLO.
// If resume is true, sends RESUME; otherwise sends IDENTIFY.
// Must NOT be called while g.mu is held.
func (g *GatewayClient) reconnect(ctx context.Context, resume bool) error {
	g.mu.Lock()
	if g.conn != nil {
		_ = g.conn.Close()
		g.conn = nil // signals old heartbeatLoop goroutine to exit
	}
	g.mu.Unlock()

	client := NewClient(g.botToken)
	gatewayURL, err := client.GetGatewayURL(ctx)
	if err != nil {
		return fmt.Errorf("get gateway url: %w", err)
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.DialContext(ctx, gatewayURL+"?v=10&encoding=json", nil)
	if err != nil {
		return fmt.Errorf("dial gateway: %w", err)
	}

	g.mu.Lock()
	g.conn = conn
	if err := g.doHandleHello(ctx, resume); err != nil {
		_ = g.conn.Close()
		g.conn = nil
		g.mu.Unlock()
		return fmt.Errorf("handle hello: %w", err)
	}
	g.mu.Unlock()

	return nil
}

// CanResume reports whether a session resume is possible.
func (g *GatewayClient) CanResume() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.sessionID != "" && g.seq != nil
}

// BotUserID returns the bot's own user ID (populated after READY event).
func (g *GatewayClient) BotUserID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.botUserID
}

// doHandleHello receives HELLO opcode and sends either IDENTIFY or RESUME.
// Must be called while g.mu is held.
func (g *GatewayClient) doHandleHello(ctx context.Context, resume bool) error {
	// Set read deadline for HELLO
	deadline := time.Now().Add(10 * time.Second)
	_ = g.conn.SetReadDeadline(deadline)
	defer func() { _ = g.conn.SetReadDeadline(time.Time{}) }()

	var event GatewayEvent
	if err := g.conn.ReadJSON(&event); err != nil {
		return fmt.Errorf("read hello: %w", err)
	}

	if event.Op != OpcodeHello {
		return fmt.Errorf("expected hello opcode %d, got %d", OpcodeHello, event.Op)
	}

	var hello Hello
	data, _ := json.Marshal(event.D)
	if err := json.Unmarshal(data, &hello); err != nil {
		return fmt.Errorf("parse hello: %w", err)
	}

	if resume {
		seq := 0
		if g.seq != nil {
			seq = *g.seq
		}
		resumePayload := Resume{
			Op: OpcodeResume,
			D: ResumeData{
				Token:     g.botToken,
				SessionID: g.sessionID,
				Seq:       seq,
			},
		}
		if err := g.conn.WriteJSON(resumePayload); err != nil {
			return fmt.Errorf("send resume: %w", err)
		}
		g.log.Info("Sent RESUME", slog.String("session_id", g.sessionID))
	} else {
		identifyData := IdentifyData{
			Token:   g.botToken,
			Intents: g.intents,
			Properties: map[string]string{
				"os":      "linux",
				"browser": "pilot",
				"device":  "pilot",
			},
		}
		identify := Identify{Op: OpcodeIdentify, D: identifyData}
		if err := g.conn.WriteJSON(identify); err != nil {
			return fmt.Errorf("send identify: %w", err)
		}
		g.log.Info("Sent IDENTIFY", slog.Int("heartbeat_interval", hello.HeartbeatInterval))
	}

	// Stop previous ticker before starting a new one
	if g.heartbeatTick != nil {
		g.heartbeatTick.Stop()
	}
	g.heartbeatTick = time.NewTicker(time.Duration(hello.HeartbeatInterval) * time.Millisecond)
	go g.heartbeatLoop()

	return nil
}

// heartbeatLoop sends periodic heartbeat messages.
func (g *GatewayClient) heartbeatLoop() {
	defer g.heartbeatTick.Stop()

	for {
		select {
		case <-g.stopCh:
			return
		case <-g.heartbeatTick.C:
			g.mu.Lock()
			if g.conn == nil {
				g.mu.Unlock()
				return
			}

			hb := Heartbeat{
				Op: OpcodeHeartbeat,
				D:  g.seq,
			}

			_ = g.conn.WriteJSON(hb)
			g.mu.Unlock()
		}
	}
}

// Listen returns a channel of incoming events. Blocks until ctx is cancelled.
func (g *GatewayClient) Listen(ctx context.Context) (<-chan GatewayEvent, error) {
	if g.conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	out := make(chan GatewayEvent, 64)

	go func() {
		defer close(out)

		for {
			select {
			case <-ctx.Done():
				return
			case <-g.stopCh:
				return
			default:
			}

			var event GatewayEvent
			if err := g.conn.ReadJSON(&event); err != nil {
				g.log.Warn("Read event error", slog.Any("error", err))
				return
			}

			// Track sequence number for RESUME
			if event.S != nil {
				g.mu.Lock()
				g.seq = event.S
				g.mu.Unlock()
			}

			// Track session ID and bot user ID on READY
			if event.T != nil && *event.T == "READY" {
				var readyData struct {
					SessionID string `json:"session_id"`
					User      struct {
						ID string `json:"id"`
					} `json:"user"`
				}
				data, _ := json.Marshal(event.D)
				if err := json.Unmarshal(data, &readyData); err == nil {
					g.mu.Lock()
					g.sessionID = readyData.SessionID
					g.botUserID = readyData.User.ID
					g.mu.Unlock()
					g.log.Info("Received READY",
						slog.String("session_id", readyData.SessionID),
						slog.String("bot_user_id", readyData.User.ID))
				}
			}

			select {
			case out <- event:
			case <-ctx.Done():
				return
			case <-g.stopCh:
				return
			}
		}
	}()

	return out, nil
}

// Resume attempts to resume the session over the existing connection.
func (g *GatewayClient) Resume(ctx context.Context) error {
	g.mu.Lock()
	if g.conn == nil || g.sessionID == "" || g.seq == nil {
		g.mu.Unlock()
		return fmt.Errorf("cannot resume: missing session")
	}

	resume := Resume{
		Op: OpcodeResume,
		D: ResumeData{
			Token:     g.botToken,
			SessionID: g.sessionID,
			Seq:       *g.seq,
		},
	}
	g.mu.Unlock()

	if err := g.conn.WriteJSON(resume); err != nil {
		return fmt.Errorf("send resume: %w", err)
	}

	g.log.Info("Sent RESUME", slog.String("session_id", g.sessionID))
	return nil
}

// Close closes the WebSocket connection. Safe to call multiple times.
func (g *GatewayClient) Close() error {
	var connErr error
	g.closeOnce.Do(func() {
		close(g.stopCh)
		g.mu.Lock()
		if g.heartbeatTick != nil {
			g.heartbeatTick.Stop()
		}
		if g.conn != nil {
			connErr = g.conn.Close()
		}
		g.mu.Unlock()
	})
	return connErr
}
